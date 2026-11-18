package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var currentVersion = "3.1.1"

const (
	githubReleasesURL = "https://api.github.com/repos/KyarottoOwO/mew/releases/latest"
	// updateAssetName is the build inside the release, updaterName is the
	// helper that swaps it in for the running one.
	updateAssetName = "mew.exe"
	updaterName     = "update.exe"
)

var (
	// updateClient fetches release metadata. downloadClient streams the
	// updater build and only bounds the wait for headers, so a slow connection
	// is not cut off mid-transfer (http.Get had no bounds at all, so a stalled
	// server hung the update forever).
	updateClient   = &http.Client{Timeout: 10 * time.Second}
	downloadClient = &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 30 * time.Second,
	}}
)

// versionNumbers splits "1.2.3" (with an optional v prefix or -suffix) into
// comparable numbers.
func versionNumbers(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(v)), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		nums = append(nums, n)
	}
	return nums, true
}

// versionNewer reports whether latest is a higher version than current.
// Comparing the strings for inequality offered a downgrade whenever the latest
// release tag was older than the running build (or "1.9" vs "1.10"); tags that
// do not parse fall back to inequality so unusual suffixes still notify.
func versionNewer(latest, current string) bool {
	l, okL := versionNumbers(latest)
	c, okC := versionNumbers(current)
	if !okL || !okC {
		return latest != current
	}
	for i := 0; i < len(l) || i < len(c); i++ {
		var a, b int
		if i < len(l) {
			a = l[i]
		}
		if i < len(c) {
			b = c[i]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

type ghAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Digest      string `json:"digest"` // "sha256:<hex>"
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

func (r *ghRelease) asset(name string) *ghAsset {
	for i := range r.Assets {
		if strings.EqualFold(r.Assets[i].Name, name) {
			return &r.Assets[i]
		}
	}
	return nil
}

// releaseAPI is where the update check looks. MEW_RELEASE_API points it at a
// local fake, so the whole update flow can be run without publishing a
// release; everything except loopback assets is still refused below.
func releaseAPI() string {
	if v := strings.TrimSpace(os.Getenv("MEW_RELEASE_API")); v != "" {
		return v
	}
	return githubReleasesURL
}

// fetchLatestRelease reads the newest published release. A nil release with no
// error means the repository has no release to install yet.
func fetchLatestRelease() (*ghRelease, error) {
	req, err := http.NewRequest(http.MethodGet, releaseAPI(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mew-updater")

	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("checking for a release failed: %s", resp.Status)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("reading the release: %w", err)
	}
	// A release without a build and an updater can't be installed, so it is
	// nothing to offer.
	if rel.asset(updateAssetName) == nil || rel.asset(updaterName) == nil {
		return nil, nil
	}
	return &rel, nil
}

type UpdateInfo struct {
	NeedsUpdate   bool   `json:"needsUpdate"`
	LatestVersion string `json:"latestVersion"`
}

type ChangelogEntry struct {
	Tag  string `json:"tag"`
	URL  string `json:"url"`
	Date string `json:"date"`
	Body string `json:"body"`
}

func (a *App) GetVersion() string {
	return currentVersion
}

func (a *App) CheckForUpdate() UpdateInfo {
	rel, err := fetchLatestRelease()
	if err != nil || rel == nil {
		return UpdateInfo{NeedsUpdate: false}
	}

	latestVersion := strings.TrimPrefix(rel.TagName, "v")
	if latestVersion == "" || !versionNewer(latestVersion, currentVersion) {
		return UpdateInfo{NeedsUpdate: false}
	}

	return UpdateInfo{NeedsUpdate: true, LatestVersion: latestVersion}
}

// updaterDir is where update.exe is kept: %LOCALAPPDATA%\mew, next to the
// settings, so nothing extra lands beside mew.exe. The updater gets MEW's path
// as an argument, so it can run from there. Without a data folder it falls
// back to the folder the app runs from.
func updaterDir(exeDir string) string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		return filepath.Join(base, "mew")
	}
	return exeDir
}

// allowedUpdateURL reports whether a download URL is a GitHub release asset, the
// only place MEW's own updates come from. The URL arrives from a release, so it
// is validated rather than trusted.
func allowedUpdateURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme == "https" {
		return host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com")
	}
	// A fake release (MEW_RELEASE_API) serves its assets from loopback only.
	if u.Scheme == "http" && os.Getenv("MEW_RELEASE_API") != "" {
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	return false
}

// ensureUpdater returns the path of update.exe in dir, downloading it from the
// release first when it isn't there.
func ensureUpdater(rel *ghRelease, dir string) (string, error) {
	path := filepath.Join(dir, updaterName)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	asset := rel.asset(updaterName)
	if asset == nil {
		return "", fmt.Errorf("%s is missing and release %s doesn't include it", updaterName, rel.TagName)
	}
	if !allowedUpdateURL(asset.DownloadURL) {
		return "", errors.New("the updater is not published on GitHub")
	}

	resp, err := downloadClient.Get(asset.DownloadURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("downloaded updater is empty")
	}
	sum := sha256.Sum256(data)
	if want := strings.TrimPrefix(strings.ToLower(asset.Digest), "sha256:"); want != "" && want != hex.EncodeToString(sum[:]) {
		return "", errors.New("downloaded updater does not match its published checksum")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// StartUpdate hands the update to update.exe, which downloads the new build,
// swaps it in and starts MEW again. MEW quits so its executable can be
// replaced: a running Windows executable can't overwrite itself.
func (a *App) StartUpdate() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not find the running executable: %v", err)
	}

	rel, err := fetchLatestRelease()
	if err != nil {
		return err
	}
	if rel == nil {
		return errors.New("no newer release is available")
	}
	// The banner only offers a newer build, but the updater would happily
	// install an older one if the check went stale.
	if !versionNewer(strings.TrimPrefix(rel.TagName, "v"), currentVersion) {
		return fmt.Errorf("MEW is already up to date (v%s)", currentVersion)
	}

	updater, err := ensureUpdater(rel, updaterDir(filepath.Dir(exe)))
	if err != nil {
		return fmt.Errorf("could not get the updater: %v", err)
	}

	a.logDebug("StartUpdate: handing over to " + updater)
	if _, err := os.StartProcess(updater, []string{updater, exe}, &os.ProcAttr{
		Dir:   filepath.Dir(exe),
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}); err != nil {
		return fmt.Errorf("could not start the updater: %v", err)
	}

	// Give the updater a moment to start, then get out of the way.
	go func() {
		time.Sleep(300 * time.Millisecond)
		wailsRuntime.Quit(a.ctx)
	}()
	return nil
}

func (a *App) GetChangelog() []ChangelogEntry {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/KyarottoOwO/mew/releases", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := updateClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var releases []struct {
		TagName   string `json:"tag_name"`
		HTMLURL   string `json:"html_url"`
		Published string `json:"published_at"`
		Body      string `json:"body"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil
	}

	var entries []ChangelogEntry
	for _, r := range releases {
		date := ""
		if t, err := time.Parse(time.RFC3339, r.Published); err == nil {
			date = t.Format("Jan 02, 2006")
		}
		entries = append(entries, ChangelogEntry{
			Tag:  strings.TrimPrefix(r.TagName, "v"),
			URL:  r.HTMLURL,
			Date: date,
			Body: r.Body,
		})
	}
	return entries
}

func (a *App) OpenFolder(dir string) {
	stat, err := os.Stat(dir)
	if err == nil && !stat.IsDir() {
		// Opening a file directly would launch it (or worse, run an .exe).
		// Reveal it inside its folder instead.
		switch runtime.GOOS {
		case "windows":
			exec.Command("explorer", "/select,\""+dir+"\"").Start()
			return
		case "darwin":
			exec.Command("open", "-R", dir).Start()
			return
		default:
			dir = filepath.Dir(dir)
		}
	}
	switch runtime.GOOS {
	case "windows":
		exec.Command("explorer", dir).Start()
	case "darwin":
		exec.Command("open", dir).Start()
	default:
		exec.Command("xdg-open", dir).Start()
	}
}

func (a *App) openInBrowser(rawURL string) {
	u := strings.ToLower(strings.TrimSpace(rawURL))
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return
	}
	wailsRuntime.BrowserOpenURL(a.ctx, rawURL)
}

func (a *App) OpenDiscordLink() {
	a.openInBrowser("https://discord.gg/nv9GrqTVM3")
}

func (a *App) OpenMewDataDir() {
	a.OpenFolder(filepath.Join(os.Getenv("LOCALAPPDATA"), "mew"))
}

func (a *App) OpenDonationLink() {
	a.openInBrowser("https://www.paypal.me/spotters1")
}

func (a *App) OpenDownloadLink(url string) {
	a.openInBrowser(url)
}
