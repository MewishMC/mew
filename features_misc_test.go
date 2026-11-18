package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- URL porting ---------------------------------------------------------

// TestPortPackFromURL downloads a pack and ports it, the "port from URL" flow.
func TestPortPackFromURL(t *testing.T) {
	a, _, outDir := featureApp(t)
	pack := javaPackZip(t, "from the web")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="Remote Pack.zip"`)
		w.Write(pack)
	}))
	defer srv.Close()

	got, err := a.PortPackFromURL(srv.URL + "/download")
	if err != nil {
		t.Fatalf("PortPackFromURL: %v", err)
	}
	if got == "" {
		t.Fatal("PortPackFromURL returned no file name")
	}
	if _, err := os.Stat(filepath.Join(outDir, "Remote Pack.mcpack")); err != nil {
		t.Fatalf("the ported pack is not on disk: %v", err)
	}

	if _, err := a.PortPackFromURL(""); err == nil {
		t.Error("an empty URL should be refused")
	}
	if _, err := a.PortPackFromURL("http://127.0.0.1:1/nothing.zip"); err == nil {
		t.Error("an unreachable URL should fail")
	}
}

func TestPortPackFromURLRejectsNonArchive(t *testing.T) {
	a, _, _ := featureApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="notes.txt"`)
		w.Write([]byte("just text"))
	}))
	defer srv.Close()

	if _, err := a.PortPackFromURL(srv.URL + "/notes.txt"); err == nil {
		t.Fatal("a downloaded .txt should be refused")
	}
}

// TestPortFolderDirectLink is the Folder Porter's single-file path.
func TestPortFolderDirectLink(t *testing.T) {
	a, _, outDir := featureApp(t)
	a.settings["deleteOriginals"] = false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="Folder Pack.zip"`)
		w.Write(javaPackZip(t, "single"))
	}))
	defer srv.Close()

	if err := a.PortFolder(srv.URL + "/pack"); err != nil {
		t.Fatalf("PortFolder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "Bedrock", "Folder Pack.mcpack")); err != nil {
		t.Fatalf("no ported pack in Bedrock/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "Java", "Folder Pack.zip")); err != nil {
		t.Errorf("the original was not kept in Java/: %v", err)
	}

	// Cancelling when nothing is running is a no-op, not a crash.
	a.CancelPortFolder()
}

// TestPortFolderReportsFailure: when every file fails, the caller must not be
// told the batch succeeded.
func TestPortFolderReportsFailure(t *testing.T) {
	a, _, _ := featureApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="notes.txt"`)
		w.Write([]byte("not a pack"))
	}))
	defer srv.Close()

	err := a.PortFolder(srv.URL + "/notes.txt")
	if err == nil {
		t.Fatal("a batch where nothing ported should return an error")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error = %v", err)
	}
}

func TestPortFolderRejectsEmptyURL(t *testing.T) {
	a, _, _ := featureApp(t)
	if err := a.PortFolder("   "); err == nil {
		t.Fatal("an empty URL should be refused")
	}
}

// --- Vanilla cache -------------------------------------------------------

func seedVanillaCache(t *testing.T, rel string, data []byte) string {
	t.Helper()
	full := filepath.Join(vanillaCacheDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0644); err != nil {
		t.Fatal(err)
	}
	return full
}

// TestVanillaFileFromCache reads a cached vanilla file without any network.
func TestVanillaFileFromCache(t *testing.T) {
	a, _, _ := featureApp(t)
	seedVanillaCache(t, "ui/hud_screen.json", []byte(`{"namespace":"hud"}`))
	seedVanillaCache(t, "ui/_ui_defs.json", []byte(`{"ui_defs":["ui/hud_screen.json"]}`))

	file, err := a.GetVanillaFile("ui/hud_screen.json")
	if err != nil {
		t.Fatalf("GetVanillaFile: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil || !strings.Contains(string(decoded), "hud") {
		t.Fatalf("content = %q, %v", decoded, err)
	}

	for _, bad := range []string{"", "..", "../../secret.json", "ui/../../x.json"} {
		if _, err := a.GetVanillaFile(bad); err == nil {
			t.Errorf("GetVanillaFile(%q) should be refused", bad)
		}
	}

	raw, err := a.GetVanillaFileBytes("ui/hud_screen.json")
	if err != nil || raw == "" {
		t.Fatalf("GetVanillaFileBytes = %.30q, %v", raw, err)
	}

	uiFiles, err := a.GetVanillaUiFiles()
	if err != nil {
		t.Fatalf("GetVanillaUiFiles: %v", err)
	}
	if len(uiFiles) == 0 {
		t.Error("GetVanillaUiFiles returned nothing for a cached file")
	}
}

func TestVanillaCacheStatusAndClear(t *testing.T) {
	a, _, _ := featureApp(t)
	seedVanillaCache(t, "ui/_ui_defs.json", []byte(`{"ui_defs":[]}`))
	seedVanillaCache(t, "ui/hud_screen.json", []byte(`{}`))

	status := a.GetVanillaCacheStatus()
	if status.Cached < 2 {
		t.Errorf("GetVanillaCacheStatus = %+v, want at least 2 cached files", status)
	}
	if !status.Ready {
		t.Errorf("a cache with _ui_defs.json should be ready: %+v", status)
	}

	if err := a.ClearVanillaCache(); err != nil {
		t.Fatalf("ClearVanillaCache: %v", err)
	}
	if _, err := os.Stat(vanillaCacheDir()); !os.IsNotExist(err) {
		t.Error("the vanilla cache folder survived ClearVanillaCache")
	}
	if after := a.GetVanillaCacheStatus(); after.Cached != 0 || after.Ready {
		t.Errorf("status after clearing = %+v", after)
	}
	if err := a.ClearVanillaCache(); err != nil {
		t.Errorf("clearing an already empty cache should be fine: %v", err)
	}
}

// TestGetPackFileNeedsVanillaCache: pack files are resolved through the vanilla
// resource list, so an unknown file is reported instead of read blindly.
func TestGetPackFileNeedsVanillaCache(t *testing.T) {
	a, packsRoot, _ := featureApp(t)
	writeViewerPack(t, packsRoot, "Viewer")

	if _, err := a.GetPackFile("Viewer", "../../manifest.json"); err == nil {
		t.Error("a traversing path should be refused")
	}
	if _, err := a.GetPackFile("Viewer", "ui/not_cached_anywhere.json"); err == nil {
		t.Error("a file outside the vanilla list should not be read")
	}

	// With the file in the vanilla cache, the pack's own override is returned.
	seedVanillaCache(t, "ui/hud_screen.json", []byte(`{}`))
	file, err := a.GetPackFile("Viewer", "ui/hud_screen.json")
	if err != nil {
		t.Fatalf("GetPackFile: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil || !strings.Contains(string(decoded), "hud") {
		t.Fatalf("pack file content = %q, %v", decoded, err)
	}
}

// --- Discord presence ---------------------------------------------------

// TestSetDiscordActivityDoesNotBlock: the UI pushes presence on every page
// change, so the call must stay cheap and safe with no Discord client running.
func TestSetDiscordActivityDoesNotBlock(t *testing.T) {
	a, _, _ := featureApp(t)
	defer a.stopDiscordRPC()

	done := make(chan struct{})
	go func() {
		a.SetDiscordActivity("Porting a pack", "Pack Porter")
		a.SetDiscordActivity("Porting a pack", "Pack Porter") // duplicate: no-op
		a.SetDiscordActivity("Browsing", "Pack Viewer")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SetDiscordActivity blocked without a Discord client")
	}
}

// --- Remaining cache texture helpers ------------------------------------

func TestCacheTextureHelpers(t *testing.T) {
	a, _, _ := featureApp(t)
	cacheRoot := t.TempDir()
	writeViewerPack(t, cacheRoot, "Cached")

	uri, err := a.GetPackItemTextureFromCache("Cached", "diamond_sword", cacheRoot)
	if err != nil || !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("GetPackItemTextureFromCache = %.30q, %v", uri, err)
	}
	names, err := a.GetPackItemTextureNamesFromCache("Cached", cacheRoot)
	if err != nil || len(names) == 0 {
		t.Fatalf("GetPackItemTextureNamesFromCache = %v, %v", names, err)
	}
	sub, err := a.GetPackSkySubpackTexturesFromCache("Cached", "hd", cacheRoot)
	if err != nil || sub.Cubemap0 == "" {
		t.Fatalf("GetPackSkySubpackTexturesFromCache = %+v, %v", sub, err)
	}
	if uri := a.GetPlayerSkinTextureFromCache("Cached", cacheRoot); !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("GetPlayerSkinTextureFromCache = %.30q", uri)
	}
}
