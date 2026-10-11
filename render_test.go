package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// solidPNG encodes a solid-color PNG, the procedural texture the render tests
// use instead of shipping game art.
func solidPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// testApp builds an app rooted at a temp resource-packs dir with a temp
// vanilla cache, so nothing here touches the network.
func testApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	root := t.TempDir()
	a := NewApp(false)
	a.settings["resourcePacksPath"] = root
	// Tests never touch the network: vanilla textures come from the seeded
	// cache, and anything else fails fast.
	offlineVanilla(t)
	return a
}

func decodeDataURI(t *testing.T, uri string) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(dataURIBytes(t, uri)))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func dataURIBytes(t *testing.T, uri string) []byte {
	t.Helper()
	comma := strings.IndexByte(uri, ',')
	if comma < 0 {
		t.Fatalf("not a data URI: %q", uri)
	}
	data, err := base64.StdEncoding.DecodeString(uri[comma+1:])
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func countOpaque(img image.Image) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				n++
			}
		}
	}
	return n
}

func TestResolversPreferPackThenVanilla(t *testing.T) {
	a := testApp(t)
	root := a.getResourcePacksPath()

	packArmor := solidPNG(t, 64, 32, color.NRGBA{255, 0, 0, 255})
	vanillaArmor := solidPNG(t, 64, 32, color.NRGBA{0, 255, 0, 255})
	packSword := solidPNG(t, 16, 16, color.NRGBA{0, 0, 255, 255})
	vanillaSword := solidPNG(t, 16, 16, color.NRGBA{255, 255, 0, 255})

	writeFile(t, filepath.Join(root, "pack", "textures", "models", "armor", "diamond_1.png"), packArmor)
	writeFile(t, filepath.Join(vanillaCacheDir(), "textures", "models", "armor", "diamond_2.png"), vanillaArmor)
	writeFile(t, filepath.Join(root, "pack", "textures", "items", "diamond_sword.png"), packSword)
	writeFile(t, filepath.Join(vanillaCacheDir(), "textures", "items", "iron_sword.png"), vanillaSword)

	if got := a.armorLayer(a.getPackDir("pack"), "diamond", 1); got == nil || !isRed(got.img) {
		t.Errorf("layer 1 should come from the pack")
	}
	if got := a.armorLayer(a.getPackDir("pack"), "diamond", 2); got == nil || !isGreen(got.img) {
		t.Errorf("layer 2 should fall back to vanilla")
	}
	if got := a.itemTexture(a.getPackDir("pack"), "diamond_sword"); got == nil || !isBlue(got.img) {
		t.Errorf("diamond_sword should come from the pack")
	}
	if got := a.itemTexture(a.getPackDir("pack"), "iron_sword"); got == nil || !isYellow(got.img) {
		t.Errorf("iron_sword should fall back to vanilla")
	}
	if got := a.itemTexture(a.getPackDir("pack"), "diamond_hoe"); got != nil {
		t.Errorf("an allow-listed but uncached item should be nil offline, got %v", got)
	}
}

// offlineVanilla points the vanilla client at a counting transport and restores
// it when the test ends.
func offlineVanilla(t *testing.T) *int {
	t.Helper()
	count := new(int)
	prev := vanillaClient.Transport
	vanillaClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		*count++
		return nil, errors.New("offline")
	})
	t.Cleanup(func() { vanillaClient.Transport = prev })
	return count
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestGoldenAppleUsesBedrockTextureName pins the golden apple's real Bedrock
// sprite name. Bedrock ships it as "apple_golden.png"; asking vanilla for
// "golden_apple.png" 404s, so the item used to silently not appear.
func TestGoldenAppleUsesBedrockTextureName(t *testing.T) {
	a := testApp(t)
	root := a.getResourcePacksPath()
	writeFile(t, filepath.Join(vanillaCacheDir(), "textures", "items", "apple_golden.png"), solidPNG(t, 16, 16, color.NRGBA{255, 255, 0, 255}))

	if got := a.itemTexture("", "golden_apple"); got == nil || !isYellow(got.img) {
		t.Fatal("golden_apple should fall back to vanilla apple_golden.png")
	}
	// A pack that names the sprite the friendly way still wins.
	writeFile(t, filepath.Join(root, "pack", "textures", "items", "golden_apple.png"), solidPNG(t, 16, 16, color.NRGBA{255, 0, 0, 255}))
	if got := a.itemTexture(a.getPackDir("pack"), "golden_apple"); got == nil || !isRed(got.img) {
		t.Fatal("pack's golden_apple.png should be preferred")
	}
	// And a pack using Bedrock's own name works too.
	writeFile(t, filepath.Join(root, "bedrockPack", "textures", "items", "apple_golden.png"), solidPNG(t, 16, 16, color.NRGBA{0, 0, 255, 255}))
	if got := a.itemTexture(a.getPackDir("bedrockPack"), "golden_apple"); got == nil || !isBlue(got.img) {
		t.Fatal("pack's apple_golden.png should be found via the alias")
	}
}

// TestBedrockTierNamesNormalized: Bedrock names the tiers wood_* and gold_*,
// not Java's wooden_*/golden_*. A request for the latter must still resolve.
func TestBedrockTierNamesNormalized(t *testing.T) {
	a := testApp(t)
	writeFile(t, filepath.Join(vanillaCacheDir(), "textures", "items", "gold_hoe.png"), solidPNG(t, 16, 16, color.NRGBA{255, 255, 0, 255}))
	writeFile(t, filepath.Join(vanillaCacheDir(), "textures", "items", "wood_axe.png"), solidPNG(t, 16, 16, color.NRGBA{0, 255, 0, 255}))

	if got := a.itemTexture("", "golden_hoe"); got == nil || !isYellow(got.img) {
		t.Fatal("golden_hoe should resolve to vanilla gold_hoe.png")
	}
	if got := a.itemTexture("", "wooden_axe"); got == nil || !isGreen(got.img) {
		t.Fatal("wooden_axe should resolve to vanilla wood_axe.png")
	}
	if got := a.itemTexture("", "gold_hoe"); got == nil || !isYellow(got.img) {
		t.Fatal("the canonical gold_hoe should fetch directly")
	}
}

func TestItemTextureUnknownNameNeverFetches(t *testing.T) {
	a := testApp(t)
	requests := offlineVanilla(t)

	if got := a.itemTexture(a.getPackDir("pack"), "totally_made_up"); got != nil {
		t.Errorf("unknown item should be nil, got %v", got)
	}
	if got := a.itemTexture(a.getPackDir("pack"), "../../secret"); got != nil {
		t.Errorf("path-like item should be nil, got %v", got)
	}
	if *requests != 0 {
		t.Errorf("unknown names must not hit the network, got %d requests", *requests)
	}
}

func TestPlayerTexturesBasics(t *testing.T) {
	a := testApp(t)
	root := a.getResourcePacksPath()
	skin := solidPNG(t, 64, 64, color.NRGBA{120, 120, 120, 255})
	writeFile(t, filepath.Join(root, "withSkin", "textures", "entity", "steve.png"), skin)
	writeFile(t, filepath.Join(root, "armorOnly", "textures", "models", "armor", "diamond_1.png"), skin)
	writeFile(t, filepath.Join(root, "armorOnly", "textures", "models", "armor", "diamond_2.png"), skin)

	for _, req := range []PlayerRequest{
		{Pack: "withSkin"},
		{Pack: "armorOnly", Material: "diamond"},
		{},
	} {
		tex, err := a.GetPlayerTextures(req)
		if err != nil {
			t.Fatalf("%+v: %v", req, err)
		}
		if !strings.HasPrefix(tex.Skin, "data:image/png;base64,") {
			t.Fatalf("%+v: skin is not a PNG data URI", req)
		}
		if got := decodeDataURI(t, tex.Skin).Bounds().Dx(); got != 64 {
			t.Fatalf("%+v: skin is %dpx wide, want 64", req, got)
		}
		if (req.Material != "") != (tex.Layer1 != "" && tex.Layer2 != "") {
			t.Fatalf("%+v: armor layers %v/%v", req, tex.Layer1 != "", tex.Layer2 != "")
		}
	}
	// A pack's PNG reaches the viewer byte for byte, as the game reads it.
	tex, err := a.GetPlayerTextures(PlayerRequest{Pack: "withSkin"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dataURIBytes(t, tex.Skin), skin) {
		t.Error("the pack's skin PNG should be passed through unchanged")
	}
}

// "Change Skin" previews a skin on one pack: it replaces that pack's own skin
// and no other's, and closing the viewer drops it. Saving keeps it for that
// pack across restarts, and clearing brings the pack's own skin back.
func TestPackSkinPreviewAndSave(t *testing.T) {
	a := testApp(t)
	root := a.getResourcePacksPath()
	blue := solidPNG(t, 64, 64, color.NRGBA{0, 0, 255, 255})
	writeFile(t, filepath.Join(root, "one", "textures", "entity", "steve.png"), blue)
	writeFile(t, filepath.Join(root, "two", "textures", "entity", "steve.png"), blue)
	render := func(a *App, pack string) string {
		t.Helper()
		tex, err := a.GetPlayerTextures(PlayerRequest{Pack: pack})
		if err != nil {
			t.Fatal(err)
		}
		return tex.Skin
	}
	before := render(a, "one")
	red := "data:image/png;base64," + base64.StdEncoding.EncodeToString(solidPNG(t, 64, 64, color.NRGBA{255, 0, 0, 255}))

	// A preview shows on its pack only, and closing the viewer drops it.
	if err := a.PreviewPackSkin("one", "", red); err != nil {
		t.Fatal(err)
	}
	changed := render(a, "one")
	if changed == before {
		t.Fatal("the previewed skin should replace the pack's own")
	}
	if render(a, "two") != before {
		t.Fatal("previewing on one pack changed another pack")
	}
	if a.HasPackSkin("one", "") {
		t.Fatal("a preview is not saved until Save")
	}
	a.ClearPreviewSkins()
	if render(a, "one") != before {
		t.Fatal("closing the viewer should drop the preview")
	}
	if err := a.SavePackSkin("one", ""); err == nil {
		t.Fatal("saving with nothing previewed should fail")
	}

	// Saved, it stays for that pack, across a restart.
	if err := a.PreviewPackSkin("one", "", red); err != nil {
		t.Fatal(err)
	}
	if err := a.SavePackSkin("one", ""); err != nil {
		t.Fatal(err)
	}
	a.ClearPreviewSkins()
	if !a.HasPackSkin("one", "") || a.HasPackSkin("two", "") {
		t.Fatal("HasPackSkin should be true for the saved pack only")
	}
	if render(a, "one") != changed {
		t.Fatal("the saved skin should show after the viewer closes")
	}
	restarted := NewApp(false)
	restarted.settings["resourcePacksPath"] = root
	if render(restarted, "one") != changed {
		t.Fatal("the saved skin should still be there after a restart")
	}

	if err := a.ClearPackSkin("one", ""); err != nil {
		t.Fatal(err)
	}
	if render(a, "one") != before {
		t.Fatal("clearing should bring the pack's own skin back")
	}

	for _, bad := range []string{"", "..", "../x", `a/b`, `a\b`} {
		if err := a.PreviewPackSkin(bad, "", red); err == nil {
			t.Errorf("PreviewPackSkin(%q) should be refused", bad)
		}
	}
	if err := a.PreviewPackSkin("one", "", "data:image/png;base64,AAAA"); err == nil {
		t.Error("a non-image should be refused")
	}
}

func TestMissingEquipmentFallsBackToPlaceholder(t *testing.T) {
	a := testApp(t) // empty vanilla cache, network disabled
	tex, err := a.GetPlayerTextures(PlayerRequest{
		Material: "diamond",
		Elytra:   true,
		Right:    "diamond_sword",
		Left:     "bread",
	})
	if err != nil {
		t.Fatal(err)
	}
	p := placeholderTexture()
	if p == nil || p.img.Bounds().Dx() != 16 {
		t.Fatalf("placeholder should be a 16px image, got %v", p)
	}
	for name, uri := range map[string]string{"layer1": tex.Layer1, "layer2": tex.Layer2, "elytra": tex.Elytra, "right": tex.Right.Item, "left": tex.Left.Item} {
		if uri != p.dataURI() {
			t.Errorf("missing %s should fall back to the placeholder, not vanish", name)
		}
	}
	if tex.Right.Flat || !tex.Left.Flat {
		t.Error("a sword is held upright and bread flat")
	}
	if uri, err := a.GetItemTexture("", "", "diamond_sword"); err != nil || uri != p.dataURI() {
		t.Errorf("GetItemTexture should fall back to the placeholder, got %v", err)
	}
}

func TestPlayerRequestValidation(t *testing.T) {
	a := testApp(t)
	if _, err := a.GetPlayerTextures(PlayerRequest{Pack: "../escape"}); err == nil {
		t.Error("expected a pack name with a path to be rejected")
	}
	if _, err := a.GetPlayerTextures(PlayerRequest{Right: "../../a"}); err == nil {
		t.Error("expected an item name with a path to be rejected")
	}
	if _, err := a.GetPlayerTextures(PlayerRequest{Material: "topaz"}); err == nil {
		t.Error("expected an unknown armor material to be rejected")
	}
	if _, err := a.GetPlayerTextures(PlayerRequest{Model: "giant"}); err == nil {
		t.Error("expected an unknown model to be rejected")
	}
	if _, err := a.GetItemTexture("../escape", "", "stick"); err == nil {
		t.Error("expected GetItemTexture to reject a pack name with a path")
	}
	if tex, err := a.GetPlayerTextures(PlayerRequest{Model: "slim"}); err != nil || !tex.Slim {
		t.Errorf("model slim should be slim, got %v", err)
	}
}

func TestIsHandEquipped(t *testing.T) {
	upright := []string{"diamond_sword", "wooden_axe", "netherite_pickaxe", "iron_shovel", "golden_hoe", "stick", "bone", "blaze_rod", "breeze_rod", "fishing_rod", "carrot_on_a_stick", "warped_fungus_on_a_stick", "mace"}
	flat := []string{"bread", "apple", "golden_apple", "cooked_beef", "ender_pearl", "bow", "totem"}
	for _, name := range upright {
		if !isHandEquipped(name) {
			t.Errorf("%s should be held upright", name)
		}
	}
	for _, name := range flat {
		if isHandEquipped(name) {
			t.Errorf("%s should be held flat", name)
		}
	}
}

func TestIsSlimSkin(t *testing.T) {
	wide := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			wide.SetNRGBA(x, y, color.NRGBA{120, 120, 120, 255})
		}
	}
	if isSlimSkin(wide) {
		t.Error("a fully opaque skin should be wide")
	}

	slim := cloneImage(wide)
	slim.SetNRGBA(50, 16, color.NRGBA{0, 0, 0, 0})
	if !isSlimSkin(slim) {
		t.Error("a transparent unused area should make a skin slim")
	}

	black := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			black.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 255})
		}
	}
	if !isSlimSkin(black) {
		t.Error("all-black unused areas should make a skin slim")
	}

	hd := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			hd.SetNRGBA(x, y, color.NRGBA{120, 120, 120, 255})
		}
	}
	if isSlimSkin(hd) {
		t.Error("a fully opaque HD skin should be wide")
	}
	hd.SetNRGBA(100, 32, color.NRGBA{0, 0, 0, 0}) // (50,16) scaled by 2
	if !isSlimSkin(hd) {
		t.Error("a transparent unused area on an HD skin should make it slim")
	}
}

func cloneImage(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func isRed(img image.Image) bool {
	r, g, b, _ := img.At(img.Bounds().Min.X, img.Bounds().Min.Y).RGBA()
	return r > 0xf000 && g < 0x1000 && b < 0x1000
}

func isGreen(img image.Image) bool {
	r, g, b, _ := img.At(img.Bounds().Min.X, img.Bounds().Min.Y).RGBA()
	return g > 0xf000 && r < 0x1000 && b < 0x1000
}

func isBlue(img image.Image) bool {
	r, g, b, _ := img.At(img.Bounds().Min.X, img.Bounds().Min.Y).RGBA()
	return b > 0xf000 && r < 0x1000 && g < 0x1000
}

func isYellow(img image.Image) bool {
	r, g, b, _ := img.At(img.Bounds().Min.X, img.Bounds().Min.Y).RGBA()
	return r > 0xf000 && g > 0xf000 && b < 0x1000
}

// A server pack renders from the pack cache folder with its own skin, any
// other folder is refused, and a skin chosen for it is kept apart from an
// installed pack with the same folder name.
func TestServerPackTextures(t *testing.T) {
	a := testApp(t)
	cache := t.TempDir()
	a.settings["packCachePath"] = cache
	writeFile(t, filepath.Join(cache, "srv", "textures", "entity", "steve.png"), solidPNG(t, 64, 64, color.NRGBA{0, 0, 255, 255}))
	writeFile(t, filepath.Join(a.getResourcePacksPath(), "srv", "textures", "entity", "steve.png"), solidPNG(t, 64, 64, color.NRGBA{0, 255, 0, 255}))

	skin := func(req PlayerRequest) string {
		t.Helper()
		tex, err := a.GetPlayerTextures(req)
		if err != nil {
			t.Fatal(err)
		}
		return tex.Skin
	}
	server := skin(PlayerRequest{Pack: "srv", Base: cache})
	installed := skin(PlayerRequest{Pack: "srv"})
	if server == installed {
		t.Fatal("the server pack should render from the cache folder, not the installed pack")
	}
	if _, err := a.GetPlayerTextures(PlayerRequest{Pack: "srv", Base: t.TempDir()}); err == nil {
		t.Fatal("a folder other than the pack cache should be refused")
	}

	red := "data:image/png;base64," + base64.StdEncoding.EncodeToString(solidPNG(t, 64, 64, color.NRGBA{255, 0, 0, 255}))
	if err := a.PreviewPackSkin("srv", cache, red); err != nil {
		t.Fatal(err)
	}
	if err := a.SavePackSkin("srv", cache); err != nil {
		t.Fatal(err)
	}
	if !a.HasPackSkin("srv", cache) || a.HasPackSkin("srv", "") {
		t.Fatal("a skin saved for a server pack must not apply to the installed pack")
	}
	if skin(PlayerRequest{Pack: "srv"}) != installed {
		t.Fatal("the installed pack should keep its own skin")
	}
}
