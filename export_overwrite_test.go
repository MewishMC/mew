package main

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests cover the two ways an export failed with
// "failed to create mcpack: open image (1).mcpack: Access is denied":
// a target file that is read-only, and an output folder that cannot be written.

func makeReadOnly(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("previous export"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	// Let TempDir clean up even when the assertion below fails first.
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
}

func TestWriteOutputFileOverwritesReadOnlyTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image (1).mcpack")
	makeReadOnly(t, path)

	if err := writeOutputFile(path, []byte("new export")); err != nil {
		t.Fatalf("writeOutputFile over a read-only target: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new export" {
		t.Fatalf("content = %q, want the new export", got)
	}
}

func TestCreateZipFromFolderOverwritesReadOnlyTarget(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "image (1).mcpack")
	makeReadOnly(t, target)

	if err := createZipFromFolder(src, target); err != nil {
		t.Fatalf("createZipFromFolder over a read-only target: %v", err)
	}
	if size := len(zipEntries(t, target)); size == 0 {
		t.Fatal("no entries written")
	}
}

// TestGetOutputDirFallsBackForUnusableConfiguredDir: a configured output dir
// that cannot be created is replaced by a writable one, which is what makes the
// sky/animator/porter exports survive an install under Program Files.
func TestGetOutputDirFallsBackForUnusableConfiguredDir(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	// A regular file where a directory is expected: creating it as a dir fails.
	configured := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(configured, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewApp(false)
	a.settings["customOutputDir"] = configured

	got := a.getOutputDir()
	if samePath(got, configured) {
		t.Skip("no writable fallback folder on this machine")
	}
	if !dirWritable(got) {
		t.Fatalf("getOutputDir returned %q, which is not writable", got)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), "Downloads") {
		t.Logf("fallback folder is %q", got)
	}
}

// TestSkyPackOverwritesReadOnlyMcpack reproduces the reported failure at its
// real entry point: an "image (1).png" sky exported into a folder that already
// holds a read-only "image (1).mcpack". Before the fix this returned
// "failed to create mcpack: open image (1).mcpack: Access is denied".
func TestSkyPackOverwritesReadOnlyMcpack(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	outDir := t.TempDir()
	target := filepath.Join(outDir, "image (1).mcpack")
	makeReadOnly(t, target)

	// Precondition: this filesystem really does refuse to write that file, so a
	// pass below means the export cleared it rather than never hitting the case.
	f, openErr := os.OpenFile(target, os.O_WRONLY, 0)
	if openErr == nil {
		f.Close()
		t.Skip("filesystem does not enforce the read-only attribute")
	}
	t.Logf("pre-fix precondition, writing the target fails: %v", openErr)

	src := encodeTestPNG(t, color.RGBA{R: 10, G: 200, B: 40, A: 255})
	got, err := buildSkyPack(src, "image (1).png", 128, outDir, "sky", "", nil)
	if err != nil {
		t.Fatalf("buildSkyPack over a read-only target: %v", err)
	}
	if !samePath(got, target) {
		t.Fatalf("buildSkyPack wrote %q, want %q", got, target)
	}
	entries := zipEntries(t, target)
	for _, want := range []string{"manifest.json", "pack_icon.png"} {
		if !entries[want] {
			t.Errorf("written pack is missing %s (has %v)", want, entries)
		}
	}
}

// TestGetOutputDirRecomputesAfterSaveSettings: fixing the folder and saving the
// settings must not keep serving the stale fallback.
func TestGetOutputDirRecomputesAfterSaveSettings(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	a := NewApp(false)
	writable := t.TempDir()

	a.SaveSettings(map[string]interface{}{"customOutputDir": writable})
	if got := a.getOutputDir(); !samePath(got, writable) {
		t.Fatalf("getOutputDir = %q, want %q", got, writable)
	}

	// Same value saved again: the memoised entry must be dropped and rebuilt.
	a.SaveSettings(map[string]interface{}{"customOutputDir": writable})
	if got := a.getOutputDir(); !samePath(got, writable) {
		t.Fatalf("getOutputDir after re-save = %q, want %q", got, writable)
	}
}
