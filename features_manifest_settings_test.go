package main

import (
	"encoding/base64"
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Manifest tool -------------------------------------------------------

func TestManifestNewPackAndDuplicateName(t *testing.T) {
	a, packsRoot, _ := featureApp(t)

	manifest, err := a.NewManifestTemplate()
	if err != nil {
		t.Fatal(err)
	}
	created, err := a.CreatePackFromManifest(manifest)
	if err != nil {
		t.Fatalf("CreatePackFromManifest: %v", err)
	}
	if created != "My Pack" {
		t.Fatalf("created %q, want %q", created, "My Pack")
	}
	if _, err := os.Stat(filepath.Join(packsRoot, created, "manifest.json")); err != nil {
		t.Fatalf("manifest.json was not written: %v", err)
	}

	// The same name again must not overwrite the first pack.
	second, err := a.CreatePackFromManifest(manifest)
	if err != nil {
		t.Fatalf("second CreatePackFromManifest: %v", err)
	}
	if second == created {
		t.Fatalf("the second pack reused the name %q", second)
	}
	if _, err := os.Stat(filepath.Join(packsRoot, second, "manifest.json")); err != nil {
		t.Fatalf("second pack has no manifest: %v", err)
	}
}

func TestManifestValidation(t *testing.T) {
	a, _, _ := featureApp(t)

	cases := map[string]map[string]interface{}{
		"no format_version": {
			"header":  map[string]interface{}{"name": "x", "uuid": "u"},
			"modules": []interface{}{},
		},
		"no header": {"format_version": 1},
		"no name": {
			"format_version": 1,
			"header":         map[string]interface{}{"uuid": "u"},
		},
		"no uuid": {
			"format_version": 1,
			"header":         map[string]interface{}{"name": "x"},
		},
		"header not an object": {"format_version": 1, "header": "nope"},
	}
	for name, m := range cases {
		if _, err := a.CreatePackFromManifest(m); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestManifestInstallEditAndBackup(t *testing.T) {
	a, packsRoot, _ := featureApp(t)

	manifest, err := a.NewManifestTemplate()
	if err != nil {
		t.Fatal(err)
	}
	created, err := a.CreatePackFromManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}

	installed, err := a.GetInstalledManifestPacks()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 {
		t.Fatalf("GetInstalledManifestPacks = %+v, want 1 pack", installed)
	}
	if got := installed[0]; !got.HasManifest || got.ModuleType != "resources" || got.PackVersion != "1.0.0" {
		t.Errorf("installed info = %+v", got)
	}
	if installed[0].IconURI != "" {
		t.Errorf("a new pack has no icon yet, got %.40q", installed[0].IconURI)
	}

	edited, err := a.GetPackManifest(created)
	if err != nil {
		t.Fatal(err)
	}
	edited["header"].(map[string]interface{})["description"] = "edited by the tool"
	if err := a.ApplyManifestToPack(created, edited); err != nil {
		t.Fatalf("ApplyManifestToPack: %v", err)
	}

	saved, err := a.GetPackManifest(created)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved["header"].(map[string]interface{})["description"]; got != "edited by the tool" {
		t.Errorf("description = %v, want the edit", got)
	}
	if _, err := os.Stat(filepath.Join(packsRoot, created, "manifest.json.bak")); err != nil {
		t.Errorf("the old manifest was not backed up: %v", err)
	}

	if err := a.ApplyManifestToPack("missing", edited); err == nil {
		t.Error("applying to a missing pack should fail")
	}
	if err := a.ApplyManifestToPack(created, nil); err == nil {
		t.Error("applying an empty manifest should fail")
	}
}

// TestManifestUploadRoundTrip edits an uploaded .mcpack and re-exports it.
func TestManifestUploadRoundTrip(t *testing.T) {
	a, _, outDir := featureApp(t)

	m, err := a.GetManifestFromUpload(bedrockPackZip(t, "Uploaded Pack"))
	if err != nil {
		t.Fatalf("GetManifestFromUpload: %v", err)
	}
	header, _ := m["header"].(map[string]interface{})
	if header["name"] != "Uploaded Pack" {
		t.Fatalf("uploaded manifest name = %v", header["name"])
	}
	header["name"] = "Renamed Pack"

	out, err := a.ApplyManifestToUpload(m, "uploaded.mcpack")
	if err != nil {
		t.Fatalf("ApplyManifestToUpload: %v", err)
	}
	if filepath.Dir(out) != outDir {
		t.Fatalf("exported to %q, want inside %q", out, outDir)
	}

	var written struct {
		Header struct {
			Name string `json:"name"`
		} `json:"header"`
	}
	if err := json.Unmarshal(readZipEntry(t, out, "manifest.json"), &written); err != nil {
		t.Fatal(err)
	}
	if written.Header.Name != "Renamed Pack" {
		t.Errorf("exported manifest name = %q, want the edit", written.Header.Name)
	}
	// The pack's textures must survive the round trip.
	if !zipEntries(t, out)["textures/items/diamond_sword.png"] {
		t.Error("the re-exported pack lost its textures")
	}
}

func TestManifestUploadRequiresAPack(t *testing.T) {
	a, _, _ := featureApp(t)
	if _, err := a.GetManifestFromUpload(nil); err == nil {
		t.Error("no data should be refused")
	}
	if _, err := a.GetManifestFromUpload([]byte("not a zip")); err == nil {
		t.Error("a corrupt upload should be refused")
	}
	// With nothing uploaded, applying must not invent a pack.
	if _, err := a.ApplyManifestToUpload(map[string]interface{}{"format_version": 1}, "x.mcpack"); err == nil {
		t.Error("applying before uploading should fail")
	}
}

func TestRegenerateUUIDAndDirAccess(t *testing.T) {
	a, _, _ := featureApp(t)

	u1, err := a.RegenerateUuid()
	if err != nil || len(u1) < 32 {
		t.Fatalf("RegenerateUuid = %q, %v", u1, err)
	}
	u2, _ := a.RegenerateUuid()
	if u1 == u2 {
		t.Error("RegenerateUuid returned the same uuid twice")
	}

	dir := t.TempDir()
	manifest, err := json.Marshal(map[string]interface{}{
		"format_version": 1,
		"header":         map[string]interface{}{"name": "Dir Pack", "uuid": u1, "version": []int{1, 2, 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := a.GetPackManifestFromDir(dir)
	if err != nil {
		t.Fatalf("GetPackManifestFromDir: %v", err)
	}
	m["header"].(map[string]interface{})["name"] = "Dir Pack 2"
	if err := a.ApplyManifestToDir(dir, m); err != nil {
		t.Fatalf("ApplyManifestToDir: %v", err)
	}
	again, err := a.GetPackManifestFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again["header"].(map[string]interface{})["name"] != "Dir Pack 2" {
		t.Error("ApplyManifestToDir did not persist the edit")
	}
	if _, err := a.GetPackManifestFromDir(filepath.Join(dir, "nope")); err == nil {
		t.Error("a missing directory should be refused")
	}
}

// --- Packs, export, cache, delete ---------------------------------------

func writeInstalledPack(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "textures", "blocks"), 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"format_version":1,"header":{"name":"` + name + `","description":"d","uuid":"` + name + `-uuid","version":[1,0,0]},"modules":[{"type":"resources","uuid":"` + name + `-mod","version":[1,0,0]}]}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "textures", "blocks", "stone.png"), solidPNG(t, 8, 8, colorWhite), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestExportAndDeleteInstalledPacks(t *testing.T) {
	a, packsRoot, outDir := featureApp(t)
	writeInstalledPack(t, packsRoot, "PackA")
	writeInstalledPack(t, packsRoot, "PackB")

	list, err := a.GetPackListWithInfo()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("GetPackListWithInfo returned %d packs, want 2", len(list))
	}
	for _, p := range list {
		if p.Size <= 0 || p.ModTime == "" {
			t.Errorf("%s: size %d, modTime %q", p.DirName, p.Size, p.ModTime)
		}
	}

	single, err := a.ExportInstalledPack("PackA")
	if err != nil {
		t.Fatalf("ExportInstalledPack: %v", err)
	}
	if !samePath(single, filepath.Join(outDir, "PackA.mcpack")) {
		t.Fatalf("exported %q", single)
	}
	if !zipEntries(t, single)["manifest.json"] {
		t.Error("exported pack has no manifest")
	}

	written, err := a.ExportInstalledPacks([]string{"PackA", "PackB"})
	if err != nil {
		t.Fatalf("ExportInstalledPacks: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("ExportInstalledPacks wrote %d files, want 2", len(written))
	}

	// Recent packs record what was exported.
	recent := a.GetRecentPacks()
	if len(recent) == 0 {
		t.Error("exporting did not add a recent pack")
	}
	for _, r := range recent {
		if _, err := os.Stat(r.Path); err != nil {
			t.Errorf("recent pack %q points at a missing file %q", r.Name, r.Path)
		}
	}

	for _, bad := range []string{"", "..", "../escape", `a/b`, `a\b`} {
		if err := a.DeleteInstalledPack(bad); err == nil {
			t.Errorf("DeleteInstalledPack(%q) should be refused", bad)
		}
	}
	if err := a.DeleteInstalledPack("PackB"); err != nil {
		t.Fatalf("DeleteInstalledPack: %v", err)
	}
	if _, err := os.Stat(filepath.Join(packsRoot, "PackB")); !os.IsNotExist(err) {
		t.Error("PackB was not deleted")
	}
}

func TestPackCacheExport(t *testing.T) {
	a, _, _ := featureApp(t)
	cacheRoot := t.TempDir()
	a.settings["packCachePath"] = cacheRoot
	writeInstalledPack(t, cacheRoot, "CachedPack")

	sources := a.GetPackCache()
	if len(sources) != 1 || !sources[0].Found {
		t.Fatalf("GetPackCache = %+v", sources)
	}
	if len(sources[0].Packs) != 1 || sources[0].Packs[0] != "CachedPack" {
		t.Errorf("cache packs = %v", sources[0].Packs)
	}

	list, err := a.GetPackCacheList("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("GetPackCacheList returned %d packs, want 1", len(list))
	}

	written, err := a.ExportCachePacks([]string{"CachedPack"}, "")
	if err != nil {
		t.Fatalf("ExportCachePacks: %v", err)
	}
	if len(written) != 1 || !zipEntries(t, written[0])["manifest.json"] {
		t.Fatalf("ExportCachePacks wrote %v", written)
	}
	if _, err := a.ExportCachePacks(nil, ""); err == nil {
		t.Error("exporting nothing should be refused")
	}
	if _, err := a.ExportCachePacks([]string{"../escape"}, ""); err == nil {
		t.Error("a traversing pack name should be refused")
	}
}

func TestInstalledPacksListing(t *testing.T) {
	a, packsRoot, _ := featureApp(t)
	writeInstalledPack(t, packsRoot, "Zeta")
	writeInstalledPack(t, packsRoot, "Alpha")
	if err := os.WriteFile(filepath.Join(packsRoot, "loose.mcpack"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	info := a.GetInstalledPacks()
	if !info.Found {
		t.Fatal("Found should be true")
	}
	if len(info.Packs) != 3 {
		t.Fatalf("packs = %v, want 2 dirs + 1 mcpack", info.Packs)
	}
	// Sorted by name.
	if info.Packs[0] != "Alpha" {
		t.Errorf("packs are not sorted: %v", info.Packs)
	}

	detailed := a.GetInstalledPacksDetailed()
	if len(detailed.Packs) != 3 {
		t.Fatalf("detailed packs = %+v", detailed.Packs)
	}

	preview, err := a.GetPackPreviewInfo("Alpha")
	if err != nil {
		t.Fatalf("GetPackPreviewInfo: %v", err)
	}
	if preview.Name != "Alpha" || preview.Description != "d" {
		t.Errorf("preview = %+v", preview)
	}
	if _, err := a.GetPackPreviewInfo("nope"); err == nil {
		t.Error("a missing pack should be refused")
	}

	sets := a.GetSkyPacks()
	if len(sets) != 0 {
		t.Errorf("packs without a sky should not be listed, got %+v", sets)
	}
}

// --- Settings, data folders, recent packs -------------------------------

func TestSettingsPersistAcrossRestart(t *testing.T) {
	a, _, outDir := featureApp(t)
	a.SaveSettings(map[string]interface{}{
		"autoImport":      true,
		"customOutputDir": outDir,
		"skyPresetNames":  []string{"one", "two"},
		"theme":           "light",
	})

	restarted := NewApp(false)
	loaded := restarted.GetSettings()
	if loaded["autoImport"] != true {
		t.Errorf("autoImport = %v, want true", loaded["autoImport"])
	}
	if loaded["customOutputDir"] != outDir {
		t.Errorf("customOutputDir = %v, want %q", loaded["customOutputDir"], outDir)
	}
	if loaded["theme"] != "light" {
		t.Errorf("theme = %v", loaded["theme"])
	}
	// Defaults must survive a load that does not mention them.
	if loaded["discordRPC"] != true {
		t.Errorf("a default setting was lost: discordRPC = %v", loaded["discordRPC"])
	}
	if got := restarted.getOutputDir(); !samePath(got, outDir) {
		t.Errorf("getOutputDir = %q, want %q", got, outDir)
	}
}

func TestSaveSettingsIgnoresNull(t *testing.T) {
	a, _, outDir := featureApp(t)
	a.SaveSettings(nil)
	if a.getOutputDir() != outDir {
		t.Error("a null settings payload wiped the settings")
	}
	// A later write must not panic on a nil map.
	if _, err := a.SetResourcePacksPath(outDir); err != nil {
		t.Fatalf("SetResourcePacksPath after a null save: %v", err)
	}
}

func TestCustomItemsAndRemovedItems(t *testing.T) {
	a, _, _ := featureApp(t)

	if items := a.GetCustomItems(); len(items) != 0 {
		t.Fatalf("fresh install has custom items: %v", items)
	}
	if err := a.SaveCustomItem("my_sword"); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveCustomItem("my_sword"); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveCustomItem("my_pickaxe"); err != nil {
		t.Fatal(err)
	}
	if items := a.GetCustomItems(); len(items) != 2 {
		t.Fatalf("custom items = %v, want 2 (duplicates collapsed)", items)
	}
	if err := a.RemoveCustomItem("my_sword"); err != nil {
		t.Fatal(err)
	}
	if items := a.GetCustomItems(); len(items) != 1 || items[0] != "my_pickaxe" {
		t.Errorf("custom items after remove = %v", items)
	}

	if err := a.SaveRemovedItem("diamond_sword"); err != nil {
		t.Fatal(err)
	}
	if items := a.GetRemovedItems(); len(items) != 1 {
		t.Fatalf("removed items = %v", items)
	}
	if err := a.RestoreRemovedItem("diamond_sword"); err != nil {
		t.Fatal(err)
	}
	if items := a.GetRemovedItems(); len(items) != 0 {
		t.Errorf("removed items after restore = %v", items)
	}
}

func TestDefaultSkinRoundTrip(t *testing.T) {
	a, _, _ := featureApp(t)

	if got := a.GetDefaultSkin(); got != "" {
		t.Fatalf("fresh install has a default skin: %.40q", got)
	}
	skin := solidPNG(t, 64, 64, colorRGBA(10, 20, 30))
	if err := a.SaveDefaultSkin("data:image/png;base64," + base64.StdEncoding.EncodeToString(skin)); err != nil {
		t.Fatalf("SaveDefaultSkin: %v", err)
	}
	got := a.GetDefaultSkin()
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("GetDefaultSkin = %.40q", got)
	}
	if err := a.SaveDefaultSkin("nonsense"); err == nil {
		t.Error("a non data URI should be refused")
	}
}

func TestRecentPacksCapAndOrder(t *testing.T) {
	a, _, _ := featureApp(t)
	for i := 0; i < 12; i++ {
		a.AddRecentPack("pack"+string(rune('A'+i)), "/tmp/pack"+string(rune('A'+i)))
	}
	if got := len(a.GetRecentPacks()); got != 5 {
		t.Errorf("GetRecentPacks returned %d, want the 5 newest", got)
	}
	all := a.GetAllRecentPacks()
	if len(all) != 10 {
		t.Fatalf("GetAllRecentPacks returned %d, want 10", len(all))
	}
	if all[0].Name != "packL" {
		t.Errorf("newest pack = %q, want packL", all[0].Name)
	}
	if all[0].Timestamp == "" {
		t.Error("recent packs have no timestamp")
	}
}

func TestClearCache(t *testing.T) {
	a, _, _ := featureApp(t)

	temp := getMewTempDir("sub")
	if err := os.MkdirAll(temp, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	a.AddRecentPack("x", "/tmp/x")

	if err := a.ClearCache(); err != nil {
		t.Fatalf("ClearCache: %v", err)
	}
	if _, err := os.Stat(getMewTempDir("")); !os.IsNotExist(err) {
		t.Error("the temp folder survived ClearCache")
	}
	if got := a.GetRecentPacks(); len(got) != 0 {
		t.Errorf("recent packs survived ClearCache: %v", got)
	}
}

func TestSetResourcePacksPath(t *testing.T) {
	a, packsRoot, _ := featureApp(t)

	info, err := a.SetResourcePacksPath(packsRoot)
	if err != nil {
		t.Fatalf("SetResourcePacksPath: %v", err)
	}
	if !info.Found || !samePath(info.Path, packsRoot) {
		t.Errorf("info = %+v", info)
	}
	if _, err := a.SetResourcePacksPath(filepath.Join(packsRoot, "missing")); err == nil {
		t.Error("a missing folder should be refused")
	}
}

func TestDetectMinecraftPaths(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	t.Setenv("LOCALAPPDATA", t.TempDir())

	wanted := filepath.Join(appData, "Minecraft Bedrock", "Users", "Shared", "games", "com.mojang", "resource_packs")
	if err := os.MkdirAll(wanted, 0755); err != nil {
		t.Fatal(err)
	}

	a := NewApp(false)
	paths := a.DetectMinecraftPaths()
	if len(paths) == 0 {
		t.Fatal("the installed Minecraft folder was not detected")
	}
	found := false
	for _, p := range paths {
		if samePath(p.Path, wanted) {
			found = true
			if p.Name == "" {
				t.Error("detected path has no label")
			}
		}
	}
	if !found {
		t.Errorf("detected paths %+v, want %q", paths, wanted)
	}
	if got := a.getDefaultResourcePacksPath(); !samePath(got, wanted) {
		t.Errorf("default resource packs path = %q, want %q", got, wanted)
	}
}

func TestVersionAndDebugFlag(t *testing.T) {
	a, _, _ := featureApp(t)
	if v := a.GetVersion(); v == "" {
		t.Error("GetVersion returned nothing")
	}
	if a.IsDebug() {
		t.Error("a test app should not be in debug mode")
	}
	if NewApp(true).IsDebug() != true {
		t.Error("NewApp(true) should enable debug")
	}
}

// Small local helpers so this file does not depend on other tests' colours.
var colorWhite = color.NRGBA{R: 255, G: 255, B: 255, A: 255}

func colorRGBA(r, g, b uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: 255} }
