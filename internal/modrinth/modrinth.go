// Package modrinth downloads packs from Modrinth project links. Modrinth needs
// no account: its API is public and the files live on a plain CDN, so a pasted
// project page is enough to fetch the newest release.
package modrinth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxDownloadBytes caps a single download, so a broken or hostile answer cannot
// stream until the desktop app runs out of memory.
const maxDownloadBytes = 512 << 20 // 512 MiB

// userAgent identifies us to Modrinth, which asks for a descriptive one.
const userAgent = "MewPackPorter (github.com/kyarottoOwO/mew)"

// apiBase is a variable so tests can aim at a local server.
var apiBase = "https://api.modrinth.com/v2"

// projectTypes are the Modrinth page paths this app will port. "mod" and
// "datapack" are left out on purpose: Bedrock can run neither, so those links
// are refused instead of downloaded. "project" is the slug-free form
// (modrinth.com/project/<id>), whose real type is only known from the API.
var projectTypes = map[string]bool{
	"resourcepack": true,
	"project":      true,
}

// unportableTypes are the page paths that name something Bedrock cannot use.
var unportableTypes = map[string]bool{
	"mod":      true,
	"datapack": true,
}

// portTypes are the Modrinth project types that can actually be ported.
var portTypes = map[string]bool{
	"resourcepack": true,
}

// IsModrinthURL reports whether rawURL points at a Modrinth project page rather
// than at a search, user or collection page.
func IsModrinthURL(rawURL string) bool {
	_, ok := projectSlug(rawURL)
	return ok
}

// UnsupportedType reports the project type in rawURL when it names something
// this app cannot port (a mod or a datapack), so the caller can say plainly why
// instead of handing the porter a file it would only reject later.
func UnsupportedType(rawURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Host) {
	case "modrinth.com", "www.modrinth.com":
	default:
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	typ := strings.ToLower(parts[0])
	if !unportableTypes[typ] {
		return "", false
	}
	return typ, true
}

// UnportableError explains why a mod or datapack cannot become a Bedrock pack.
func UnportableError(typ string) error {
	return fmt.Errorf("Modrinth %ss can't be ported to Bedrock — paste a resource pack link", typ)
}

// projectSlug pulls the project slug out of a Modrinth project URL.
func projectSlug(rawURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Host) {
	case "modrinth.com", "www.modrinth.com":
	default:
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	if !projectTypes[strings.ToLower(parts[0])] {
		return "", false
	}
	// Modrinth slugs are lowercase but project IDs are not, so the segment is
	// kept exactly as pasted: lowercasing an ID turns a live project into a 404.
	slug := strings.TrimSpace(parts[1])
	if slug == "" {
		return "", false
	}
	return slug, true
}

type project struct {
	ProjectType string `json:"project_type"`
}

type versionFile struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
}

type version struct {
	DatePublished string        `json:"date_published"`
	Files         []versionFile `json:"files"`
}

// DownloadProject fetches the newest pack from a Modrinth project link.
func DownloadProject(client *http.Client, rawURL string) ([]byte, string, error) {
	if typ, ok := UnsupportedType(rawURL); ok {
		return nil, "", UnportableError(typ)
	}
	slug, ok := projectSlug(rawURL)
	if !ok {
		return nil, "", fmt.Errorf("not a Modrinth project link")
	}

	info, err := fetchProject(client, slug)
	if err != nil {
		return nil, "", err
	}
	if unportableTypes[info.ProjectType] {
		return nil, "", UnportableError(info.ProjectType)
	}
	if !portTypes[info.ProjectType] {
		return nil, "", fmt.Errorf("that Modrinth project is a %s, not a resource pack", info.ProjectType)
	}

	versions, err := fetchVersions(client, slug)
	if err != nil {
		return nil, "", err
	}
	file, err := newestFile(versions)
	if err != nil {
		return nil, "", err
	}
	if file.Size > maxDownloadBytes {
		return nil, "", fmt.Errorf("%s is larger than %d MB", file.Filename, maxDownloadBytes>>20)
	}

	data, err := fetch(client, file.URL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download %s: %v", file.Filename, err)
	}
	if file.Filename == "" {
		// The CDN URL always ends in the filename, so this should not happen;
		// refusing beats handing a pack to the porter with no extension.
		return nil, "", fmt.Errorf("Modrinth did not report a file name for that project")
	}
	return data, file.Filename, nil
}

func fetchProject(client *http.Client, slug string) (project, error) {
	var info project

	req, err := http.NewRequest("GET", apiBase+"/project/"+url.PathEscape(slug), nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return info, fmt.Errorf("failed to reach Modrinth: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return info, fmt.Errorf("Modrinth has no project at that link")
	}
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("Modrinth returned status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&info); err != nil {
		return info, fmt.Errorf("failed to read Modrinth's answer: %v", err)
	}
	return info, nil
}

func fetchVersions(client *http.Client, slug string) ([]version, error) {
	req, err := http.NewRequest("GET", apiBase+"/project/"+url.PathEscape(slug)+"/version?include_changelog=false", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach Modrinth: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("Modrinth has no project at that link")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Modrinth returned status %d", resp.StatusCode)
	}

	var versions []version
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&versions); err != nil {
		return nil, fmt.Errorf("failed to read Modrinth's answer: %v", err)
	}
	return versions, nil
}

// newestFile picks the primary file of the most recently published version.
func newestFile(versions []version) (versionFile, error) {
	var best *version
	for i := range versions {
		if len(versions[i].Files) == 0 {
			continue
		}
		// date_published is RFC 3339 UTC, so a string compare orders correctly.
		if best == nil || versions[i].DatePublished > best.DatePublished {
			best = &versions[i]
		}
	}
	if best == nil {
		return versionFile{}, fmt.Errorf("that Modrinth project has no downloadable files")
	}

	chosen := best.Files[0]
	for _, f := range best.Files {
		if f.Primary {
			chosen = f
			break
		}
	}
	if chosen.URL == "" {
		return versionFile{}, fmt.Errorf("that Modrinth project has no downloadable files")
	}
	return chosen, nil
}

func fetch(client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxDownloadBytes {
		return nil, fmt.Errorf("file is larger than %d MB", maxDownloadBytes>>20)
	}
	return data, nil
}
