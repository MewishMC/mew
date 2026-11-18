package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Sky Converter -------------------------------------------------------

func skyFacePNG(t *testing.T, packPath, face string) image.Image {
	t.Helper()
	r, err := zip.OpenReader(packPath)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	defer r.Close()
	name := "textures/environment/overworld_cubemap/" + face + ".png"
	for _, f := range r.File {
		if filepath.ToSlash(f.Name) != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		img, err := png.Decode(rc)
		if err != nil {
			t.Fatalf("%s is not a png: %v", name, err)
		}
		return img
	}
	t.Fatalf("sky pack has no %s", name)
	return nil
}

// TestCreateSkyPackEquirect is the main Sky Converter path: a 360 image becomes
// six cubemap faces plus a manifest and an icon.
func TestCreateSkyPackEquirect(t *testing.T) {
	a, _, outDir := featureApp(t)

	// A wide gradient so faces differ from one another.
	src := image.NewRGBA(image.Rect(0, 0, 512, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 512; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x / 2), G: uint8(y), B: uint8(255 - x/2), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}

	got, err := a.CreateSkyPack(buf.Bytes(), "my sky.png", 128, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateSkyPack: %v", err)
	}
	want := filepath.Join(outDir, "my sky.mcpack")
	if !samePath(got, want) {
		t.Fatalf("CreateSkyPack wrote %q, want %q", got, want)
	}

	entries := zipEntries(t, got)
	for _, name := range []string{"manifest.json", "pack_icon.png"} {
		if !entries[name] {
			t.Errorf("pack is missing %s", name)
		}
	}
	for i := 0; i < 6; i++ {
		img := skyFacePNG(t, got, "cubemap_"+string(rune('0'+i)))
		if b := img.Bounds(); b.Dx() != 128 || b.Dy() != 128 {
			t.Errorf("cubemap_%d is %dx%d, want 128x128", i, b.Dx(), b.Dy())
		}
	}

	var manifest struct {
		Header struct {
			Name string `json:"name"`
		} `json:"header"`
	}
	if err := json.Unmarshal(readZipEntry(t, got, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Header.Name != "my sky" {
		t.Errorf("pack name = %q, want %q", manifest.Header.Name, "my sky")
	}
}

// TestCreateSkyPackSmallFaceSizeClamped: the UI can ask for a tiny face; the
// backend clamps it so the pack is still usable.
func TestCreateSkyPackSmallFaceSizeClamped(t *testing.T) {
	a, _, _ := featureApp(t)
	got, err := a.CreateSkyPack(solidPNG(t, 256, 128, color.NRGBA{R: 1, A: 255}), "tiny.png", 1, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateSkyPack: %v", err)
	}
	if b := skyFacePNG(t, got, "cubemap_0").Bounds(); b.Dx() != 128 {
		t.Fatalf("face is %dpx, want the 128px minimum", b.Dx())
	}
}

// TestCreateSkyPackCrossMode ports a Java-style cross layout: each face must be
// the matching quadrant of the source image.
func TestCreateSkyPackCrossMode(t *testing.T) {
	a, _, _ := featureApp(t)

	// 4x3 faces, each a distinct colour.
	colors := map[[2]int]color.RGBA{
		{0, 1}: {R: 255, A: 255},         // cubemap_0
		{3, 1}: {G: 255, A: 255},         // cubemap_3
		{1, 0}: {B: 255, A: 255},         // cubemap_4
		{1, 2}: {R: 255, G: 255, A: 255}, // cubemap_5
	}
	const fs = 8
	src := image.NewRGBA(image.Rect(0, 0, 4*fs, 3*fs))
	for y := 0; y < 3*fs; y++ {
		for x := 0; x < 4*fs; x++ {
			c, ok := colors[[2]int{x / fs, y / fs}]
			if !ok {
				c = color.RGBA{A: 255}
			}
			src.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}

	got, err := a.CreateSkyPack(buf.Bytes(), "cross.png", 64, nil, "cross", nil)
	if err != nil {
		t.Fatalf("CreateSkyPack(cross): %v", err)
	}
	checks := []struct {
		face string
		want color.RGBA
	}{
		{"cubemap_0", colors[[2]int{0, 1}]},
		{"cubemap_3", colors[[2]int{3, 1}]},
		{"cubemap_4", colors[[2]int{1, 0}]},
		{"cubemap_5", colors[[2]int{1, 2}]},
	}
	for _, c := range checks {
		img := skyFacePNG(t, got, c.face)
		r, g, b, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
		if uint8(r>>8) != c.want.R || uint8(g>>8) != c.want.G || uint8(b>>8) != c.want.B {
			t.Errorf("%s centre = rgb(%d,%d,%d), want rgb(%d,%d,%d)",
				c.face, r>>8, g>>8, b>>8, c.want.R, c.want.G, c.want.B)
		}
	}
}

// TestCreateSkyPackMergesIntoInstalledPack: "merge into pack" writes the faces
// into an installed pack instead of exporting a new one.
func TestCreateSkyPackMergesIntoInstalledPack(t *testing.T) {
	a, packsRoot, _ := featureApp(t)
	packDir := filepath.Join(packsRoot, "Existing")
	if err := os.MkdirAll(filepath.Join(packDir, "textures", "blocks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "manifest.json"), []byte(`{"header":{"name":"Existing"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := a.CreateSkyPack(
		solidPNG(t, 256, 128, color.NRGBA{G: 100, A: 255}),
		"merged.png", 64,
		[]SkyMergeTarget{{Kind: "installed", Name: "Existing"}},
		"", nil,
	)
	if err != nil {
		t.Fatalf("CreateSkyPack(merge): %v", err)
	}

	face := filepath.Join(packDir, "textures", "environment", "overworld_cubemap", "cubemap_0.png")
	if _, err := os.Stat(face); err != nil {
		t.Fatalf("merge did not write the cubemap into the pack: %v", err)
	}
	// The merge must not clobber the pack's other textures.
	if _, err := os.Stat(filepath.Join(packDir, "textures", "blocks")); err != nil {
		t.Errorf("merge removed the pack's texture folder: %v", err)
	}
}

// TestCreateSkyPackMergeFailsWhenPackIsGone: a stale selection must report an
// error instead of silently doing nothing.
func TestCreateSkyPackMergeFailsWhenPackIsGone(t *testing.T) {
	a, _, _ := featureApp(t)
	_, err := a.CreateSkyPack(solidPNG(t, 128, 64, color.NRGBA{A: 255}), "x.png", 64,
		[]SkyMergeTarget{{Kind: "installed", Name: "NotInstalled"}}, "", nil)
	if err == nil {
		t.Fatal("merging into a missing pack should fail")
	}
}

func TestCreateSkyPackRejectsBadInput(t *testing.T) {
	a, _, _ := featureApp(t)
	if _, err := a.CreateSkyPack(nil, "x.png", 64, nil, "", nil); err == nil {
		t.Error("no image data should be refused")
	}
	pngBytes := solidPNG(t, 64, 32, color.NRGBA{A: 255})
	if _, err := a.CreateSkyPack(pngBytes, "x.gif", 64, nil, "", nil); err == nil {
		t.Error("a .gif should be refused by the sky converter")
	}
	if _, err := a.CreateSkyPack([]byte("not an image"), "x.png", 64, nil, "", nil); err == nil {
		t.Error("invalid image bytes should be refused")
	}
}

// TestCreateSkyPackAutoImport installs the generated sky pack.
func TestCreateSkyPackAutoImport(t *testing.T) {
	a, packsRoot, _ := featureApp(t)
	a.settings["autoImport"] = true

	if _, err := a.CreateSkyPack(solidPNG(t, 256, 128, color.NRGBA{A: 255}), "autosky.png", 64, nil, "", nil); err != nil {
		t.Fatalf("CreateSkyPack: %v", err)
	}
	if _, err := os.Stat(filepath.Join(packsRoot, "autosky", "manifest.json")); err != nil {
		t.Fatalf("auto-import did not install the sky pack: %v", err)
	}
}

// --- Animated Inventory --------------------------------------------------

func testGIF(t *testing.T, frames, w, h int) []byte {
	t.Helper()
	g := &gif.GIF{}
	for i := 0; i < frames; i++ {
		pal := color.Palette{color.RGBA{A: 255}, color.RGBA{R: uint8(40 * (i + 1)), A: 255}}
		img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.SetColorIndex(x, y, uint8(1))
			}
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 6)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func flipbookFrom(t *testing.T, packPath string) image.Image {
	t.Helper()
	data := readZipEntry(t, packPath, "textures/animated_ui/inventory_bg/inventory_vertical_flipbook.png")
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("flipbook is not a png: %v", err)
	}
	return img
}

// TestCreateAnimatedInventoryGIF is the Animated Inventory feature: a gif
// becomes a vertical flipbook plus the UI definitions the game needs.
func TestCreateAnimatedInventoryGIF(t *testing.T) {
	a, _, outDir := featureApp(t)

	got, err := a.CreateAnimatedInventory(testGIF(t, 3, 64, 64), "spinner.gif", "0.06", nil, false, false, nil, "#ff0000")
	if err != nil {
		t.Fatalf("CreateAnimatedInventory: %v", err)
	}
	if !samePath(got, filepath.Join(outDir, "spinner.mcpack")) {
		t.Fatalf("wrote %q", got)
	}

	entries := zipEntries(t, got)
	for _, want := range []string{
		"manifest.json",
		"pack_icon.png",
		"textures/animated_ui/inventory_bg/inventory_vertical_flipbook.png",
		"textures/animated_ui/inventory_bg/inventory_overlay.png",
		"ui/_global_variables.json",
		"ui/_ui_defs.json",
		"ui/inventory_screen.json",
	} {
		if !entries[want] {
			t.Errorf("pack is missing %s", want)
		}
	}
	if b := flipbookFrom(t, got).Bounds(); b.Dx() != animFrameWidth || b.Dy() != animFrameHeight*3 {
		t.Errorf("flipbook is %dx%d, want %dx%d (3 frames)", b.Dx(), b.Dy(), animFrameWidth, animFrameHeight*3)
	}

	vars := string(readZipEntry(t, got, "ui/_global_variables.json"))
	if !strings.Contains(vars, "03") {
		t.Errorf("_global_variables.json does not carry the frame count:\n%s", vars)
	}
	if strings.Contains(vars, "$num") {
		t.Error("_global_variables.json still has the $num placeholder")
	}
}

// TestCreateAnimatedInventoryStatic: a still image is accepted too, as one frame.
func TestCreateAnimatedInventoryStatic(t *testing.T) {
	a, _, _ := featureApp(t)

	got, err := a.CreateAnimatedInventory(solidPNG(t, 64, 64, color.NRGBA{G: 200, A: 255}), "still.png", "0.06", nil, false, false, nil, "#00ff00")
	if err != nil {
		t.Fatalf("CreateAnimatedInventory(static): %v", err)
	}
	if b := flipbookFrom(t, got).Bounds(); b.Dy() != animFrameHeight {
		t.Errorf("static flipbook is %dpx tall, want %d", b.Dy(), animFrameHeight)
	}
}

// TestCreateAnimatedInventoryRejectsBadInput guards the UI-facing validation.
func TestCreateAnimatedInventoryRejectsBadInput(t *testing.T) {
	a, _, _ := featureApp(t)
	gifBytes := testGIF(t, 2, 32, 32)

	if _, err := a.CreateAnimatedInventory(nil, "x.gif", "0.06", nil, false, false, nil, ""); err == nil {
		t.Error("no data should be refused")
	}
	if _, err := a.CreateAnimatedInventory(gifBytes, "x.txt", "0.06", nil, false, false, nil, ""); err == nil {
		t.Error("a .txt should be refused")
	}
	for _, d := range []string{"0.01", "0.5", "fast", ""} {
		if _, err := a.CreateAnimatedInventory(gifBytes, "x.gif", d, nil, false, false, nil, ""); err == nil {
			t.Errorf("frame duration %q should be refused", d)
		}
	}
	if _, err := a.CreateAnimatedInventory([]byte("not a gif"), "x.gif", "0.06", nil, false, false, nil, ""); err == nil {
		t.Error("invalid gif bytes should be refused")
	}
}

// TestCreateAnimatedInventoryMarriesOverlay: a custom overlay is scaled onto the
// 352x332 frame.
func TestCreateAnimatedInventoryMarriesOverlay(t *testing.T) {
	a, _, _ := featureApp(t)
	overlay := solidPNG(t, 32, 32, color.NRGBA{B: 255, A: 255})

	got, err := a.CreateAnimatedInventory(testGIF(t, 2, 48, 48), "overlay.gif", "0.07", overlay, true, true, nil, "#123456")
	if err != nil {
		t.Fatalf("CreateAnimatedInventory(overlay): %v", err)
	}
	data := readZipEntry(t, got, "textures/animated_ui/inventory_bg/inventory_overlay.png")
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != animFrameWidth || b.Dy() != animFrameHeight {
		t.Errorf("overlay is %dx%d, want %dx%d", b.Dx(), b.Dy(), animFrameWidth, animFrameHeight)
	}
}
