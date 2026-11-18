package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGitHub serves a latest-release document and the asset it points at.
// A status other than 200 makes the release endpoint answer with it.
type fakeGitHub struct {
	status    int
	asset     []byte
	digest    string // overrides the real digest when set
	noAsset   bool
	downloads atomic.Int32
}

func (f *fakeGitHub) server(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			if f.status != 0 && f.status != http.StatusOK {
				w.WriteHeader(f.status)
				return
			}
			if f.noAsset {
				fmt.Fprint(w, `{"tag_name":"v9","assets":[{"name":"notes.txt","browser_download_url":"x"}]}`)
				return
			}
			digest := f.digest
			if digest == "" {
				digest = "sha256:" + hashBytes(f.asset)
			}
			fmt.Fprintf(w, `{"tag_name":"v9","assets":[{"name":"mew.exe","browser_download_url":"%s/download/mew.exe","digest":"%s"}]}`, srv.URL, digest)
		case "/download/mew.exe":
			f.downloads.Add(1)
			_, _ = w.Write(f.asset)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func installed(t *testing.T, content string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), ExeName)
	if content != "" {
		if err := os.WriteFile(exe, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return exe
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoLeftovers(t *testing.T, exe string) {
	t.Helper()
	for _, extra := range []string{exe + ".new", exe + ".old"} {
		if _, err := os.Stat(extra); !os.IsNotExist(err) {
			t.Errorf("%s left behind", filepath.Base(extra))
		}
	}
}

func TestUpdateInstallsNewerRelease(t *testing.T) {
	gh := &fakeGitHub{asset: []byte("mew v9")}
	srv := gh.server(t)
	exe := installed(t, "mew v8")

	updated, err := update(srv.URL+"/latest", exe)
	if err != nil || !updated {
		t.Fatalf("update = %v, %v; want true, nil", updated, err)
	}
	if got := readFile(t, exe); got != "mew v9" {
		t.Errorf("installed %q, want the release", got)
	}
	assertNoLeftovers(t, exe)
}

func TestUpdateSkipsDownloadWhenAlreadyCurrent(t *testing.T) {
	gh := &fakeGitHub{asset: []byte("mew v9")}
	srv := gh.server(t)
	exe := installed(t, "mew v9")

	updated, err := update(srv.URL+"/latest", exe)
	if err != nil || updated {
		t.Fatalf("update = %v, %v; want false, nil", updated, err)
	}
	if n := gh.downloads.Load(); n != 0 {
		t.Errorf("downloaded %d times, want 0 when the digest already matches", n)
	}
}

func TestUpdateInstallsWhenNothingIsInstalled(t *testing.T) {
	gh := &fakeGitHub{asset: []byte("mew v9")}
	srv := gh.server(t)
	exe := installed(t, "")

	if updated, err := update(srv.URL+"/latest", exe); err != nil || !updated {
		t.Fatalf("update = %v, %v; want true, nil", updated, err)
	}
	if got := readFile(t, exe); got != "mew v9" {
		t.Errorf("installed %q", got)
	}
	assertNoLeftovers(t, exe)
}

// Every failure must leave the installed MEW untouched so it can still start.
func TestUpdateFailuresKeepInstalledMew(t *testing.T) {
	cases := map[string]*fakeGitHub{
		"no release yet":  {status: http.StatusNotFound},
		"rate limited":    {status: http.StatusForbidden},
		"no mew.exe asset": {noAsset: true},
		"checksum mismatch": {asset: []byte("tampered"), digest: "sha256:" + hashBytes([]byte("mew v9"))},
		"empty download":  {asset: []byte{}, digest: "sha256:abc"},
	}
	for name, gh := range cases {
		t.Run(name, func(t *testing.T) {
			srv := gh.server(t)
			exe := installed(t, "mew v8")
			updated, err := update(srv.URL+"/latest", exe)
			if err == nil || updated {
				t.Fatalf("update = %v, %v; want an error", updated, err)
			}
			if got := readFile(t, exe); got != "mew v8" {
				t.Errorf("installed file changed to %q", got)
			}
			assertNoLeftovers(t, exe)
		})
	}
}

func TestUpdateUnreachableServer(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/latest"
	srv.Close()
	exe := installed(t, "mew v8")
	if _, err := update(url, exe); err == nil {
		t.Fatal("want an error when GitHub can't be reached")
	}
	if got := readFile(t, exe); got != "mew v8" {
		t.Errorf("installed file changed to %q", got)
	}
}

func TestRemoveBackupClearsTheReplacedBuild(t *testing.T) {
	exe := installed(t, "mew v8")
	prev := exe + ".old"
	if err := os.WriteFile(prev, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	removeBackup(exe)
	if _, err := os.Stat(prev); !os.IsNotExist(err) {
		t.Error("the replaced build was left behind")
	}
}

// The old MEW holds its image for a moment after the swap, so the backup must
// be cleared once it lets go instead of being left beside the app.
func TestRemoveBackupWaitsForTheOldBuild(t *testing.T) {
	exe := installed(t, "mew v8")
	prev := exe + ".old"
	if err := os.WriteFile(prev, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(prev)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(400 * time.Millisecond)
		_ = held.Close()
	}()
	removeBackup(exe)
	if _, err := os.Stat(prev); !os.IsNotExist(err) {
		t.Error("the replaced build was left behind after the lock was released")
	}
}

func TestTargetPathDefaultsToUpdaterFolder(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	if got, want := targetPath([]string{"update.exe"}), filepath.Join(filepath.Dir(self), ExeName); got != want {
		t.Errorf("targetPath = %q, want %q", got, want)
	}
	if got := targetPath([]string{"update.exe", `C:\x\mew.exe`}); got != `C:\x\mew.exe` {
		t.Errorf("explicit path ignored: %q", got)
	}
}
