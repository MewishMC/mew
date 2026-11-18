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

// writeViewerPack creates an installed pack with UI screens, item textures, a
// sky (plus a subpack sky) and a player skin, i.e. everything the Pack Viewer
// and the texture editors read.
func writeViewerPack(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	write := func(rel string, data []byte) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	manifest := map[string]interface{}{
		"format_version": 1,
		"header": map[string]interface{}{
			"name":        name,
			"description": "viewer fixture",
			"uuid":        name + "-uuid",
			"version":     []int{1, 2, 3},
		},
		"modules": []interface{}{map[string]interface{}{
			"type": "resources", "uuid": name + "-module", "version": []int{1, 2, 3},
		}},
		"subpacks": []interface{}{map[string]interface{}{"folder_name": "hd", "name": "HD Sky"}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	write("manifest.json", data)
	write("pack_icon.png", solidPNG(t, 32, 32, color.NRGBA{R: 4, G: 5, B: 6, A: 255}))
	write("ui/hud_screen.json", []byte(`{"namespace":"hud","main":{"type":"screen"}}`))
	write("ui/sub/nested.json", []byte(`{"namespace":"nested","root":{"type":"screen"}}`))
	write("ui/notjson.txt", []byte("ignored"))
	write("textures/items/diamond_sword.png", solidPNG(t, 16, 16, color.NRGBA{R: 9, A: 255}))
	write("textures/items/apple.png", solidPNG(t, 16, 16, color.NRGBA{G: 9, A: 255}))
	write("textures/entity/steve.png", solidPNG(t, 64, 64, color.NRGBA{B: 9, A: 255}))
	write("textures/blocks/stone.png", solidPNG(t, 16, 16, color.NRGBA{R: 1, G: 1, A: 255}))
	for i := 0; i < 6; i++ {
		write("textures/environment/overworld_cubemap/cubemap_"+string(rune('0'+i))+".png",
			solidPNG(t, 8, 8, color.NRGBA{R: uint8(10 * i), A: 255}))
	}
	write("subpacks/hd/textures/environment/overworld_cubemap/cubemap_0.png",
		solidPNG(t, 8, 8, color.NRGBA{G: 200, A: 255}))
	return dir
}

// --- Pack Viewer: UI screens and textures -------------------------------

func TestPackViewerUiScreens(t *testing.T) {
	a, packsRoot, _ := featureApp(t)
	writeViewerPack(t, packsRoot, "Viewer")

	files, err := a.GetPackMenuFiles("Viewer")
	if err != nil {
		t.Fatalf("GetPackMenuFiles: %v", err)
	}
	want := []string{"hud_screen.json", "sub/nested.json"}
	if len(files) != len(want) {
		t.Fatalf("menu files = %v, want %v (only .json, nested included)", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("menu files = %v, want %v", files, want)
		}
	}

	content, err := a.GetPackMenuContent("Viewer", "sub/nested.json")
	if err != nil {
		t.Fatalf("nested menu file is unreadable: %v", err)
	}
	if !strings.Contains(content, "nested") {
		t.Errorf("content = %q", content)
	}
	for _, bad := range []string{"../manifest.json", `..\manifest.json`, "/etc/passwd", "missing.json"} {
		if _, err := a.GetPackMenuContent("Viewer", bad); err == nil {
			t.Errorf("GetPackMenuContent(%q) should be refused", bad)
		}
	}

	if !a.PackHasUiScreens("Viewer") {
		t.Error("a pack with screen files was not detected")
	}
	if a.PackHasUiScreens("Missing") {
		t.Error("a missing pack cannot have UI screens")
	}

	ui, err := a.GetPackAllUiResources("Viewer")
	if err != nil {
		t.Fatalf("GetPackAllUiResources: %v", err)
	}
	// The JSON UI stage gets every resource a pack overrides: screens, textures
	// and all, with binaries flagged so it does not parse them as JSON.
	jsonFiles, binaries := 0, 0
	for _, r := range ui {
		if r.IsBinary {
			binaries++
			continue
		}
		jsonFiles++
		if r.Content == "" || !strings.Contains(r.Path, ".json") {
			t.Errorf("json resource %+v carries no content", r)
		}
	}
	if jsonFiles != 2 || binaries != 10 {
		t.Errorf("ui resources = %d json + %d binary, want 2 + 10", jsonFiles, binaries)
	}
}

func TestPackViewerItemTextures(t *testing.T) {
	a, packsRoot, _ := featureApp(t)
	writeViewerPack(t, packsRoot, "Viewer")

	names, err := a.GetPackItemTextureNames("Viewer")
	if err != nil {
		t.Fatalf("GetPackItemTextureNames: %v", err)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "diamond_sword") || !strings.Contains(joined, "apple") {
		t.Fatalf("item names = %v", names)
	}

	uri, err := a.GetPackItemTexture("Viewer", "diamond_sword")
	if err != nil || !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("GetPackItemTexture = %.30q, %v", uri, err)
	}
	// A missing texture reads as empty, which is what the picker expects.
	if missing, err := a.GetPackItemTexture("Viewer", "not_an_item"); err != nil || missing != "" {
		t.Errorf("GetPackItemTexture(missing) = %.20q, %v", missing, err)
	}
	for _, bad := range []string{"../pack_icon", `..\pack_icon`, ".."} {
		if got, err := a.GetPackItemTexture("Viewer", bad); err != nil || got != "" {
			t.Errorf("GetPackItemTexture(%q) = %.20q, %v; traversing names must read nothing", bad, got, err)
		}
	}

	all, err := a.GetPackAllItemTextures("Viewer")
	if err != nil || len(all) != 2 {
		t.Fatalf("GetPackAllItemTextures = %d, %v", len(all), err)
	}

	// A custom item is listed; the picker hides removed ones itself, and the
	// curated material list drops them too.
	if err := os.WriteFile(filepath.Join(packsRoot, "Viewer", "textures", "items", "custom_ring.png"),
		solidPNG(t, 16, 16, color.NRGBA{R: 200, G: 100, A: 255}), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveCustomItem("custom_ring"); err != nil {
		t.Fatal(err)
	}
	names, err = a.GetPackItemTextureNames("Viewer")
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(names, ",")
	if !strings.Contains(joined, "custom_ring") {
		t.Errorf("the custom item is missing from %v", names)
	}

	curated, err := a.GetPackItemTextures("Viewer", "diamond")
	if err != nil {
		t.Fatal(err)
	}
	hasSword := false
	for _, it := range curated {
		if it.Name == "diamond_sword" {
			hasSword = true
		}
	}
	if !hasSword {
		t.Fatalf("GetPackItemTextures(diamond) = %+v, want the sword", curated)
	}
	if err := a.SaveRemovedItem("diamond_sword"); err != nil {
		t.Fatal(err)
	}
	curated, err = a.GetPackItemTextures("Viewer", "diamond")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range curated {
		if it.Name == "diamond_sword" {
			t.Error("a removed item still appears in the material list")
		}
	}
}

// --- Pack Viewer: sky and subpacks --------------------------------------

func TestPackViewerSkyAndSubpacks(t *testing.T) {
	a, packsRoot, _ := featureApp(t)
	writeViewerPack(t, packsRoot, "Viewer")

	sky, err := a.GetPackSkyTextures("Viewer")
	if err != nil {
		t.Fatalf("GetPackSkyTextures: %v", err)
	}
	faces := []string{sky.Cubemap0, sky.Cubemap1, sky.Cubemap2, sky.Cubemap3, sky.Cubemap4, sky.Cubemap5}
	for i, f := range faces {
		if !strings.HasPrefix(f, "data:image/png;base64,") {
			t.Errorf("cubemap_%d is empty: %.30q", i, f)
		}
	}

	subs, err := a.GetPackSkySubpacks("Viewer")
	if err != nil {
		t.Fatalf("GetPackSkySubpacks: %v", err)
	}
	if len(subs) != 1 || subs[0].FolderName != "hd" || subs[0].Name != "HD Sky" {
		t.Fatalf("subpacks = %+v", subs)
	}

	subSky, err := a.GetPackSkySubpackTextures("Viewer", "hd")
	if err != nil {
		t.Fatalf("GetPackSkySubpackTextures: %v", err)
	}
	if subSky.Cubemap0 == "" {
		t.Error("the subpack's cubemap was not read")
	}
	if other, err := a.GetPackSkySubpackTextures("Viewer", "nope"); err != nil || other.Cubemap0 != "" {
		t.Errorf("a missing subpack should read as empty, got %+v, %v", other, err)
	}

	sets := a.GetSkyPacks()
	if len(sets) != 1 {
		t.Fatalf("GetSkyPacks = %+v, want the one pack with a sky", sets)
	}
	if !sets[0].HasSky || len(sets[0].Subpacks) != 1 {
		t.Errorf("sky pack = %+v", sets[0])
	}
	if sets[0].Name != "Viewer" {
		t.Errorf("sky pack name = %q", sets[0].Name)
	}
}

// --- Texture editor on an installed pack --------------------------------

func TestInstalledPackTextureEditing(t *testing.T) {
	a, packsRoot, outDir := featureApp(t)
	dir := writeViewerPack(t, packsRoot, "Viewer")

	if got := a.GetExportOutputDir(); !samePath(got, outDir) {
		t.Errorf("GetExportOutputDir = %q, want %q", got, outDir)
	}

	root, err := a.GetPackRoot("Viewer", "")
	if err != nil || !samePath(root, dir) {
		t.Fatalf("GetPackRoot = %q, %v", root, err)
	}
	if _, err := a.GetPackRoot("Missing", ""); err == nil {
		t.Error("a missing pack should be an error")
	}

	textures, err := a.ListPackTextures("Viewer", "")
	if err != nil {
		t.Fatalf("ListPackTextures: %v", err)
	}
	byRel := map[string]RecolorTexture{}
	for _, tex := range textures {
		byRel[tex.RelPath] = tex
	}
	if _, ok := byRel["textures/items/apple.png"]; !ok {
		t.Errorf("apple.png is not listed: %v", textures)
	}
	for rel := range byRel {
		if strings.Contains(rel, "entity") {
			t.Errorf("entity textures should be hidden: %s", rel)
		}
	}

	uri, err := a.GetPackTexture("Viewer", "textures/items/apple.png", "")
	if err != nil || !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("GetPackTexture = %.30q, %v", uri, err)
	}
	for _, bad := range []string{"../manifest.json", `..\manifest.json`, "textures/../../manifest.json"} {
		if _, err := a.GetPackTexture("Viewer", bad, ""); err == nil {
			t.Errorf("GetPackTexture(%q) should be refused", bad)
		}
	}

	replacement := solidPNG(t, 32, 32, color.NRGBA{R: 77, G: 88, B: 99, A: 255})
	if err := a.SavePackTexture("Viewer", "textures/items/apple.png",
		base64.StdEncoding.EncodeToString(replacement), ""); err != nil {
		t.Fatalf("SavePackTexture: %v", err)
	}
	updated, err := a.ListPackTextures("Viewer", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tex := range updated {
		if tex.RelPath == "textures/items/apple.png" && tex.Size != int64(len(replacement)) {
			t.Errorf("saved texture is %d bytes, want %d", tex.Size, len(replacement))
		}
	}
	if err := a.SavePackTexture("Viewer", "textures/items/apple.png", "not base64", ""); err == nil {
		t.Error("invalid image data should be refused")
	}

	// The thumbnail is cached, so a second call returns the same value.
	thumb1, err := a.GetPackThumb("Viewer", "textures/items/apple.png", "")
	if err != nil || thumb1 == "" {
		t.Fatalf("GetPackThumb: %q, %v", thumb1, err)
	}
	thumb2, err := a.GetPackThumb("Viewer", "textures/items/apple.png", "")
	if err != nil || thumb2 != thumb1 {
		t.Errorf("thumbnail changed between calls: %.20q vs %.20q", thumb1, thumb2)
	}
	if _, err := a.GetPackThumb("Viewer", "textures/items/missing.png", ""); err == nil {
		t.Error("a missing texture should have no thumbnail")
	}
}

// --- Per-pack skins ----------------------------------------------------

func TestPackSkins(t *testing.T) {
	a, packsRoot, _ := featureApp(t)
	writeViewerPack(t, packsRoot, "Viewer")
	skin := solidPNG(t, 64, 64, color.NRGBA{R: 120, G: 60, B: 30, A: 255})
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(skin)

	if a.HasPackSkin("Viewer", "") {
		t.Fatal("a fresh pack has a saved skin")
	}
	if err := a.SavePackSkin("Viewer", ""); err == nil {
		t.Error("saving without a previewed skin should fail")
	}
	if err := a.PreviewPackSkin("Viewer", "", dataURI); err != nil {
		t.Fatalf("PreviewPackSkin: %v", err)
	}
	if a.HasPackSkin("Viewer", "") {
		t.Error("a preview must not be saved yet")
	}
	if err := a.SavePackSkin("Viewer", ""); err != nil {
		t.Fatalf("SavePackSkin: %v", err)
	}
	if !a.HasPackSkin("Viewer", "") {
		t.Fatal("the saved skin is not reported")
	}
	a.ClearPreviewSkins()
	if !a.HasPackSkin("Viewer", "") {
		t.Error("ClearPreviewSkins dropped a saved skin")
	}
	if err := a.ClearPackSkin("Viewer", ""); err != nil {
		t.Fatalf("ClearPackSkin: %v", err)
	}
	if a.HasPackSkin("Viewer", "") {
		t.Error("ClearPackSkin kept the skin")
	}

	// Cache packs get a separate slot from installed packs of the same name.
	cacheRoot := t.TempDir()
	writeViewerPack(t, cacheRoot, "Viewer")
	if err := a.PreviewPackSkin("Viewer", cacheRoot, dataURI); err != nil {
		t.Fatal(err)
	}
	if err := a.SavePackSkin("Viewer", cacheRoot); err != nil {
		t.Fatal(err)
	}
	if !a.HasPackSkin("Viewer", cacheRoot) {
		t.Error("the cache pack's skin is not reported")
	}
	if a.HasPackSkin("Viewer", "") {
		t.Error("the cache pack's skin leaked onto the installed pack")
	}

	for _, bad := range []string{"", "..", "../x", `a/b`} {
		if err := a.PreviewPackSkin(bad, "", dataURI); err == nil {
			t.Errorf("PreviewPackSkin(%q) should be refused", bad)
		}
		if a.HasPackSkin(bad, "") {
			t.Errorf("HasPackSkin(%q) should be false", bad)
		}
	}
	if err := a.PreviewPackSkin("Viewer", "", "nonsense"); err == nil {
		t.Error("a non data URI should be refused")
	}
	tiny := solidPNG(t, 32, 16, color.NRGBA{A: 255})
	if err := a.PreviewPackSkin("Viewer", "", "data:image/png;base64,"+base64.StdEncoding.EncodeToString(tiny)); err == nil {
		t.Error("a skin smaller than 64x32 should be refused")
	}
}

// TestPackViewerOwnSkin: a pack shipping its own player skin shows it.
func TestPackViewerOwnSkin(t *testing.T) {
	a, packsRoot, _ := featureApp(t)
	writeViewerPack(t, packsRoot, "Viewer")

	if uri := a.GetPlayerSkinTexture("Viewer"); !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("GetPlayerSkinTexture = %.30q", uri)
	}
	if uri := a.GetPlayerSkinTexture("Missing"); uri != "" {
		t.Errorf("a missing pack has a skin: %.30q", uri)
	}
}

// --- Pack previews from the server cache --------------------------------

func TestPackPreviewFromCache(t *testing.T) {
	a, _, _ := featureApp(t)
	cacheRoot := t.TempDir()
	writeViewerPack(t, cacheRoot, "Cached")

	info, err := a.GetPackPreviewInfoFromCache("Cached", cacheRoot)
	if err != nil {
		t.Fatalf("GetPackPreviewInfoFromCache: %v", err)
	}
	if info.Name != "Cached" || info.Description != "viewer fixture" {
		t.Errorf("preview = %+v", info)
	}

	textures, err := a.ListPackTextures("Cached", cacheRoot)
	if err != nil || len(textures) == 0 {
		t.Fatalf("ListPackTextures(cache) = %d, %v", len(textures), err)
	}
	if !a.PackHasUiScreensFromCache("Cached", cacheRoot) {
		t.Error("PackHasUiScreensFromCache missed the screens")
	}
	if _, err := a.GetPackItemTexturesFromCache("Cached", "diamond", cacheRoot); err != nil {
		t.Errorf("GetPackItemTexturesFromCache: %v", err)
	}
	if _, err := a.GetPackSkyTexturesFromCache("Cached", cacheRoot); err != nil {
		t.Errorf("GetPackSkyTexturesFromCache: %v", err)
	}
	if _, err := a.GetPackSkySubpacksFromCache("Cached", cacheRoot); err != nil {
		t.Errorf("GetPackSkySubpacksFromCache: %v", err)
	}
}
