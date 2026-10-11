package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetPackSkinThumbnails(t *testing.T) {
	skin, err := os.ReadFile("frontend/src/assets/default-skin.png")
	if err != nil {
		t.Fatal(err)
	}
	a := testApp(t)
	root := a.getResourcePacksPath()
	writeFile(t, filepath.Join(root, "withSkin", "textures", "entity", "steve.png"), skin)
	if err := os.MkdirAll(filepath.Join(root, "noSkin"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := a.GetPackThumbnailTextures([]string{"withSkin", "noSkin", "../withSkin", ""}, "")

	if len(got) != 2 {
		t.Fatalf("got thumbnails for %d packs, want 2: %v", len(got), keys(got))
	}
	for _, name := range []string{"withSkin", "noSkin"} {
		if tex := got[name]; !strings.HasPrefix(tex.Skin, "data:image/png;base64,") || tex.Layer1 == "" || tex.Right.Item == "" {
			t.Fatalf("%s thumbnail is missing its skin, armor or sword", name)
		}
	}
}

func keys(m map[string]PlayerTextures) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
