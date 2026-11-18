package mediafire

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

func respWithDisposition(cd string) *http.Response {
	h := http.Header{}
	if cd != "" {
		h.Set("Content-Disposition", cd)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     h,
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}
}

// TestExtractFilenameSanitizes: the name is later joined onto a folder, so a
// header-supplied path must be reduced to a base name (SEC-1).
func TestExtractFilenameSanitizes(t *testing.T) {
	cases := []struct {
		name   string
		header string
		url    string
		want   string
	}{
		{"plain header", `attachment; filename="cool_pack.zip"`, "https://x/y", "cool_pack.zip"},
		{"traversal header", `attachment; filename="..\..\Startup\evil.zip"`, "https://x/y", "evil.zip"},
		{"forward traversal", `attachment; filename="../../evil.zip"`, "https://x/y", "evil.zip"},
		{"absolute header", `attachment; filename="C:\Windows\evil.zip"`, "https://x/y", "evil.zip"},
		{"header wins over url", `attachment; filename="real.zip"`, "https://x/path/other.zip", "real.zip"},
		{"from url", "", "https://x/path/fromurl.zip", "fromurl.zip"},
		{"traversal url", "", "https://x/path/..%2F..%2Fevil.zip", "evil.zip"},
		{"nothing usable", "", "https://x/path", "download.zip"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractFilename(respWithDisposition(c.header), c.url); got != c.want {
				t.Errorf("extractFilename(%q, %q) = %q, want %q", c.header, c.url, got, c.want)
			}
		})
	}
}

// TestSanitizeFilenameRejectsSeparators: the result may never contain a path
// separator, whatever was supplied.
func TestSanitizeFilenameRejectsSeparators(t *testing.T) {
	for _, in := range []string{"a/b.zip", `a\b.zip`, "..", ".", "  .zip  "} {
		got := sanitizeFilename(in)
		if got == ".." || got == "." || got == "" {
			continue
		}
		if bytes.ContainsAny([]byte(got), `/\`) {
			t.Errorf("sanitizeFilename(%q) = %q, still contains a separator", in, got)
		}
	}
}
