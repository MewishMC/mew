package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSafeArchiveName: a name that came from a download must never be able to
// choose the folder it is written into (SEC-1).
func TestSafeArchiveName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"pack.zip", "pack.zip"},
		{"My Pack (1).zip", "My Pack (1).zip"},
		{"../../escape.zip", "escape.zip"},
		{`..\..\escape.zip`, "escape.zip"},
		{`C:\Windows\System32\evil.zip`, "evil.zip"},
		{"/etc/cron.d/evil.zip", "evil.zip"},
		{"a/b/c/pack.zip", "pack.zip"},
		{".zip", "zip"},
		{"", "pack.zip"},
		{"   ", "pack.zip"},
		{"..", "pack.zip"},
		{"bad\x00name.zip", "badname.zip"},
	}
	for _, c := range cases {
		if got := safeArchiveName(c.in); got != c.want {
			t.Errorf("safeArchiveName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSafeArchiveNameCannotEscape is the property that matters: joining the
// result to a folder always lands inside that folder.
func TestSafeArchiveNameCannotEscape(t *testing.T) {
	base := filepath.Join(t.TempDir(), "out")
	for _, in := range []string{"../../x.zip", `..\..\x.zip`, `C:\x.zip`, "a/../../x.zip"} {
		joined := filepath.Join(base, safeArchiveName(in))
		if filepath.Dir(joined) != base {
			t.Errorf("safeArchiveName(%q) joined to %q gives %q, outside", in, base, joined)
		}
	}
}

func testZipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestWriteMcpackIgnoresTraversalName: buildMcpackBytes/writeMcpack must write
// inside the output folder even when the incoming file name tries to leave it.
func TestWriteMcpackIgnoresTraversalName(t *testing.T) {
	src := testZipBytes(t, map[string]string{"manifest.json": `{"header":{"name":"x"}}`})

	outDir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	a := NewApp(false)
	written, err := a.writeMcpack(src, `..\..\escaped.zip`, outDir, "")
	if err != nil {
		t.Fatalf("writeMcpack: %v", err)
	}
	if filepath.Dir(written) != outDir {
		t.Fatalf("writeMcpack wrote %q, outside %q", written, outDir)
	}
	if !strings.HasSuffix(written, ".mcpack") {
		t.Fatalf("writeMcpack wrote %q, want a .mcpack file", written)
	}
	if _, err := os.Stat(filepath.Join(outDir, "escaped.mcpack")); err != nil {
		t.Fatalf("expected escaped.mcpack inside the output dir: %v", err)
	}
}

// TestUnzipRejectsTraversal: entries that would land outside the destination
// are refused instead of being extracted.
func TestUnzipRejectsTraversal(t *testing.T) {
	src := testZipBytes(t, map[string]string{"../evil.txt": "pwned"})

	root := t.TempDir()
	archive := filepath.Join(root, "pack.zip")
	if err := os.WriteFile(archive, src, 0644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(root, "extracted")
	if err := unzip(archive, dest); err == nil {
		t.Fatal("unzip accepted a zip-slip entry")
	}
	if _, err := os.Stat(filepath.Join(root, "evil.txt")); err == nil {
		t.Fatal("zip-slip entry was written outside the destination")
	}
}
