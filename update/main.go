// Command update installs the latest MEW release next to itself, then starts
// MEW. It is meant to be what the user launches: whatever goes wrong while
// checking or downloading (offline, GitHub down, no release yet), MEW still
// starts with the version already installed.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	ExeName = "mew.exe"
	// LatestReleaseAPI answers 404 when the repository has no release yet,
	// instead of redirecting to the releases page like /releases/latest does.
	LatestReleaseAPI = "https://api.github.com/repos/MewishMC/mew/releases/latest"
)

// httpClient is bounded so a slow or unreachable GitHub delays MEW by seconds
// rather than forever.
var httpClient = &http.Client{Timeout: 30 * time.Second}

var (
	user32     = syscall.NewLazyDLL("user32.dll")
	messageBox = user32.NewProc("MessageBoxW")
)

func showError(title, message string) {
	_, _, _ = messageBox.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(message))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		0x00000010, // MB_ICONERROR
	)
}

func main() {
	exe := targetPath(os.Args)

	// MEW_RELEASE_API points at a local fake to try updates without publishing.
	api := LatestReleaseAPI
	if v := os.Getenv("MEW_RELEASE_API"); v != "" {
		api = v
	}
	if updated, err := update(api, exe); err != nil {
		fmt.Println("Update skipped:", err)
	} else if updated {
		fmt.Println("Installed the latest MEW.")
	} else {
		fmt.Println("MEW is up to date.")
	}

	if err := launch(exe); err != nil {
		showError("MEW could not start", fmt.Sprintf("Could not start %s:\n\n%v", exe, err))
		os.Exit(1)
	}
	removeBackup(exe)
}

// targetPath is the MEW executable to update: the first argument if given,
// otherwise mew.exe in the updater's own folder (not the working directory,
// which is wherever a shortcut happened to start in).
func targetPath(args []string) string {
	if len(args) > 1 && args[1] != "" {
		return args[1]
	}
	self, err := os.Executable()
	if err != nil {
		return ExeName
	}
	return filepath.Join(filepath.Dir(self), ExeName)
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"browser_download_url"`
		Digest      string `json:"digest"` // "sha256:<hex>"
	} `json:"assets"`
}

// update brings exe in line with the latest release. It reports whether a new
// file was installed.
func update(apiURL, exe string) (bool, error) {
	rel, err := latestRelease(apiURL)
	if err != nil {
		return false, err
	}
	var downloadURL, digest string
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, ExeName) {
			downloadURL, digest = a.DownloadURL, a.Digest
			break
		}
	}
	if downloadURL == "" {
		return false, fmt.Errorf("release %s has no %s asset", rel.TagName, ExeName)
	}

	current, err := fileHash(exe)
	if err != nil {
		return false, err
	}
	want := strings.TrimPrefix(strings.ToLower(digest), "sha256:")
	if want != "" && want == current {
		return false, nil
	}

	data, err := download(downloadURL)
	if err != nil {
		return false, err
	}
	got := hashBytes(data)
	if want != "" && got != want {
		return false, fmt.Errorf("download of %s does not match its published checksum", rel.TagName)
	}
	if got == current {
		return false, nil
	}
	if err := install(exe, data); err != nil {
		return false, err
	}
	return true, nil
}

func latestRelease(apiURL string) (*release, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mew-updater")
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil, errors.New("no release has been published yet")
	case res.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("checking for a release failed: %s", res.Status)
	}
	var rel release
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("reading the release: %w", err)
	}
	return &rel, nil
}

func download(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mew-updater")
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s failed: %s", ExeName, res.Status)
	}
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("downloaded file is empty")
	}
	return data, nil
}

// install replaces exe without ever leaving a half-written file in its place:
// the new build is written beside it, the old one is moved aside (Windows
// allows renaming a running exe, not overwriting it), then the new one takes
// its name. A failure part way puts the old file back.
func install(exe string, data []byte) error {
	next := exe + ".new"
	prev := exe + ".old"
	if err := os.WriteFile(next, data, 0o755); err != nil {
		return err
	}
	_ = os.Remove(prev)
	hadOld := true
	if err := os.Rename(exe, prev); err != nil {
		if !os.IsNotExist(err) {
			_ = os.Remove(next)
			return err
		}
		hadOld = false
	}
	if err := os.Rename(next, exe); err != nil {
		if hadOld {
			_ = os.Rename(prev, exe)
		}
		_ = os.Remove(next)
		return err
	}
	// Best effort: this fails while the old MEW is still running, and the next
	// update clears it instead.
	_ = os.Remove(prev)
	return nil
}

// removeBackup deletes the .old copy the swap left behind. Windows keeps a
// running image locked, so this waits for the old MEW to exit instead of
// leaving ~17 MB beside the app; the next update clears anything still stuck.
func removeBackup(exe string) {
	prev := exe + ".old"
	for i := 0; i < 20; i++ {
		if err := os.Remove(prev); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// fileHash hashes the installed executable, or returns "" when there is
// nothing installed yet so any release counts as newer.
func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return hashBytes(data), nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// launch starts MEW with any extra arguments the updater was given.
func launch(exe string) error {
	args := []string{exe}
	if len(os.Args) > 2 {
		args = append(args, os.Args[2:]...)
	}
	_, err := os.StartProcess(exe, args, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Env:   os.Environ(),
		Dir:   filepath.Dir(exe),
	})
	return err
}
