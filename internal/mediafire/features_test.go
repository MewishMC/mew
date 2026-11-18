package mediafire

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsMediaFireURL(t *testing.T) {
	cases := map[string]bool{
		"https://www.mediafire.com/file/abc/pack.zip/file": true,
		"http://MEDIAFIRE.com/file/abc":                    true,
		"https://example.com/mediafire.com":                true,
		"https://example.com/pack.zip":                     false,
	}
	for u, want := range cases {
		if got := IsMediaFireURL(u); got != want {
			t.Errorf("IsMediaFireURL(%q) = %v, want %v", u, got, want)
		}
	}
	if !IsMediaFireFolderURL("https://www.mediafire.com/folder/abc/packs") {
		t.Error("a folder URL was not recognised")
	}
	if IsMediaFireFolderURL("https://www.mediafire.com/file/abc/x.zip/file") {
		t.Error("a file URL was treated as a folder")
	}
}

// TestDownloadFromURLStreamsFile: the plain-download path used by the porter and
// the folder downloader.
func TestDownloadFromURLStreamsFile(t *testing.T) {
	payload := []byte("PK\x03\x04 pretend this is a pack")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="Cool Pack.zip"`)
		w.Header().Set("Content-Type", "application/zip")
		w.Write(payload)
	}))
	defer srv.Close()

	data, name, err := DownloadFromURL(srv.URL + "/dl")
	if err != nil {
		t.Fatalf("DownloadFromURL: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("downloaded %d bytes, want %d", len(data), len(payload))
	}
	if name != "Cool Pack.zip" {
		t.Errorf("filename = %q, want the Content-Disposition name", name)
	}
}

// TestDownloadFromURLSanitizesServerName: the server cannot choose a path.
func TestDownloadFromURLSanitizesServerName(t *testing.T) {
	for _, cd := range []string{
		`attachment; filename="..\..\evil.zip"`,
		`attachment; filename="/etc/passwd"`,
		`attachment; filename="%2e%2e%2f%2e%2e%2fevil.zip"`,
		`attachment; filename="C:\Windows\evil.zip"`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Disposition", cd)
			w.Write([]byte("PK"))
		}))
		_, name, err := DownloadFromURL(srv.URL + "/dl")
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", cd, err)
		}
		if strings.ContainsAny(name, `/\:`) || strings.Contains(name, "..") {
			t.Errorf("%s produced unsafe filename %q", cd, name)
		}
		if name == "" {
			t.Errorf("%s produced an empty filename", cd)
		}
	}
}

func TestDownloadFromURLFallsBackToURLName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("PK"))
	}))
	defer srv.Close()

	_, name, err := DownloadFromURL(srv.URL + "/files/My%20Pack.zip")
	if err != nil {
		t.Fatal(err)
	}
	if name != "My Pack.zip" {
		t.Errorf("filename = %q, want the decoded URL name", name)
	}
}

func TestDownloadFromURLRejectsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, _, err := DownloadFromURL(srv.URL); err == nil {
		t.Fatal("a 404 should be an error")
	}
	if _, _, err := DownloadFromURL("http://127.0.0.1:1/nothing"); err == nil {
		t.Fatal("an unreachable server should be an error")
	}
}

// TestReadAllLimited enforces the memory cap without streaming half a gigabyte.
func TestReadAllLimited(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 100)
	got, err := readAllLimited(bytes.NewReader(data), 100)
	if err != nil || len(got) != 100 {
		t.Fatalf("exactly the limit: %d bytes, %v", len(got), err)
	}
	if _, err := readAllLimited(bytes.NewReader(data), 99); err == nil {
		t.Fatal("data over the limit should be refused")
	} else if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestDownloadDirectRejectsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html>not a file</html>"))
	}))
	defer srv.Close()

	_, _, err := DownloadDirect(CreateHTTPClient(), srv.URL+"/dl")
	if err == nil || !strings.Contains(err.Error(), "HTML") {
		t.Fatalf("expected an HTML error, got %v", err)
	}
}

// TestExtractMediaFireFolderLinksPaginates walks the API, following
// more_chunks, and escapes the folder key into the query.
func TestExtractMediaFireFolderLinksPaginates(t *testing.T) {
	oldBase := mediaFireAPIBase
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/folder/get_content.php") {
			http.NotFound(w, r)
			return
		}
		// The pasted key must not be able to rewrite the query: it arrives as
		// one escaped value and the other parameters stay ours.
		if got := r.URL.Query().Get("folder_key"); got != "abc&content_type=nope" {
			t.Errorf("folder_key = %q", got)
		}
		if got := r.URL.Query().Get("content_type"); got != "files" {
			t.Errorf("content_type was overridden: %q", got)
		}
		chunk := r.URL.Query().Get("chunk")
		var files []string
		if chunk == "1" {
			for i := 0; i < 100; i++ {
				files = append(files, fmt.Sprintf(`{"filename":"Pack+%03d.zip","links":{"normal_download":"https://download.example/%03d"}}`, i, i))
			}
			fmt.Fprintf(w, `{"response":{"result":"Success","folder_content":{"more_chunks":"yes","files":[%s]}}}`, strings.Join(files, ","))
			return
		}
		// Not a full page, so the loop must stop even without more_chunks=no.
		fmt.Fprint(w, `{"response":{"result":"Success","folder_content":{"more_chunks":"yes","files":[
			{"filename":"Last One.zip","links":{"normal_download":"https://download.example/last"}},
			{"filename":"No Link.zip","links":{}}
		]}}}`)
	}))
	defer srv.Close()
	mediaFireAPIBase = srv.URL
	t.Cleanup(func() { mediaFireAPIBase = oldBase })

	files, err := ExtractMediaFireFolderLinks(CreateHTTPClient(), "https://www.mediafire.com/folder/abc&content_type=nope/packs")
	if err != nil {
		t.Fatalf("ExtractMediaFireFolderLinks: %v", err)
	}
	if len(files) != 101 {
		t.Fatalf("got %d files, want 101 (files with no link skipped)", len(files))
	}
	if files[0].Name != "Pack 000.zip" {
		t.Errorf("first name = %q, want the + decoded to a space", files[0].Name)
	}
	if files[0].URL != "https://download.example/000" {
		t.Errorf("first URL = %q", files[0].URL)
	}
	if files[100].Name != "Last One.zip" {
		t.Errorf("last name = %q", files[100].Name)
	}
}

func TestExtractMediaFireFolderLinksErrors(t *testing.T) {
	if _, err := ExtractMediaFireFolderLinks(CreateHTTPClient(), "https://www.mediafire.com/file/x/y.zip/file"); err == nil {
		t.Error("a file URL has no folder key and should fail")
	}

	oldBase := mediaFireAPIBase
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"response":{"result":"Error","message":"Folder not found"}}`)
	}))
	defer srv.Close()
	mediaFireAPIBase = srv.URL
	t.Cleanup(func() { mediaFireAPIBase = oldBase })

	_, err := ExtractMediaFireFolderLinks(CreateHTTPClient(), "https://www.mediafire.com/folder/missing")
	if err == nil || !strings.Contains(err.Error(), "Folder not found") {
		t.Fatalf("expected the API message, got %v", err)
	}
}

func TestExtractMediaFireDownloadURL(t *testing.T) {
	// A page with the download button.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><a id="downloadButton" class="btn" href="//download1234.mediafire.com/abc/pack.zip">Download</a></html>`)
	}))
	defer srv.Close()

	got, err := ExtractMediaFireDownloadURL(CreateHTTPClient(), srv.URL+"/file/abc/pack.zip/file")
	if err != nil {
		t.Fatalf("ExtractMediaFireDownloadURL: %v", err)
	}
	if got != "https://download1234.mediafire.com/abc/pack.zip" {
		t.Errorf("download URL = %q", got)
	}

	// A redirect straight to the file.
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://download9999.mediafire.com/direct/pack.zip", http.StatusFound)
	}))
	defer redirect.Close()
	got, err = ExtractMediaFireDownloadURL(CreateHTTPClient(), redirect.URL+"/file/xyz/pack.zip/file")
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	if !strings.Contains(got, "download9999.mediafire.com") {
		t.Errorf("redirect URL = %q", got)
	}

	// A page with no link at all.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><p>Nothing here</p></body></html>`)
	}))
	defer empty.Close()
	if _, err := ExtractMediaFireDownloadURL(CreateHTTPClient(), empty.URL+"/file/none/x.zip/file"); err == nil {
		t.Error("a page without a link should fail")
	}
}

func TestExtractFilenameFallbacks(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	if got := extractFilename(resp, "https://download.example/path/My%20Pack.zip"); got != "My Pack.zip" {
		t.Errorf("filename = %q", got)
	}
	if got := extractFilename(resp, "https://download.example/"); got != "download.zip" {
		t.Errorf("filename without a name = %q, want download.zip", got)
	}
	resp.Header.Set("Content-Disposition", `attachment; filename="../../x.zip"`)
	if got := extractFilename(resp, "https://download.example/a.zip"); got != "x.zip" {
		t.Errorf("traversing name = %q", got)
	}
	// A URL that is only a host must not become the file name.
	if got := extractFilename(&http.Response{Header: http.Header{}}, "https://dl.example.com/"); got != "download.zip" {
		t.Errorf("host-only URL name = %q", got)
	}
	// RFC 5987 encoded names are decoded instead of keeping the prefix.
	resp.Header.Set("Content-Disposition", `attachment; filename=pack.zip; filename*=UTF-8''na%C3%AFve%20pack.zip`)
	if got := extractFilename(resp, "https://download.example/x"); got != "naïve pack.zip" {
		t.Errorf("RFC 5987 name = %q, want %q", got, "naïve pack.zip")
	}
}
