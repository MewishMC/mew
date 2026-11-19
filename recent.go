package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type RecentPack struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Timestamp string `json:"timestamp"`
}

func (a *App) getRecentPacksPath() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	dir := filepath.Join(localAppData, "mew", "Recent_Packs")
	os.MkdirAll(dir, os.ModePerm)
	newPath := filepath.Join(dir, "recent_packs.json")

	exePath, err := os.Executable()
	if err == nil {
		oldPath := filepath.Join(filepath.Dir(exePath), "recent_packs.json")
		if _, statErr := os.Stat(oldPath); statErr == nil {
			if _, statErr := os.Stat(newPath); os.IsNotExist(statErr) {
				a.logDebug(fmt.Sprintf("Migrating recent_packs from %s to %s", oldPath, newPath))
				data, readErr := os.ReadFile(oldPath)
				if readErr == nil {
					os.WriteFile(newPath, data, 0644)
				}
				os.Remove(oldPath)
			}
		}
	}

	return newPath
}

func (a *App) GetRecentPacks() []RecentPack {
	return a.loadRecentPacks(5)
}

// GetAllRecentPacks returns the whole history, not just the newest few.
func (a *App) GetAllRecentPacks() []RecentPack {
	return a.loadRecentPacks(0)
}

func (a *App) loadRecentPacks(max int) []RecentPack {
	data, err := os.ReadFile(a.getRecentPacksPath())
	if err != nil {
		return []RecentPack{}
	}

	var packs []RecentPack
	if err := json.Unmarshal(data, &packs); err != nil {
		return []RecentPack{}
	}

	if max > 0 && len(packs) > max {
		packs = packs[:max]
	}
	return packs
}

func (a *App) AddRecentPack(name, path string) {
	data, err := os.ReadFile(a.getRecentPacksPath())
	var packs []RecentPack
	if err == nil {
		json.Unmarshal(data, &packs)
	}

	newPack := RecentPack{
		Name:      name,
		Path:      path,
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
	}

	packs = append([]RecentPack{newPack}, packs...)

	jsonData, err := json.MarshalIndent(packs, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(a.getRecentPacksPath(), jsonData, 0644)
	a.emit("recent-packs-changed")
}
