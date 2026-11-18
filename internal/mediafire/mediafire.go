package mediafire

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func CreateHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func DownloadFromURL(url string) ([]byte, string, error) {
	client := CreateHTTPClient()

	if IsMediaFireURL(url) {
		directURL, err := ExtractMediaFireDownloadURL(client, url)
		if err != nil {
			return nil, "", fmt.Errorf("failed to extract MediaFire download URL: %v", err)
		}
		url = directURL
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	data, err := readAllLimited(resp.Body, maxDownloadBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response body: %v", err)
	}

	filename := extractFilename(resp, url)

	return data, filename, nil
}

// maxDownloadBytes caps a single download. Without it a hostile or broken
// server can stream until the process runs out of memory, which in a desktop app
// means the window simply disappears.
const maxDownloadBytes = 512 << 20 // 512 MiB

func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file is larger than %d MB", limit>>20)
	}
	return data, nil
}

func IsMediaFireURL(url string) bool {
	return strings.Contains(strings.ToLower(url), "mediafire.com")
}

func IsMediaFireFolderURL(url string) bool {
	return IsMediaFireURL(url) && strings.Contains(url, "/folder/")
}

type mediaFireFileEntry struct {
	Name string
	URL  string
}

func extractMediaFireFolderKey(folderURL string) string {
	parts := strings.Split(folderURL, "/")
	for i, p := range parts {
		if p == "folder" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

var mediaFireAPIBase = "https://www.mediafire.com/api/1.5"

func ExtractMediaFireFolderLinks(client *http.Client, folderURL string) ([]mediaFireFileEntry, error) {
	folderKey := extractMediaFireFolderKey(folderURL)
	if folderKey == "" {
		return nil, fmt.Errorf("could not extract folder key from URL")
	}

	type mfFileLink struct {
		NormalDownload string `json:"normal_download"`
	}
	type mfFile struct {
		Quickkey string     `json:"quickkey"`
		Filename string     `json:"filename"`
		Links    mfFileLink `json:"links"`
	}
	type mfFolderContent struct {
		Files      []mfFile `json:"files"`
		MoreChunks string   `json:"more_chunks"`
	}
	type mfResponse struct {
		FolderContent mfFolderContent `json:"folder_content"`
		Result        string          `json:"result"`
		Message       string          `json:"message"`
	}
	type mfAPIResponse struct {
		Response mfResponse `json:"response"`
	}

	// The folder key comes from a URL the user pasted, so it is escaped into the
	// query instead of being spliced in raw ("k&content_type=x" would otherwise
	// rewrite the request). The chunk loop is capped so a server that always
	// answers "more_chunks: yes" cannot keep the app paginating forever.
	const maxFolderChunks = 100
	var allFiles []mediaFireFileEntry
	chunk := 1
	for {
		apiURL := fmt.Sprintf("%s/folder/get_content.php?folder_key=%s&content_type=files&response_format=json&chunk=%d&first_item=1&last_item=100", mediaFireAPIBase, url.QueryEscape(folderKey), chunk)

		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch MediaFire API: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("MediaFire API returned HTTP %d", resp.StatusCode)
		}

		body, err := readAllLimited(resp.Body, 8<<20)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read API response: %v", err)
		}

		var apiResp mfAPIResponse
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return nil, fmt.Errorf("failed to parse API response: %v", err)
		}

		if !strings.EqualFold(apiResp.Response.Result, "Success") {
			msg := apiResp.Response.Message
			if msg == "" {
				msg = "MediaFire API returned an error"
			}
			return nil, fmt.Errorf("MediaFire API error: %s", msg)
		}

		files := apiResp.Response.FolderContent.Files
		if len(files) == 0 {
			break
		}

		for _, f := range files {
			if f.Links.NormalDownload == "" {
				continue
			}
			name := f.Filename
			name = strings.ReplaceAll(name, "+", " ")
			allFiles = append(allFiles, mediaFireFileEntry{
				Name: name,
				URL:  f.Links.NormalDownload,
			})
		}

		if len(files) < 100 || strings.EqualFold(apiResp.Response.FolderContent.MoreChunks, "no") || chunk >= maxFolderChunks {
			break
		}
		chunk++
	}

	return allFiles, nil
}

func DownloadDirect(client *http.Client, url string) ([]byte, string, error) {
	dlURL := url
	if IsMediaFireURL(url) && !IsMediaFireFolderURL(url) {
		extracted, err := ExtractMediaFireDownloadURL(client, url)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get MediaFire download link: %v", err)
		}
		dlURL = extracted
	}

	req, err := http.NewRequest("GET", dlURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("status %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/html") {
		return nil, "", fmt.Errorf("got HTML instead of file (likely a bad download link)")
	}

	data, err := readAllLimited(resp.Body, maxDownloadBytes)
	if err != nil {
		return nil, "", err
	}

	return data, extractFilename(resp, dlURL), nil
}

func resolveDownloadURL(base *url.URL, loc string) string {
	if strings.HasPrefix(loc, "//") {
		return "https:" + loc
	}
	ref, err := url.Parse(loc)
	if err != nil {
		return loc
	}
	return base.ResolveReference(ref).String()
}

func ExtractMediaFireDownloadURL(client *http.Client, pageURL string) (string, error) {
	noRedirect := &http.Client{
		Timeout:   client.Timeout,
		Transport: client.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch MediaFire page: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		if loc != "" {
			return resolveDownloadURL(resp.Request.URL, loc), nil
		}
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from MediaFire", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read MediaFire page: %v", err)
	}

	html := string(body)

	patterns := []string{
		`id="downloadButton"[^>]*href="([^"]+)"`,
		`class="input popsok"[^>]*href="([^"]+)"`,
		`href="(https?://download\d*[^"]*\.zip[^"]*)"`,
		`href="(https?://[^"]*\.mediafire\.com/[^"]*)"`,
		`var\s+LimiServer\s*=\s*"([^"]+)"`,
		`class="download_link[^"]*"[^>]*href="([^"]+)"`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(html)
		if len(matches) > 1 {
			dlURL := strings.Trim(matches[1], `"`)
			if strings.HasPrefix(dlURL, "//") {
				dlURL = "https:" + dlURL
			}
			return dlURL, nil
		}
	}

	dlRe := regexp.MustCompile(`href="(https?://[^"]*)"`)
	allLinks := dlRe.FindAllStringSubmatch(html, -1)
	for _, match := range allLinks {
		if len(match) > 1 {
			link := match[1]
			if strings.Contains(link, "download") || strings.Contains(link, ".zip") {
				return link, nil
			}
		}
	}

	return "", fmt.Errorf("could not find download link on MediaFire page")
}

func extractFilename(resp *http.Response, rawURL string) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		// RFC 5987 puts the real (UTF-8) name in filename*=charset'lang'value.
		// Reading it as a plain filename kept the "UTF-8''" prefix and left
		// percent escapes in the name.
		encoded := regexp.MustCompile(`(?i)filename\*\s*=\s*([^;\n]+)`)
		if m := encoded.FindStringSubmatch(cd); len(m) > 1 {
			value := strings.Trim(strings.TrimSpace(m[1]), `"`)
			if i := strings.Index(value, "''"); i >= 0 {
				value = value[i+2:]
			}
			if decoded, err := url.PathUnescape(value); err == nil {
				value = decoded
			}
			if name := sanitizeFilename(value); name != "" {
				return name
			}
		}

		re := regexp.MustCompile(`filename="?([^";\n]+)"?`)
		matches := re.FindStringSubmatch(cd)
		if len(matches) > 1 {
			name := sanitizeFilename(matches[1])
			if name != "" {
				return name
			}
		}
	}

	// Only the path is searched: taking the last dotted segment of the whole URL
	// named files after the host ("https://dl.example.com/" became
	// "dl.example.com").
	if parsed, err := url.Parse(rawURL); err == nil {
		parts := strings.Split(parsed.Path, "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" && strings.Contains(parts[i], ".") {
				if name := sanitizeFilename(parts[i]); name != "" {
					return name
				}
			}
		}
	}

	return "download.zip"
}

// sanitizeFilename reduces a server supplied name to a plain base name. The name
// is later joined onto a temp or output folder, so a value carrying "..\\", an
// absolute path or a drive letter would let the server choose where MEW writes.
// Returns "" when nothing usable is left.
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	// Names from a URL are percent-encoded; decode first so "%2F..%2F" is
	// seen as the traversal it is rather than kept as literal characters.
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\\|?*`, r) {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	return name
}
