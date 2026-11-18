package main

import (
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
