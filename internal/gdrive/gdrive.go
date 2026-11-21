// Package gdrive reads public Google Drive share links: a file link becomes a
// direct download URL, and a folder link is listed so every pack inside can be
// ported. Nothing here signs in, so a link only works when it is shared with
// "Anyone with the link"; anything else is reported as not public instead of
// sending the user to a login page.
package gdrive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

const (
	// The usercontent host is the endpoint Drive's own download button uses;
	// confirm=t skips the "can't scan for viruses" interstitial that the older
	// uc?export=download endpoint answers large files with.
	downloadEndpoint = "https://drive.usercontent.google.com/download"
	folderPageBase   = "https://drive.google.com/drive/folders/"
	folderMIME       = "application/vnd.google-apps.folder"

	maxFolderDepth = 5
	maxFolderFiles = 2000
	maxFolderPages = 400
	maxPageBytes   = 32 << 20
)

// ErrNotPublic is returned when a link needs a signed-in Google account.
var ErrNotPublic = errors.New("this Google Drive link is not shared publicly; set sharing to \"Anyone with the link\" and try again")

var (
	driveFileIDRe   = regexp.MustCompile(`/(?:file|document|spreadsheets|presentation)/d/([A-Za-z0-9_-]{10,})`)
	driveFolderIDRe = regexp.MustCompile(`/folders/([A-Za-z0-9_-]{10,})`)
)

func IsDriveURL(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "drive.google.com") ||
		strings.Contains(lower, "docs.google.com") ||
		strings.Contains(lower, "drive.usercontent.google.com")
}

// FileID pulls the file id out of a share link: /file/d/<id>/view,
// /open?id=<id> and the older uc?id=<id> form.
func FileID(raw string) string {
	if m := driveFileIDRe.FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	if driveFolderIDRe.MatchString(raw) {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		if id := u.Query().Get("id"); id != "" {
			return id
		}
	}
	return ""
}

// FolderID pulls the folder id out of /drive/folders/<id> and its /u/0/ variant.
func FolderID(raw string) string {
	if m := driveFolderIDRe.FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	return ""
}

func IsDriveFolderURL(raw string) bool {
	return IsDriveURL(raw) && FolderID(raw) != ""
}

func DownloadURL(fileID string) string {
	q := url.Values{}
	q.Set("id", fileID)
	q.Set("export", "download")
	q.Set("confirm", "t")
	return downloadEndpoint + "?" + q.Encode()
}

// ResolveDownloadURL swaps a Drive file link for its direct download URL and
// leaves every other URL untouched, so one code path can take either host.
func ResolveDownloadURL(raw string) string {
	if !IsDriveURL(raw) {
		return raw
	}
	if id := FileID(raw); id != "" {
		return DownloadURL(id)
	}
	return raw
}

type FileEntry struct {
	Name string
	URL  string
}

type driveEntry struct {
	id       string
	name     string
	isFolder bool
}

// ExtractFolderFiles lists every downloadable file in a public Drive folder,
// including the contents of subfolders: Drive folders are usually a shelf of
// subfolders, so a flat listing would find nothing.
func ExtractFolderFiles(client *http.Client, folderURL string) ([]FileEntry, error) {
	rootID := FolderID(folderURL)
	if rootID == "" {
		return nil, fmt.Errorf("could not find a Google Drive folder id in the link")
	}

	var files []FileEntry
	visited := map[string]bool{}
	pages := 0

	var walk func(id string, depth int) error
	walk = func(id string, depth int) error {
		if visited[id] || depth > maxFolderDepth || pages >= maxFolderPages || len(files) >= maxFolderFiles {
			return nil
		}
		visited[id] = true
		pages++

		entries, err := listFolder(client, id)
		if err != nil {
			if depth > 0 {
				// A subfolder can be private while the folder holding it is
				// public; skip that one instead of losing the whole batch.
				return nil
			}
			return err
		}

		for _, e := range entries {
			if e.isFolder {
				if err := walk(e.id, depth+1); err != nil {
					return err
				}
				continue
			}
			files = append(files, FileEntry{Name: e.name, URL: DownloadURL(e.id)})
			if len(files) >= maxFolderFiles {
				return nil
			}
		}
		return nil
	}

	if err := walk(rootID, 0); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no files found in the Google Drive folder")
	}
	return files, nil
}

func listFolder(client *http.Client, folderID string) ([]driveEntry, error) {
	req, err := http.NewRequest("GET", folderPageBase+url.PathEscape(folderID)+"?hl=en", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to open the Google Drive folder: %v", err)
	}
	defer resp.Body.Close()

	if final := resp.Request.URL; final != nil && strings.Contains(final.Host, "accounts.google.com") {
		return nil, ErrNotPublic
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return nil, ErrNotPublic
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google Drive returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read the Google Drive folder page: %v", err)
	}
	return parseFolderPage(string(body))
}

// driveIVDRe matches the inline script the folder page ships its listing in:
// window['_DRIVE_ivd'] = '[[[\"id\",[\"parent\"],\"name\",\"mime\"]]]';
var driveIVDRe = regexp.MustCompile(`window\['_DRIVE_ivd'\]\s*=\s*'((?:[^'\\]|\\.)*)'`)

func parseFolderPage(page string) ([]driveEntry, error) {
	m := driveIVDRe.FindStringSubmatch(page)
	if len(m) < 2 {
		if strings.Contains(page, "accounts.google.com") || strings.Contains(page, "ServiceLogin") {
			return nil, ErrNotPublic
		}
		return nil, fmt.Errorf("could not read the file list from the Google Drive folder page")
	}

	var root any
	if err := json.Unmarshal([]byte(unescapeJS(m[1])), &root); err != nil {
		return nil, fmt.Errorf("failed to parse the Google Drive folder listing: %v", err)
	}

	var entries []driveEntry
	collectEntries(root, &entries)
	return entries, nil
}

// unescapeJS turns the JavaScript string literal Drive wraps the listing in back
// into the JSON text it was made from (\xNN, \uNNNN and the usual escapes).
func unescapeJS(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'x':
			if i+2 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
					b.WriteByte(byte(v))
					i += 2
					continue
				}
			}
			b.WriteByte('x')
		case 'u':
			if i+4 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
					b.WriteRune(rune(v))
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		default:
			// Covers \\ (a backslash the JSON text needs) and \" / \'.
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// collectEntries walks the nested arrays Drive ships and picks the file rows
// out of them. Everything else in there is display metadata.
func collectEntries(node any, out *[]driveEntry) {
	list, ok := node.([]any)
	if !ok {
		return
	}
	if entry, skip, matched := asDriveEntry(list); matched {
		if !skip {
			*out = append(*out, entry)
		}
		return
	}
	for _, child := range list {
		collectEntries(child, out)
	}
}

// asDriveEntry recognises a row shaped [id, [parent ids], name, mimeType, ...].
func asDriveEntry(list []any) (entry driveEntry, skip, matched bool) {
	if len(list) < 4 {
		return driveEntry{}, false, false
	}
	id, ok := list[0].(string)
	if !ok {
		return driveEntry{}, false, false
	}
	if _, ok := list[1].([]any); !ok {
		return driveEntry{}, false, false
	}
	name, ok := list[2].(string)
	if !ok {
		return driveEntry{}, false, false
	}
	mime, ok := list[3].(string)
	if !ok || !strings.HasPrefix(mime, "application/") {
		return driveEntry{}, false, false
	}

	if mime == folderMIME {
		return driveEntry{id: id, name: name, isFolder: true}, false, true
	}
	if strings.HasPrefix(mime, "application/vnd.google-apps.") {
		// Docs, Sheets and Forms have no archive to download.
		return driveEntry{}, true, true
	}
	return driveEntry{id: id, name: name}, false, true
}
