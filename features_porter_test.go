package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// featureApp builds an App wired to throwaway folders: a packs root, an output
// folder and a LOCALAPPDATA, with vanilla fetches disabled so nothing here
// touches the network.
func featureApp(t *testing.T) (a *App, packsRoot, outDir string) {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	packsRoot = t.TempDir()
	outDir = t.TempDir()

	a = NewApp(false)
	a.settings["resourcePacksPath"] = packsRoot
	a.settings["customOutputDir"] = outDir
	offlineVanilla(t)
	return a, packsRoot, outDir
}

// zipBytes builds a zip from name -> content.
func zipBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// javaPackZip is a minimal Java resource pack: swim_porter only insists on
// pack.mcmeta, the rest exercises the item/texture conversions.
func javaPackZip(t *testing.T, desc string) []byte {
	t.Helper()
	meta, err := json.Marshal(map[string]interface{}{
		"pack": map[string]interface{}{"pack_format": 15, "description": desc},
	})
	if err != nil {
		t.Fatal(err)
	}
	return zipBytes(t, map[string][]byte{
		"pack.mcmeta": meta,
		"pack.png":    solidPNG(t, 64, 64, color.NRGBA{R: 30, G: 60, B: 90, A: 255}),
		"assets/minecraft/textures/item/diamond_sword.png": solidPNG(t, 16, 16, color.NRGBA{R: 9, A: 255}),
		"assets/minecraft/textures/block/stone.png":        solidPNG(t, 16, 16, color.NRGBA{G: 9, A: 255}),
	})
}

// bedrockPackZip is a pack the recolor tool accepts (Bedrock layout).
func bedrockPackZip(t *testing.T, name string) []byte {
	t.Helper()
	manifest, err := json.Marshal(map[string]interface{}{
		"format_version": 1,
		"header": map[string]interface{}{
			"name":        name,
			"description": "test pack",
			"uuid":        "0f0f0f0f-1111-2222-3333-444455556666",
			"version":     []int{1, 0, 0},
		},
		"modules": []interface{}{map[string]interface{}{
			"type":    "resources",
			"uuid":    "0f0f0f0f-1111-2222-3333-777788889999",
			"version": []int{1, 0, 0},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return zipBytes(t, map[string][]byte{
		"manifest.json":                    manifest,
		"textures/items/diamond_sword.png": solidPNG(t, 16, 16, color.NRGBA{R: 200, A: 255}),
		"textures/blocks/stone.png":        solidPNG(t, 16, 16, color.NRGBA{G: 200, A: 255}),
		"textures/entity/steve.png":        solidPNG(t, 64, 64, color.NRGBA{B: 200, A: 255}),
		"ui/hud_screen.json":               []byte(`{"namespace":"hud","main":{"type":"panel"}}`),
	})
}

func readZipEntry(t *testing.T, path, name string) []byte {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer r.Close()
	for _, f := range r.File {
		if filepath.ToSlash(f.Name) != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	t.Fatalf("%s has no entry %q", filepath.Base(path), name)
	return nil
}

// --- Pack Porter ---------------------------------------------------------

// TestPortPackConvertsJavaPack drives the main feature end to end: Java zip in,
// .mcpack on disk with a cleaned Bedrock manifest.
func TestPortPackConvertsJavaPack(t *testing.T) {
	a, _, outDir := featureApp(t)

	if _, err := a.PortPack(javaPackZip(t, "A java pack"), "Cool Pack.zip"); err != nil {
		t.Fatalf("PortPack: %v", err)
	}

	written := filepath.Join(outDir, "Cool Pack.mcpack")
	entries := zipEntries(t, written)
	for _, want := range []string{"manifest.json", "pack_icon.png"} {
		if !entries[want] {
			t.Fatalf("%s is missing %s (has %v)", filepath.Base(written), want, entries)
		}
	}

	var manifest struct {
		FormatVersion int `json:"format_version"`
		Header        struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"header"`
		Modules []struct {
			Type string `json:"type"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(readZipEntry(t, written, "manifest.json"), &manifest); err != nil {
		t.Fatalf("manifest is not json: %v", err)
	}
	// The ".zip" must not survive into the pack name.
	if manifest.Header.Name != "Cool Pack" {
		t.Errorf("pack name = %q, want %q", manifest.Header.Name, "Cool Pack")
	}
	if manifest.FormatVersion != 1 {
		t.Errorf("format_version = %d, want 1", manifest.FormatVersion)
	}
	if len(manifest.Modules) == 0 || manifest.Modules[0].Type != "resources" {
		t.Errorf("modules = %+v, want one resources module", manifest.Modules)
	}
}

func TestPortPackRejectsNonArchive(t *testing.T) {
	a, _, _ := featureApp(t)
	if _, err := a.PortPack([]byte("not a zip"), "notes.txt"); err == nil {
		t.Fatal("a .txt upload should be refused")
	}
	if _, err := a.PortPack([]byte("garbage"), "broken.zip"); err == nil {
		t.Fatal("a corrupt zip should be refused")
	}
}

// TestPortPackBlocksConcurrentPorts: two porting operations must not clobber the
// shared temp folders.
func TestPortPackBlocksConcurrentPorts(t *testing.T) {
	a, _, _ := featureApp(t)
	if err := a.acquirePort(); err != nil {
		t.Fatal(err)
	}
	defer a.releasePort()

	_, err := a.PortPack(javaPackZip(t, "x"), "second.zip")
	if err == nil || !strings.Contains(err.Error(), "another porting operation") {
		t.Fatalf("second port should be refused while one is running, got %v", err)
	}
}

// TestPortPackAutoImport installs the ported pack into the resource packs
// folder, the "auto-import" feature.
func TestPortPackAutoImport(t *testing.T) {
	a, packsRoot, _ := featureApp(t)
	a.settings["autoImport"] = true

	if _, err := a.PortPack(javaPackZip(t, "x"), "Auto Pack.zip"); err != nil {
		t.Fatalf("PortPack: %v", err)
	}

	manifestPath := filepath.Join(packsRoot, "Auto Pack", "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("auto-import did not install the pack: %v", err)
	}
	if _, err := os.Stat(filepath.Join(packsRoot, "Auto Pack", "pack_icon.png")); err != nil {
		t.Errorf("imported pack has no icon: %v", err)
	}
}

// TestPortLocalArchiveBatch is the Multi-Pack Porter: an outer archive holding
// two packs, both ported, originals kept in Java/.
func TestPortLocalArchiveBatch(t *testing.T) {
	a, _, outDir := featureApp(t)
	a.settings["deleteOriginals"] = false

	outer := zipBytes(t, map[string][]byte{
		"one.zip": javaPackZip(t, "first"),
		"two.zip": javaPackZip(t, "second"),
	})
	if err := a.PortLocalArchive(outer, "batch.zip"); err != nil {
		t.Fatalf("PortLocalArchive: %v", err)
	}

	for _, name := range []string{"one", "two"} {
		if _, err := os.Stat(filepath.Join(outDir, "Bedrock", name+".mcpack")); err != nil {
			t.Errorf("missing ported pack %s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(outDir, "Java", name+".zip")); err != nil {
			t.Errorf("missing original copy %s: %v", name, err)
		}
	}
}

func TestPortLocalArchiveRejectsEmpty(t *testing.T) {
	a, _, _ := featureApp(t)
	if err := a.PortLocalArchive(nil, "empty.zip"); err == nil {
		t.Fatal("empty upload should be refused")
	}
	if err := a.PortLocalArchive([]byte("x"), "pack.txt"); err == nil {
		t.Fatal("non-archive should be refused")
	}
}

// --- Recolor / CheckPack / sessions -------------------------------------

// TestCheckPackRecolorExport is the recolor feature's whole path: upload,
// browse, save a texture, export, then resume/delete the session.
func TestCheckPackRecolorExport(t *testing.T) {
	a, _, outDir := featureApp(t)

	res, err := a.CheckPack(bedrockPackZip(t, "Recolor Me"), "Recolor Me.mcpack")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.SessionId == "" {
		t.Fatalf("CheckPack = %+v, want a valid session", res)
	}
	wantFolders := map[string]bool{"textures/items": true, "textures/blocks": true}
	for _, f := range res.Folders {
		f = filepath.ToSlash(f)
		if strings.Contains(f, "entity") {
			t.Errorf("entity textures should be hidden from the recolor tool, got %q", f)
		}
		delete(wantFolders, f)
	}
	if len(wantFolders) != 0 {
		t.Errorf("missing folders in %v: %v", res.Folders, wantFolders)
	}

	images, err := a.GetImages(res.SessionId, "textures/items")
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].Filename != "diamond_sword" {
		t.Fatalf("GetImages = %+v, want the item texture", images)
	}
	if !strings.HasPrefix(images[0].DataURI, "data:image/png;base64,") {
		t.Errorf("texture is not a png data URI: %.40q", images[0].DataURI)
	}

	recolored := solidPNG(t, 16, 16, color.NRGBA{R: 0, G: 255, B: 0, A: 255})
	status, err := a.SaveImage(res.SessionId, SaveImageRequest{
		ImageName: "diamond_sword",
		RelPath:   images[0].RelPath,
		ImageData: base64.StdEncoding.EncodeToString(recolored),
	})
	if err != nil || status != "success" {
		t.Fatalf("SaveImage = %q, %v", status, err)
	}
	after, err := a.GetImages(res.SessionId, "textures/items")
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Size != int64(len(recolored)) {
		t.Errorf("saved texture is %d bytes, want %d", after[0].Size, len(recolored))
	}

	// An empty "done" message is how the UI finishes an upload batch.
	if status, err := a.SaveImage(res.SessionId, SaveImageRequest{Done: true}); err != nil || status != "done" {
		t.Fatalf("SaveImage(done) = %q, %v", status, err)
	}

	msg, err := a.ExportPack(res.SessionId, "Recolor Me")
	if err != nil {
		t.Fatalf("ExportPack: %v", err)
	}
	exported := filepath.Join(outDir, "Recolor Me-recolored.mcpack")
	if !strings.Contains(msg, exported) {
		t.Errorf("ExportPack message %q does not name %q", msg, exported)
	}
	for name := range zipEntries(t, exported) {
		if strings.Contains(name, `\`) {
			t.Fatalf("exported zip entry %q uses a backslash; Bedrock will not open it", name)
		}
	}

	// The session shows up for resume, and can be deleted.
	found := false
	for _, s := range a.ListSessions() {
		if s.Id == res.SessionId {
			found = true
			if s.PackName != "Recolor Me.mcpack" {
				t.Errorf("session pack name = %q", s.PackName)
			}
		}
	}
	if !found {
		t.Fatal("the session is not listed for resume")
	}
	if err := a.DeleteSession(res.SessionId); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetImages(res.SessionId, "textures/items"); err == nil {
		t.Fatal("a deleted session should not be readable")
	}
}

func TestCheckPackRejectsInvalidUpload(t *testing.T) {
	a, _, _ := featureApp(t)

	res, err := a.CheckPack(javaPackZip(t, "no bedrock manifest"), "java.zip")
	if err != nil {
		t.Fatalf("CheckPack returned an error instead of an invalid result: %v", err)
	}
	if res.Valid {
		t.Fatal("a Java pack has no manifest.json and should not be editable")
	}
	if res.ErrorMsg == "" {
		t.Error("an invalid pack should come with a message")
	}
	if _, err := a.CheckPack([]byte("not a zip"), "junk.mcpack"); err == nil {
		t.Error("a corrupt upload should return an error")
	}
}

// TestGetImagesRejectsEscape: the folder the UI asks for must stay in the
// session (SEC-6).
func TestGetImagesRejectsEscape(t *testing.T) {
	a, _, _ := featureApp(t)
	res, err := a.CheckPack(bedrockPackZip(t, "p"), "p.mcpack")
	if err != nil || !res.Valid {
		t.Fatalf("CheckPack: %v %+v", err, res)
	}
	defer a.DeleteSession(res.SessionId)

	for _, folder := range []string{"..", "../..", `..\..`} {
		if images, err := a.GetImages(res.SessionId, folder); err == nil {
			t.Errorf("GetImages(%q) returned %d images, want an error", folder, len(images))
		}
	}
}
