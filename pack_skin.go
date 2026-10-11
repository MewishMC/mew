package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// A skin picked with "Change Skin" is first a preview: it shows in the Pack
// Viewer while it is open and is dropped when the viewer closes. Saving it
// writes it for that pack alone, where it survives restarts.

// previewSkin is a skin being previewed for one pack.
type previewSkin struct {
	tex *texture
	gen int
}

// packSkinKey names a pack's chosen skin: its folder name for an installed
// pack, prefixed for one in the server pack cache so the two never share a
// skin. It is "" for a name that is not a plain folder name.
func packSkinKey(base, packName string) string {
	if packName == "" || filepath.Base(packName) != packName || packName == "." || packName == ".." {
		return ""
	}
	if base != "" {
		return "server-cache_" + packName
	}
	return packName
}

// chosenSkinPath is where the skin saved for one pack is kept, or "" for an
// empty key.
func chosenSkinPath(key string) string {
	if key == "" {
		return ""
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "mew", "pack_skins", key+".png")
}

// PreviewPackSkin shows a skin on one pack's player until the viewer closes
// or the skin is saved. The data URI must decode to a skin-sized image.
// base is the pack's folder, as in PlayerRequest.
func (a *App) PreviewPackSkin(packName string, base string, dataURI string) error {
	key := packSkinKey(base, packName)
	if key == "" {
		return fmt.Errorf("invalid pack name")
	}
	_, data, err := splitDataURI(dataURI)
	if err != nil {
		return err
	}
	tex, err := newTexture(data)
	if err != nil {
		return fmt.Errorf("not a skin image: %w", err)
	}
	if b := tex.img.Bounds(); b.Dx() < 64 || b.Dy() < 32 {
		return fmt.Errorf("skin is %dx%d, smaller than 64x32", b.Dx(), b.Dy())
	}
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	if a.previewSkins == nil {
		a.previewSkins = map[string]previewSkin{}
	}
	a.previewGen++
	a.previewSkins[key] = previewSkin{tex: tex, gen: a.previewGen}
	return nil
}

// ClearPreviewSkins drops every previewed skin, so each pack shows its saved
// or own skin again. The viewer calls it when it closes.
func (a *App) ClearPreviewSkins() {
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	a.previewSkins = nil
}

// SavePackSkin keeps the skin being previewed for one pack, so it shows every
// time that pack is opened, and on its card.
func (a *App) SavePackSkin(packName string, base string) error {
	key := packSkinKey(base, packName)
	p := chosenSkinPath(key)
	if p == "" {
		return fmt.Errorf("invalid pack name")
	}
	a.previewMu.Lock()
	prev, ok := a.previewSkins[key]
	a.previewMu.Unlock()
	if !ok {
		return fmt.Errorf("no skin to save for %s", packName)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	// Write then rename, so a render never reads a half-written skin.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, prev.tex.data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	a.previewMu.Lock()
	if cur, ok := a.previewSkins[key]; ok && cur.gen == prev.gen {
		delete(a.previewSkins, key)
	}
	a.previewMu.Unlock()
	return nil
}

// ClearPackSkin forgets the skin previewed and the one saved for one pack, so
// it shows its own skin (or the default) again.
func (a *App) ClearPackSkin(packName string, base string) error {
	key := packSkinKey(base, packName)
	p := chosenSkinPath(key)
	if p == "" {
		return fmt.Errorf("invalid pack name")
	}
	a.previewMu.Lock()
	delete(a.previewSkins, key)
	a.previewMu.Unlock()
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// HasPackSkin reports whether a skin is saved for this pack.
func (a *App) HasPackSkin(packName string, base string) bool {
	p := chosenSkinPath(packSkinKey(base, packName))
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// previewSkin returns the skin being previewed under a pack's skin key, or
// nil.
func (a *App) previewSkin(key string) *texture {
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	return a.previewSkins[key].tex
}
