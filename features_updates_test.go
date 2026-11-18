package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// releaseJSON is a release with the build and the updater, the two assets an
// update needs.
func releaseJSON(tag string, digest string) string {
	base := "https://github.com/KyarottoOwO/mew/releases/download/" + tag + "/"
	return `{"tag_name":"` + tag + `","assets":[` +
		`{"name":"mew.exe","browser_download_url":"` + base + `mew.exe","digest":"` + digest + `"},` +
		`{"name":"update.exe","browser_download_url":"` + base + `update.exe","digest":""}]}`
}

func TestCheckForUpdateFindsNewerRelease(t *testing.T) {
	a, _, _ := featureApp(t)
	withUpdateClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.github.com" && strings.HasSuffix(req.URL.Path, "/latest") {
			return jsonResponse(200, releaseJSON("v9.9.9", "")), nil
		}
		return jsonResponse(404, `{}`), nil
	}))

	info := a.CheckForUpdate()
	if !info.NeedsUpdate || info.LatestVersion != "9.9.9" {
		t.Fatalf("CheckForUpdate = %+v", info)
	}
}

// TestCheckForUpdateNeverOffersADowngrade: an older (or equal) release tag must
// not prompt an update.
func TestCheckForUpdateNeverOffersADowngrade(t *testing.T) {
	a, _, _ := featureApp(t)
	for _, tag := range []string{"v0.9.0", "v" + currentVersion, currentVersion} {
		withUpdateClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, releaseJSON(tag, "")), nil
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
		return jsonResponse(200, releaseJSON(newerTag, "")), nil
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

	// No release at all: nothing to offer.
	withUpdateClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(404, `{}`), nil
	}))
	if info := a.CheckForUpdate(); info.NeedsUpdate {
		t.Errorf("a missing release was treated as an update: %+v", info)
	}
}

// TestCheckForUpdateNeedsAnInstallableRelease: a release without the updater
// can't replace a running MEW, so it must not be offered as one click away.
func TestCheckForUpdateNeedsAnInstallableRelease(t *testing.T) {
	a, _, _ := featureApp(t)
	base := "https://github.com/KyarottoOwO/mew/releases/download/v99.9.9/"
	for _, assets := range []string{
		`{"name":"mew.exe","browser_download_url":"` + base + `mew.exe"}`,
		`{"name":"update.exe","browser_download_url":"` + base + `update.exe"}`,
	} {
		withUpdateClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(200, `{"tag_name":"v99.9.9","assets":[`+assets+`]}`), nil
		}))
		if info := a.CheckForUpdate(); info.NeedsUpdate {
			t.Errorf("a release lacking an asset was offered: %+v", info)
		}
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

func updaterRelease(digest string) *ghRelease {
	return &ghRelease{
		TagName: "v9.9.9",
		Assets: []ghAsset{{
			Name:        updaterName,
			DownloadURL: "https://github.com/KyarottoOwO/mew/releases/download/v9.9.9/" + updaterName,
			Digest:      digest,
		}},
	}
}

func TestEnsureUpdaterDownloadsWhenMissing(t *testing.T) {
	payload := []byte("updater build")
	sum := sha256.Sum256(payload)
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

	dir := t.TempDir()
	path, err := ensureUpdater(updaterRelease("sha256:"+hex.EncodeToString(sum[:])), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(path, filepath.Join(dir, updaterName)) {
		t.Fatalf("saved to %q", path)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("updater content = %q, %v", data, err)
	}

	// A wrong checksum must not leave a file behind.
	other := t.TempDir()
	if _, err := ensureUpdater(updaterRelease("sha256:00"), other); err == nil {
		t.Error("checksum mismatch: want an error")
	}
	if entries, err := os.ReadDir(other); err != nil || len(entries) != 0 {
		t.Errorf("mismatched updater was written: %v", entries)
	}
}

func TestEnsureUpdaterRejectsNonGitHubURL(t *testing.T) {
	withDownloadClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("an untrusted URL must never be fetched")
		return nil, nil
	}))
	rel := updaterRelease("")
	rel.Assets[0].DownloadURL = "https://evil.example.com/update.exe"
	if _, err := ensureUpdater(rel, t.TempDir()); err == nil {
		t.Error("an updater outside GitHub releases should be refused")
	}
}

func TestEnsureUpdaterKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, updaterName)
	if err := os.WriteFile(existing, []byte("mine"), 0755); err != nil {
		t.Fatal(err)
	}
	path, err := ensureUpdater(&ghRelease{TagName: "v3"}, dir)
	if err != nil || !samePath(path, existing) {
		t.Fatalf("got %q, %v", path, err)
	}
}

func TestEnsureUpdaterNeedsTheAsset(t *testing.T) {
	if _, err := ensureUpdater(&ghRelease{TagName: "v3"}, t.TempDir()); err == nil {
		t.Error("a release without update.exe should fail")
	}
}

func TestUpdaterDirIsDataFolder(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	exeDir := t.TempDir()
	if got, want := updaterDir(exeDir), filepath.Join(base, "mew"); !samePath(got, want) {
		t.Errorf("updaterDir = %q, want %q", got, want)
	}

	t.Setenv("LOCALAPPDATA", "")
	if got := updaterDir(exeDir); !samePath(got, exeDir) {
		t.Errorf("updaterDir without LOCALAPPDATA = %q, want %q", got, exeDir)
	}
}

// TestStartUpdateRefusesAnOlderRelease: the updater would install whatever the
// latest release holds, so an out-of-date check must not start it.
func TestStartUpdateRefusesAnOlderRelease(t *testing.T) {
	a, _, _ := featureApp(t)
	withUpdateClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(200, releaseJSON("v0.1.0", "")), nil
	}))
	if err := a.StartUpdate(); err == nil {
		t.Error("StartUpdate should refuse a release older than the running build")
	}
}

// TestReleaseAPIOverride: MEW_RELEASE_API points the check at a local fake so
// the update flow can be exercised without publishing.
func TestReleaseAPIOverride(t *testing.T) {
	t.Setenv("MEW_RELEASE_API", "http://127.0.0.1:8791/latest")
	if got := releaseAPI(); got != "http://127.0.0.1:8791/latest" {
		t.Errorf("releaseAPI = %q", got)
	}
	t.Setenv("MEW_RELEASE_API", "")
	if got := releaseAPI(); got != githubReleasesURL {
		t.Errorf("releaseAPI without the override = %q", got)
	}
}

// TestAllowedUpdateURLLoopbackOnlyForAFake: the override may fetch from a local
// fake release, and only from loopback.
func TestAllowedUpdateURLLoopbackOnlyForAFake(t *testing.T) {
	t.Setenv("MEW_RELEASE_API", "")
	if allowedUpdateURL("http://127.0.0.1:8791/download/mew.exe") {
		t.Error("loopback http should be refused without the override")
	}

	t.Setenv("MEW_RELEASE_API", "http://127.0.0.1:8791/latest")
	if !allowedUpdateURL("http://127.0.0.1:8791/download/mew.exe") {
		t.Error("loopback should be allowed with the override")
	}
	for _, u := range []string{
		"http://evil.example.com/mew.exe",
		"http://localhost.evil.example/mew.exe",
		"file:///C:/Windows/system32/calc.exe",
	} {
		if allowedUpdateURL(u) {
			t.Errorf("%q should be refused even with the override", u)
		}
	}
}

func TestStartUpdateWithoutAReleaseFails(t *testing.T) {
	a, _, _ := featureApp(t)
	withUpdateClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(404, `{}`), nil
	}))
	if err := a.StartUpdate(); err == nil {
		t.Error("StartUpdate should fail when there is no release")
	}
}
