package main

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // packs sometimes ship JPEG textures
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/woozymasta/tga"
)

// The player is drawn in the frontend by bedrock-skin-viewer. This file finds
// the textures it draws with - the pack's, else vanilla's, else MEW's own -
// and hands them over as PNG data URIs. Every path a request names is resolved
// here, so the frontend can never read outside the pack folders.

// maxTextureEdge bounds every texture the service decodes. HD packs ship 32
// or 64 pixel armor and item textures; anything far bigger is not one.
const maxTextureEdge = 1024

// mewDefaultSkin is MEW's own fallback skin, used when neither the pack nor
// the user's saved default skin provides one. It is MEW's asset, not game art.
//
//go:embed frontend/src/assets/default-skin.png
var mewDefaultSkin []byte

var (
	mewDefaultSkinOnce sync.Once
	mewDefaultSkinTex  *texture
)

// itemNameRe is the shape of an item texture name a pack may be asked for.
var itemNameRe = regexp.MustCompile(`^[a-z0-9_]+$`)

// vanillaItemNames are the items a vanilla texture may be fetched for. A name
// outside this set never becomes a network request, however it was spelled.
var vanillaItemNames = map[string]bool{}

func init() {
	for _, tier := range []string{"wood", "stone", "iron", "gold", "diamond", "netherite"} {
		for _, kind := range []string{"sword", "pickaxe", "axe", "shovel", "hoe"} {
			vanillaItemNames[tier+"_"+kind] = true
		}
	}
	for _, name := range []string{
		"stick", "bone", "blaze_rod", "fishing_rod", "mace", "bow", "bread",
		"apple", "cooked_beef", "apple_golden", "ender_pearl", "totem",
	} {
		vanillaItemNames[name] = true
	}
}

// itemAliases maps a friendly name to a wholly different Bedrock texture name.
// Bedrock ships the golden apple's sprite as "apple_golden", so a request for
// "golden_apple" would 404 and the item would silently not render.
var itemAliases = map[string]string{
	"golden_apple": "apple_golden",
}

// itemNameVariants returns the texture names to try for one requested item,
// most-specific first. Java-style tier prefixes ("wooden_axe", "golden_hoe")
// are mapped to Bedrock's own ("wood_axe", "gold_hoe"); everything else is
// tried as given, then by alias.
func itemNameVariants(name string) []string {
	variants := []string{name}
	if a, ok := itemAliases[name]; ok {
		variants = append(variants, a)
	}
	for _, pair := range [][2]string{{"wooden_", "wood_"}, {"golden_", "gold_"}} {
		if strings.HasPrefix(name, pair[0]) {
			variants = append(variants, pair[1]+strings.TrimPrefix(name, pair[0]))
		}
	}
	return variants
}

// PlayerRequest names what the player wears. Zero values are sensible: no
// pack, no armor, no items.
type PlayerRequest struct {
	Pack     string `json:"pack"`     // dir name; "" uses no pack (vanilla + default skin)
	Base     string `json:"base"`     // the folder Pack is in: "" for installed packs, else the server pack cache
	Model    string `json:"model"`    // "auto", "wide", "slim"
	Material string `json:"material"` // "" or "none" for no armor, else a material
	Elytra   bool   `json:"elytra"`   // an elytra in place of the chestplate
	Right    string `json:"right"`    // the right hand's item; "" holds nothing
	Left     string `json:"left"`     // the left hand's item
}

// PlayerTextures is everything the viewer draws the player with, each a PNG
// data URI. An empty texture is not worn.
type PlayerTextures struct {
	Skin   string      `json:"skin"`
	Slim   bool        `json:"slim"`
	Layer1 string      `json:"layer1"` // armor: helmet, chestplate, boots
	Layer2 string      `json:"layer2"` // armor: leggings
	Elytra string      `json:"elytra"`
	Right  HeldTexture `json:"right"`
	Left   HeldTexture `json:"left"`
}

// HeldTexture is one hand's item: its sprite, and whether the game holds it
// flat (food, materials) rather than upright (tools, weapons).
type HeldTexture struct {
	Item string `json:"item"`
	Flat bool   `json:"flat"`
}

// texture is a decoded image and the PNG bytes the frontend draws it from.
type texture struct {
	img  image.Image
	data []byte
}

// dataURI is the texture as a PNG data URI, or "" for nil.
func (t *texture) dataURI() string {
	if t == nil {
		return ""
	}
	return pngDataURI(t.data)
}

// --- texture resolution ---

// packDirFor resolves a pack named by the frontend to its folder: one of the
// installed packs when base is "", else one in the server pack cache. Any
// other base, or a name that is not a plain folder name, is refused, so a
// request can never read outside those two folders. No pack is "".
func (a *App) packDirFor(base, packName string) (string, error) {
	if packName == "" {
		return "", nil
	}
	if filepath.Base(packName) != packName || packName == "." || packName == ".." {
		return "", fmt.Errorf("invalid pack name")
	}
	if base == "" {
		return a.getPackDir(packName), nil
	}
	if !samePath(base, a.packCachePath()) {
		return "", fmt.Errorf("unknown pack folder")
	}
	return filepath.Join(base, packName), nil
}

// packCachePath is the server pack cache folder: the setting, else the game's.
func (a *App) packCachePath() string {
	if p := a.getStringSetting("packCachePath"); p != "" {
		return p
	}
	return a.getDefaultPackCachePath()
}

// samePath reports whether two paths name the same folder, ignoring case and
// a trailing separator, as Windows does.
func samePath(x, y string) bool {
	return x != "" && y != "" && strings.EqualFold(filepath.Clean(x), filepath.Clean(y))
}

// skinPath is the pack's player skin, or "" when it does not override one.
func skinPath(dir string) string {
	if dir == "" {
		return ""
	}
	return findPackSkin(dir)
}

// packArmorPath is the pack's texture for one armor layer, or "".
func packArmorPath(dir string, material string, layer int) string {
	if dir == "" {
		return ""
	}
	base := fmt.Sprintf("%s_%d", material, layer)
	return findFirstImage(filepath.Join(dir, "textures", "models", "armor"), base)
}

// packElytraPath is the pack's elytra texture, or "".
func packElytraPath(dir string) string {
	if dir == "" {
		return ""
	}
	return findFirstImage(filepath.Join(dir, "textures", "models", "armor"), "elytra")
}

// packItemPath is the pack's texture for an item, or "".
func packItemPath(dir string, name string) string {
	if dir == "" {
		return ""
	}
	return findFirstImage(filepath.Join(dir, "textures", "items"), name)
}

// defaultSkinPath is the user's saved default skin.
func defaultSkinPath() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "mew", "default_skin.png")
}

// defaultSkin returns the user's default skin, else MEW's own embedded one. It
// is the fallback for a pack without a skin.
func (a *App) defaultSkin() *texture {
	if p := defaultSkinPath(); p != "" {
		if t := a.loadFileTexture(p); t != nil {
			return t
		}
	}
	return mewSkinTexture()
}

// skinFor returns the player skin to draw: a skin being previewed for this
// pack with "Change Skin", else the one saved for it, else the pack's own,
// else the user's default skin, else MEW's own embedded one.
func (a *App) skinFor(key, dir string) *texture {
	if t := a.previewSkin(key); t != nil {
		return t
	}
	if p := chosenSkinPath(key); p != "" {
		if t := a.loadFileTexture(p); t != nil {
			return t
		}
	}
	if p := skinPath(dir); p != "" {
		if t := a.loadFileTexture(p); t != nil {
			return t
		}
	}
	return a.defaultSkin()
}

// armorLayer returns one armor layer's texture: the pack's, else vanilla's for
// a material vanilla ships. It returns nil for anything else.
func (a *App) armorLayer(dir string, material string, layer int) *texture {
	if dir != "" {
		if p := packArmorPath(dir, material, layer); p != "" {
			if t := a.loadFileTexture(p); t != nil {
				return t
			}
		}
	}
	if vanillaArmorMaterials[material] {
		return a.loadVanillaTexture(fmt.Sprintf("textures/models/armor/%s_%d.png", material, layer))
	}
	return nil
}

// elytraTexture returns the elytra's texture: the pack's, else vanilla's.
func (a *App) elytraTexture(dir string) *texture {
	if dir != "" {
		if p := packElytraPath(dir); p != "" {
			if t := a.loadFileTexture(p); t != nil {
				return t
			}
		}
	}
	return a.loadVanillaTexture("textures/models/armor/elytra.png")
}

// itemTexture returns an item's sprite: the pack's texture for that name when
// it has one, else vanilla's for the names vanilla ships. A name that is not
// a valid texture name, or whose variants are not in the vanilla allow-list,
// returns nil without ever fetching anything. Variants map friendly/Java names
// to Bedrock's own (see itemNameVariants); the pack is checked under each.
func (a *App) itemTexture(dir string, name string) *texture {
	if !itemNameRe.MatchString(name) {
		return nil
	}
	variants := itemNameVariants(name)
	if dir != "" {
		for _, n := range variants {
			if p := packItemPath(dir, n); p != "" {
				if t := a.loadFileTexture(p); t != nil {
					return t
				}
			}
		}
	}
	for _, n := range variants {
		if vanillaItemNames[n] {
			return a.loadVanillaTexture("textures/items/" + n + ".png")
		}
	}
	return nil
}

// loadFileTexture decodes an image file, cached by path, size and mtime so an
// edit in the Recolor tool is picked up.
func (a *App) loadFileTexture(path string) *texture {
	if path == "" {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	key := fmt.Sprintf("tex\x00%s\x00%d\x00%d", path, st.Size(), st.ModTime().UnixNano())
	if v, ok := a.thumbCache.Load(key); ok {
		t, _ := v.(*texture)
		return t
	}
	t, err := loadTextureFile(path)
	if err != nil {
		a.logDebug(fmt.Sprintf("mew: texture %s: %v", path, err))
		a.thumbCache.Store(key, missingTexture{})
		return nil
	}
	a.thumbCache.Store(key, t)
	return t
}

// loadVanillaTexture decodes an immutable vanilla texture, cached by its
// resource path. Only a definitive 404 is cached as missing; a transient
// failure is retried, so a download that fails mid-run does not poison the
// texture for the rest of the session.
func (a *App) loadVanillaTexture(rel string) *texture {
	key := "vtex\x00" + rel
	if v, ok := a.thumbCache.Load(key); ok {
		t, _ := v.(*texture)
		return t
	}
	data, err := a.readVanillaFile(rel)
	if err != nil {
		a.logDebug(fmt.Sprintf("mew: vanilla texture %s: %v", rel, err))
		// Only a definitive 404 is cached as missing. A transient failure must
		// not poison the texture for the rest of the session.
		if errors.Is(err, errVanillaNotFound) {
			a.thumbCache.Store(key, missingTexture{})
		}
		return nil
	}
	t, err := newTexture(data)
	if err != nil {
		a.logDebug(fmt.Sprintf("mew: vanilla texture %s: %v", rel, err))
		a.thumbCache.Store(key, missingTexture{})
		return nil
	}
	a.thumbCache.Store(key, t)
	return t
}

// missingTexture marks a cache entry as a texture that could not be loaded.
type missingTexture struct{}

// loadTextureFile reads a pack image, handling the TGA files packs ship
// alongside PNGs.
func loadTextureFile(path string) (*texture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".tga") {
		img, err := tga.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return textureFromImage(img)
	}
	return newTexture(data)
}

// newTexture decodes encoded image bytes. A PNG is kept as it is, so the
// frontend reads exactly the file the game reads; anything else (a JPEG) is
// re-encoded as one.
func newTexture(data []byte) (*texture, error) {
	img, err := decodeTexture(data)
	if err != nil {
		return nil, err
	}
	if isPNG(data) {
		return &texture{img: img, data: data}, nil
	}
	return textureFromImage(img)
}

// textureFromImage encodes a decoded image as a PNG texture.
func textureFromImage(img image.Image) (*texture, error) {
	if b := img.Bounds(); b.Dx() > maxTextureEdge || b.Dy() > maxTextureEdge {
		return nil, fmt.Errorf("texture is %dx%d, larger than %d", b.Dx(), b.Dy(), maxTextureEdge)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &texture{img: img, data: buf.Bytes()}, nil
}

// isPNG reports whether data starts with the PNG signature.
func isPNG(data []byte) bool {
	return bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n"))
}

// decodeTexture decodes encoded image bytes, bounding the edge length first so
// a hostile header cannot ask for a huge allocation.
func decodeTexture(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, errors.New("no image data")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("not a valid image: %w", err)
	}
	if cfg.Width > maxTextureEdge || cfg.Height > maxTextureEdge {
		return nil, fmt.Errorf("texture is %dx%d, larger than %d", cfg.Width, cfg.Height, maxTextureEdge)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("not a valid image: %w", err)
	}
	return img, nil
}

// mewSkinTexture decodes MEW's embedded fallback skin once.
func mewSkinTexture() *texture {
	mewDefaultSkinOnce.Do(func() {
		t, err := newTexture(mewDefaultSkin)
		if err != nil {
			debugLogf("mew: embedded default skin: %v", err)
			return
		}
		mewDefaultSkinTex = t
	})
	return mewDefaultSkinTex
}

var (
	placeholderOnce sync.Once
	placeholderTex  *texture
)

// placeholderTexture is MEW's own "missing texture" marker: a magenta and black
// checkerboard. It stands in for an armor piece, item or elytra whose texture
// could not be found in the pack or in vanilla - for example while the vanilla
// cache is empty and offline, or right after it was cleared - so the equipment
// stays visible and obviously provisional instead of silently disappearing. It
// is generated, never bundled game art.
func placeholderTexture() *texture {
	placeholderOnce.Do(func() {
		const size = 16
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		magenta := color.NRGBA{R: 255, A: 255}
		black := color.NRGBA{A: 255}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				if (x/4+y/4)%2 == 0 {
					img.SetNRGBA(x, y, magenta)
				} else {
					img.SetNRGBA(x, y, black)
				}
			}
		}
		placeholderTex, _ = textureFromImage(img)
	})
	return placeholderTex
}

// orPlaceholder returns t when a texture resolved, else the missing-texture
// marker, so the viewer never silently drops a requested piece.
func orPlaceholder(t *texture) *texture {
	if t == nil {
		return placeholderTexture()
	}
	return t
}

// --- model detection ---

// slimAreas are the four 2-pixel-wide regions a slim skin leaves unused, in
// the 64x64 layout. A skin is slim when any of them is not fully opaque, or
// all four are entirely black, or all four entirely white - the same test
// skinview3d's "Auto" used, so existing users see the same arms.
var slimAreas = [][4]int{{50, 16, 2, 4}, {54, 20, 2, 12}, {42, 48, 2, 4}, {46, 52, 2, 12}}

// isSlimSkin reports whether a skin's arms are slim. HD skins scale the tested
// areas by width/64.
func isSlimSkin(img image.Image) bool {
	if img == nil {
		return false
	}
	b := img.Bounds()
	if b.Dx() < 64 {
		return false
	}
	scale := float64(b.Dx()) / 64
	transparent, allBlack, allWhite := false, true, true
	for _, area := range slimAreas {
		hasClear, black, white := scanSlimArea(img, b, area, scale)
		if hasClear {
			transparent = true
		}
		if !black {
			allBlack = false
		}
		if !white {
			allWhite = false
		}
	}
	return transparent || allBlack || allWhite
}

// scanSlimArea reports whether an area has a non-opaque pixel, is all black or
// is all white.
func scanSlimArea(img image.Image, b image.Rectangle, area [4]int, scale float64) (hasClear, black, white bool) {
	x0 := int(float64(area[0]) * scale)
	y0 := int(float64(area[1]) * scale)
	w := int(float64(area[2]) * scale)
	h := int(float64(area[3]) * scale)
	black, white = true, true
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			if x < 0 || y < 0 || x >= b.Dx() || y >= b.Dy() {
				return true, false, false
			}
			r, g, bl, al := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if al != 0xffff {
				hasClear = true
			}
			if !(r == 0 && g == 0 && bl == 0 && al == 0xffff) {
				black = false
			}
			if !(r == 0xffff && g == 0xffff && bl == 0xffff && al == 0xffff) {
				white = false
			}
		}
	}
	return hasClear, black, white
}

// --- held items ---

// handEquippedRe matches the tool and weapon suffixes held upright by the game.
var handEquippedRe = regexp.MustCompile(`_(sword|axe|pickaxe|shovel|hoe|spear)$`)

// handEquippedNames are the non-suffixed items the game holds upright.
var handEquippedNames = map[string]bool{
	"stick": true, "bone": true, "blaze_rod": true, "breeze_rod": true,
	"fishing_rod": true, "carrot_on_a_stick": true, "warped_fungus_on_a_stick": true,
	"mace": true,
}

// isHandEquipped reports whether the game holds an item upright (a tool or
// weapon) rather than flat (food, materials).
func isHandEquipped(name string) bool {
	return handEquippedRe.MatchString(name) || handEquippedNames[name]
}

// heldFor resolves one hand's item.
func (a *App) heldFor(dir string, item string) (HeldTexture, error) {
	name := strings.TrimSpace(item)
	if name == "" {
		return HeldTexture{}, nil
	}
	if !itemNameRe.MatchString(name) {
		return HeldTexture{}, fmt.Errorf("invalid item name: %s", item)
	}
	return HeldTexture{
		Item: orPlaceholder(a.itemTexture(dir, name)).dataURI(),
		Flat: !isHandEquipped(name),
	}, nil
}

// --- endpoints ---

// GetPlayerTextures resolves every texture the player wears, validating
// everything that came from the frontend.
func (a *App) GetPlayerTextures(req PlayerRequest) (PlayerTextures, error) {
	dir, err := a.packDirFor(req.Base, req.Pack)
	if err != nil {
		return PlayerTextures{}, err
	}
	skin := a.skinFor(packSkinKey(req.Base, req.Pack), dir)
	if skin == nil {
		return PlayerTextures{}, fmt.Errorf("no skin texture")
	}

	model := strings.ToLower(strings.TrimSpace(req.Model))
	if model == "" {
		model = "auto"
	}
	switch model {
	case "auto", "wide", "slim":
	default:
		return PlayerTextures{}, fmt.Errorf("unknown model: %s", req.Model)
	}

	out := PlayerTextures{
		Skin: skin.dataURI(),
		Slim: model == "slim" || (model == "auto" && isSlimSkin(skin.img)),
	}

	material := strings.ToLower(strings.TrimSpace(req.Material))
	if material == "none" {
		material = ""
	}
	if material != "" {
		if !vanillaArmorMaterials[material] {
			return PlayerTextures{}, fmt.Errorf("unknown armor material: %s", req.Material)
		}
		out.Layer1 = orPlaceholder(a.armorLayer(dir, material, 1)).dataURI()
		out.Layer2 = orPlaceholder(a.armorLayer(dir, material, 2)).dataURI()
	}
	if req.Elytra {
		out.Elytra = orPlaceholder(a.elytraTexture(dir)).dataURI()
	}
	if out.Right, err = a.heldFor(dir, req.Right); err != nil {
		return PlayerTextures{}, err
	}
	if out.Left, err = a.heldFor(dir, req.Left); err != nil {
		return PlayerTextures{}, err
	}
	return out, nil
}

// GetItemTexture returns one item's sprite as a PNG data URI: the pack's,
// else vanilla's, else the missing-texture marker. base is the pack's folder,
// as in PlayerRequest.
func (a *App) GetItemTexture(pack string, base string, item string) (string, error) {
	dir, err := a.packDirFor(base, pack)
	if err != nil {
		return "", err
	}
	return orPlaceholder(a.itemTexture(dir, strings.TrimSpace(item))).dataURI(), nil
}

// SaveRender writes a rendered data URI to a file the user picks. A cancelled
// dialog is not an error.
func (a *App) SaveRender(dataURI string, suggestedName string) error {
	mime, data, err := splitDataURI(dataURI)
	if err != nil {
		return err
	}
	filter := wailsRuntime.FileFilter{DisplayName: "PNG image", Pattern: "*.png"}
	ext := ".png"
	if strings.Contains(mime, "gif") {
		filter = wailsRuntime.FileFilter{DisplayName: "GIF image", Pattern: "*.gif"}
		ext = ".gif"
	}
	name := filepath.Base(strings.TrimSpace(suggestedName))
	if name == "" || name == "." || name == string(os.PathSeparator) {
		name = "render" + ext
	} else if filepath.Ext(name) == "" {
		name += ext
	}
	path, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		Title:           "Save render",
		DefaultFilename: name,
		Filters:         []wailsRuntime.FileFilter{filter},
	})
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	return os.WriteFile(path, data, 0644)
}

// --- helpers ---

// pngDataURI encodes PNG bytes as a data URI.
func pngDataURI(png []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// splitDataURI separates a base64 data URI's mime type from its bytes.
func splitDataURI(uri string) (string, []byte, error) {
	if !strings.HasPrefix(uri, "data:") {
		return "", nil, fmt.Errorf("not a data URI")
	}
	comma := strings.IndexByte(uri, ',')
	if comma < 0 {
		return "", nil, fmt.Errorf("malformed data URI")
	}
	meta := uri[len("data:"):comma]
	if !strings.HasSuffix(meta, ";base64") {
		return "", nil, fmt.Errorf("data URI is not base64")
	}
	data, err := base64.StdEncoding.DecodeString(uri[comma+1:])
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSuffix(meta, ";base64"), data, nil
}
