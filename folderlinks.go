package main

import (
	"net/http"
	"strings"

	"github.com/kyarottoOwO/mew/internal/gdrive"
	"github.com/kyarottoOwO/mew/internal/mediafire"
	"github.com/kyarottoOwO/mew/internal/mega"
	"github.com/kyarottoOwO/mew/internal/modrinth"
)

// folderFile is one downloadable pack found in a pasted folder link. Fetch is
// set for hosts whose files cannot be fetched with a plain GET (MEGA encrypts
// them with the key carried in the link).
type folderFile struct {
	Name  string
	URL   string
	Fetch func(*http.Client) ([]byte, string, error)
}

// download fetches the pack, however its host serves it.
func (f folderFile) download(client *http.Client) ([]byte, string, error) {
	if f.Fetch != nil {
		return f.Fetch(client)
	}
	return mediafire.DownloadDirect(client, f.URL)
}

// isFolderLink reports whether a pasted link points at a folder rather than a
// single file, for the hosts that can be listed.
func isFolderLink(rawURL string) bool {
	return gdrive.IsDriveFolderURL(rawURL) || mega.IsMegaFolderURL(rawURL) || mediafire.IsMediaFireFolderURL(rawURL)
}

// downloadLink fetches the single file behind a pasted link, whatever host it is
// on.
func downloadLink(rawURL string) ([]byte, string, error) {
	if mega.IsMegaURL(rawURL) {
		return mega.DownloadPublicFile(mediafire.CreateHTTPClient(), rawURL)
	}
	if typ, ok := modrinth.UnsupportedType(rawURL); ok {
		return nil, "", modrinth.UnportableError(typ)
	}
	if modrinth.IsModrinthURL(rawURL) {
		return modrinth.DownloadProject(mediafire.CreateHTTPClient(), rawURL)
	}
	return mediafire.DownloadFromURL(gdrive.ResolveDownloadURL(rawURL))
}

// folderFileLinks lists the packs inside a folder link, whichever host it is on.
// Google Drive folders are walked through their subfolders, since a Drive folder
// is usually a shelf of subfolders; MediaFire folders are read through its API;
// MEGA hands over the whole tree in one answer. Only archives are returned,
// because a folder can hold a pack's loose files next to its zips.
func folderFileLinks(client *http.Client, rawURL string) ([]folderFile, error) {
	switch {
	case gdrive.IsDriveFolderURL(rawURL):
		entries, err := gdrive.ExtractFolderFiles(client, rawURL)
		if err != nil {
			return nil, err
		}
		out := make([]folderFile, 0, len(entries))
		for _, e := range entries {
			if !isArchive(strings.ToLower(e.Name)) {
				continue
			}
			out = append(out, folderFile{Name: e.Name, URL: e.URL})
		}
		return out, nil

	case mega.IsMegaFolderURL(rawURL):
		entries, err := mega.ExtractFolderFiles(client, rawURL)
		if err != nil {
			return nil, err
		}
		out := make([]folderFile, 0, len(entries))
		for _, e := range entries {
			if !isArchive(strings.ToLower(e.Name)) {
				continue
			}
			entry := e
			out = append(out, folderFile{
				Name: e.Name,
				Fetch: func(c *http.Client) ([]byte, string, error) {
					return entry.Download(c)
				},
			})
		}
		return out, nil
	}

	entries, err := mediafire.ExtractMediaFireFolderLinks(client, rawURL)
	if err != nil {
		return nil, err
	}
	out := make([]folderFile, 0, len(entries))
	for _, e := range entries {
		out = append(out, folderFile{Name: e.Name, URL: e.URL})
	}
	return out, nil
}
