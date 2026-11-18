package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Header:        http.Header{"Content-Type": {"application/json"}},
	}
}

// withUpdateClient swaps the metadata client for the duration of a test.
func withUpdateClient(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	old := updateClient
	updateClient = &http.Client{Transport: rt, Timeout: 5 * time.Second}
	t.Cleanup(func() { updateClient = old })
}

func withDownloadClient(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	old := downloadClient
	downloadClient = &http.Client{Transport: rt, Timeout: 5 * time.Second}
	t.Cleanup(func() { downloadClient = old })
}

// withDownloadsDir points the updater at a temp folder instead of the user's
// real Downloads.
func withDownloadsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := downloadsDir
	downloadsDir = func() string { return dir }
	t.Cleanup(func() { downloadsDir = old })
	return dir
}

func releaseJSON(tag string, assetName string) string {
	return `{"tag_name":"` + tag + `","assets":[{"name":"` + assetName + `","browser_download_url":"https://github.com/KyarottoOwO/mew/releases/download/` + tag + `/` + assetName + `"}]}`
}

func TestCheckForUpdateFindsNewerRelease(t *testing.T) {
	a, _, _ := featureApp(t)
	withUpdateClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.github.com" && strings.HasSuffix(req.URL.Path, "/latest") {
			return jsonResponse(200, releaseJSON("v9.9.9", "mew.exe")), nil
		}
		return jsonResponse(404, `{}`), nil
	}))

	info := a.CheckForUpdate()
	if !info.NeedsUpdate || info.LatestVersion != "9.9.9" {
		t.Fatalf("CheckForUpdate = %+v", info)
	}
	if !strings.HasSuffix(info.DownloadURL, "mew.exe") {
		t.Errorf("download URL = %q, want the exe asset", info.DownloadURL)
	}
}

// TestCheckForUpdateNeverOffersADowngrade: an older (or equal) release tag must
// not prompt an update.
func TestCheckForUpdateNeverOffersADowngrade(t *testing.T) {
	a, _, _ := featureApp(t)
	for _, tag := range []string{"v0.9.0", "v" + currentVersion, currentVersion} {
		withUpdateClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, releaseJSON(tag, "mew.exe")), nil
		}))
		if info := a.CheckForUpdate(); info.NeedsUpdate {
			t.Errorf("tag %q offered an update to %q from %q", tag, info.LatestVersion, currentVersion)
		}
	}

	// Versions compare as numbers, not text: 1.10.0 is newer than 1.1.1.
	if !versionNewer("1.10.0", "1.1.1") {
		t.Error("1.10.0 should compare newer than 1.1.1")
	}

	// A release one build past the running version is offered, whatever the
	// current version number happens to be.
	newerTag := "v" + currentVersion + ".1"
	withUpdateClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(200, releaseJSON(newerTag, "mew.exe")), nil
	}))
	if info := a.CheckForUpdate(); !info.NeedsUpdate {
		t.Errorf("%s should be offered over %s", newerTag, currentVersion)
	}
}

func TestCheckForUpdateHandlesFailures(t *testing.T) {
	a, _, _ := featureApp(t)

	withUpdateClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(403, `{"message":"rate limited"}`), nil
	}))
	if info := a.CheckForUpdate(); info.NeedsUpdate {
		t.Errorf("an HTTP 403 was treated as an update: %+v", info)
	}

	withUpdateClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, `not json`), nil
	}))
	if info := a.CheckForUpdate(); info.NeedsUpdate {
		t.Errorf("a malformed body was treated as an update: %+v", info)
	}

	// No exe asset: the UI still learns about the version but has no download.
	withUpdateClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"tag_name":"v9.9.9","assets":[{"name":"mew.dmg","browser_download_url":"https://github.com/x/y.dmg"}]}`), nil
	}))
	info := a.CheckForUpdate()
	if !info.NeedsUpdate || info.DownloadURL != "" {
		t.Errorf("CheckForUpdate without an exe = %+v", info)
	}
}

func TestGetChangelogParsesReleases(t *testing.T) {
	a, _, _ := featureApp(t)
	withUpdateClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, `[
			{"tag_name":"v1.2.0","html_url":"https://github.com/x/1.2.0","published_at":"2026-02-03T10:00:00Z","body":"- things"},
			{"tag_name":"1.1.0","html_url":"https://github.com/x/1.1.0","published_at":"not a date","body":"- older"}
		]`), nil
	}))

	entries := a.GetChangelog()
	if len(entries) != 2 {
		t.Fatalf("GetChangelog returned %d entries", len(entries))
	}
	if entries[0].Tag != "1.2.0" || entries[0].Date != "Feb 03, 2026" {
		t.Errorf("entry = %+v", entries[0])
	}
	if entries[1].Date != "" {
		t.Errorf("an unparsable date should stay empty, got %q", entries[1].Date)
	}
}

func TestDownloadUpdateSavesInstaller(t *testing.T) {
	a, _, _ := featureApp(t)
	dir := withDownloadsDir(t)
	payload := []byte("installer bytes")
	withDownloadClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    200,
			Status:        "200 OK",
			Body:          io.NopCloser(bytes.NewReader(payload)),
			ContentLength: int64(len(payload)),
			Header:        http.Header{},
			Request:       req,
		}, nil
	}))

	got, err := a.DownloadUpdate("https://github.com/KyarottoOwO/mew/releases/download/v9.9.9/mew.exe")
	if err != nil {
		t.Fatalf("DownloadUpdate: %v", err)
	}
	if !samePath(got, filepath.Join(dir, "mew.exe")) {
		t.Fatalf("saved to %q", got)
	}
	saved, err := os.ReadFile(got)
	if err != nil || !bytes.Equal(saved, payload) {
		t.Fatalf("saved file = %q, %v", saved, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mew.exe.part")); !os.IsNotExist(err) {
		t.Error("the partial download was left behind")
	}
}

// TestDownloadUpdateKeepsPreviousInstaller: a failed download must not destroy
// the copy the user already had.
func TestDownloadUpdateKeepsPreviousInstaller(t *testing.T) {
	a, _, _ := featureApp(t)
	dir := withDownloadsDir(t)
	existing := filepath.Join(dir, "mew.exe")
	if err := os.WriteFile(existing, []byte("old installer"), 0644); err != nil {
		t.Fatal(err)
	}

	withDownloadClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Status: "500", Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	}))
	if _, err := a.DownloadUpdate("https://github.com/KyarottoOwO/mew/releases/download/v9/mew.exe"); err == nil {
		t.Fatal("an HTTP 500 should fail")
	}

	withDownloadClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200", Body: io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF)), Header: http.Header{}, Request: req}, nil
	}))
	if _, err := a.DownloadUpdate("https://github.com/KyarottoOwO/mew/releases/download/v9/mew.exe"); err == nil {
		t.Fatal("a broken body should fail")
	}

	saved, err := os.ReadFile(existing)
	if err != nil || string(saved) != "old installer" {
		t.Fatalf("the previous installer was touched: %q, %v", saved, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mew.exe.part")); !os.IsNotExist(err) {
		t.Error("the failed partial download was left behind")
	}
}

func TestDownloadUpdateRejectsUntrustedURLs(t *testing.T) {
	a, _, _ := featureApp(t)
	dir := withDownloadsDir(t)
	withDownloadClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("an untrusted URL must never be fetched")
		return nil, nil
	}))

	for _, u := range []string{
		"",
		"   ",
		"http://github.com/x/mew.exe",
		"https://evil.example.com/mew.exe",
		"https://github.com.evil.example/mew.exe",
		"file:///C:/Windows/system32/calc.exe",
	} {
		if _, err := a.DownloadUpdate(u); err == nil {
			t.Errorf("DownloadUpdate(%q) should be refused", u)
		}
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("a refused download wrote files: %v", entries)
	}
}

func TestInstallUpdateValidation(t *testing.T) {
	a, _, _ := featureApp(t)
	dir := withDownloadsDir(t)

	if err := a.InstallUpdate(""); err == nil {
		t.Error("an empty path should be refused")
	}
	if err := a.InstallUpdate(filepath.Join(dir, "missing.exe")); err == nil {
		t.Error("a missing file should be refused")
	}

	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallUpdate(txt); err == nil {
		t.Error("a .txt should not be run as an installer")
	}

	elsewhere := t.TempDir()
	exe := filepath.Join(elsewhere, "mew.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallUpdate(exe); err == nil {
		t.Error("an installer outside Downloads should be refused")
	}

	if err := a.InstallUpdate(dir); err == nil {
		t.Error("a directory should be refused")
	}
}
