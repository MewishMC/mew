package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func zipEntries(t *testing.T, path string) map[string]bool {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()
	out := map[string]bool{}
	for _, f := range r.File {
		out[f.Name] = true
	}
	return out
}

func encodeTestPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestGetPackThumbCacheInvalidatedBySave is the regression test for a replaced
// texture keeping its old sidebar thumbnail: the cache key must be built the
// same way when reading and when evicting.
func TestGetPackThumbCacheInvalidatedBySave(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "Pack", "textures", "ui")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	rel := "textures/ui/button.png"
	if err := os.WriteFile(filepath.Join(base, "Pack", filepath.FromSlash(rel)), encodeTestPNG(t, color.RGBA{R: 255, A: 255}), 0644); err != nil {
		t.Fatal(err)
	}

	a := NewApp(false)
	before, err := a.GetPackThumb("Pack", rel, base)
	if err != nil {
		t.Fatalf("GetPackThumb: %v", err)
	}

	blue := encodeTestPNG(t, color.RGBA{B: 255, A: 255})
	if err := a.SavePackTexture("Pack", rel, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(blue), base); err != nil {
		t.Fatalf("SavePackTexture: %v", err)
	}

	after, err := a.GetPackThumb("Pack", rel, base)
	if err != nil {
		t.Fatalf("GetPackThumb after save: %v", err)
	}
	if before == after {
		t.Fatal("thumbnail cache served the pre-edit image after SavePackTexture")
	}
}

// TestPruneStaleSessionsKeepsSessionWithoutTimestamp: metadata written without
// updatedAt must not make a session look decades old and get it deleted.
func TestPruneStaleSessionsKeepsSessionWithoutTimestamp(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	a := NewApp(false)
	id, err := a.createSessionDir()
	if err != nil {
		t.Fatalf("createSessionDir: %v", err)
	}
	if err := os.WriteFile(a.sessionMetaPath(id), []byte(`{"id":"`+id+`","packName":"x"}`), 0644); err != nil {
		t.Fatal(err)
	}

	a.pruneStaleSessions()

	if _, err := a.sessionDir(id); err != nil {
		t.Fatalf("session with no updatedAt was pruned: %v", err)
	}
}

// TestPruneStaleSessionsDropsOldSessions guards the intended behaviour: a
// session older than the age limit is removed.
func TestPruneStaleSessionsDropsOldSessions(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	a := NewApp(false)
	id, err := a.createSessionDir()
	if err != nil {
		t.Fatalf("createSessionDir: %v", err)
	}
	old := time.Now().UnixMilli() - int64(sessionMaxAge/time.Millisecond) - 60_000
	a.writeSessionMeta(SessionMeta{Id: id, CreatedAt: old, UpdatedAt: old})

	a.pruneStaleSessions()

	if _, err := a.sessionDir(id); err == nil {
		t.Fatal("stale session was not pruned")
	}
}

// TestParseHexColor: an 8-digit value must not be mistaken for a 6-digit one.
func TestParseHexColor(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
	}{
		{"#ff0000", color.RGBA{R: 255, A: 255}},
		{"f00", color.RGBA{R: 255, A: 255}},
		{"#00ff0080", color.RGBA{G: 255, A: 255}},
		{"#0000ff", color.RGBA{B: 255, A: 255}},
		{"", color.RGBA{A: 255}},
		{"nothex", color.RGBA{A: 255}},
	}
	for _, c := range cases {
		if got := parseHexColor(c.in); got != c.want {
			t.Errorf("parseHexColor(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

// TestCreateZipFromFolder covers the export path whose per-file handles used to
// stay open until the whole walk finished.
func TestCreateZipFromFolder(t *testing.T) {
	src := t.TempDir()
	files := map[string]string{
		"manifest.json":            `{"header":{}}`,
		"textures/items/a.png":     "a",
		"textures/items/b.png":     "b",
		"textures/items/sub/c.png": "c",
	}
	for name, body := range files {
		full := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(t.TempDir(), "pack.mcpack")
	if err := createZipFromFolder(src, out); err != nil {
		t.Fatalf("createZipFromFolder: %v", err)
	}

	got := zipEntries(t, out)
	for name := range files {
		if !got[filepath.ToSlash(name)] {
			t.Errorf("zip is missing %s (has %v)", name, got)
		}
	}
}

// TestSettingsConcurrentAccess exercises the settings map from several
// goroutines the way Wails calls do. It fails (or the race detector fires) if
// the map is left unguarded.
func TestSettingsConcurrentAccess(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	a := NewApp(false)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = a.getBoolSetting("autoImport")
				_ = a.getStringSetting("manifestDescription")
				_ = a.getOutputDir()
				_ = a.GetSettings()
				a.SaveSettings(map[string]interface{}{"autoImport": true, "customOutputDir": ""})
			}
		}()
	}
	wg.Wait()
}

// TestGetInstalledPacksDetailedKeepsIconMime: pack_icon.jpg used to be served as
// image/png and rendered as a broken image.
func TestGetInstalledPacksDetailedKeepsIconMime(t *testing.T) {
	base := t.TempDir()
	pack := filepath.Join(base, "Pack")
	if err := os.MkdirAll(pack, 0755); err != nil {
		t.Fatal(err)
	}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}
	if err := os.WriteFile(filepath.Join(pack, "pack_icon.jpg"), jpeg, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pack, "manifest.json"), []byte(`{"header":{"name":"P"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	a := NewApp(false)
	a.settings["resourcePacksPath"] = base

	info := a.GetInstalledPacksDetailed()
	if len(info.Packs) != 1 {
		t.Fatalf("expected 1 pack, got %d", len(info.Packs))
	}
	icon := info.Packs[0].Icon
	if len(icon) < len("data:image/jpeg") || icon[:len("data:image/jpeg")] != "data:image/jpeg" {
		t.Fatalf("icon data URI = %q, want a jpeg data URI", icon)
	}
}
