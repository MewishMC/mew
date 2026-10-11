package main

import (
	"os"
)

// GetPackThumbnailTextures returns what each pack's card shows: the pack's
// player skin (or the user's default skin, or MEW's) in the pack's diamond
// armor holding the pack's diamond sword, both falling back to vanilla. Every
// installed pack gets one, so a pack that only retextures armor or swords
// still shows it. The frontend draws them. Keyed by pack directory name. base
// is the packs' folder, as in PlayerRequest.
func (a *App) GetPackThumbnailTextures(packNames []string, base string) map[string]PlayerTextures {
	out := make(map[string]PlayerTextures)
	for _, name := range packNames {
		dir, err := a.packDirFor(base, name)
		if err != nil || dir == "" {
			continue
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		tex, err := a.GetPlayerTextures(PlayerRequest{
			Pack:     name,
			Base:     base,
			Material: "diamond",
			Right:    "diamond_sword",
		})
		if err != nil {
			debugLogf("skin thumbnail for %s: %v", name, err)
			continue
		}
		out[name] = tex
	}
	return out
}
