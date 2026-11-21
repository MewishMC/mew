// Package mega reads public MEGA (mega.nz) share links. MEGA stores every file
// encrypted and puts the decryption key in the link fragment, so a public link
// needs no account: the key is unwrapped here and the bytes are decrypted as
// they arrive. A link that needs a password or an account is reported as such
// instead of sending the user to a login page.
package mega

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	apiEndpoint = "https://g.api.mega.co.nz/cs"
	userAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	// Same ceiling the other hosts use, so one link cannot stream the app to
	// death.
	maxDownloadBytes = 512 << 20
	maxFolderFiles   = 2000
	maxFolderDepth   = 5
	maxAPIBytes      = 32 << 20
)

// ErrNoKey is returned for a link whose fragment was lost. Without the fragment
// MEGA's own web page cannot open the link either, so it is always a copy
// mistake.
var ErrNoKey = errors.New("this MEGA link has no key in it; copy the whole share link, including the part after #")

// link is a parsed share link: the node handle plus the key from the fragment.
type link struct {
	handle   string
	key      []byte
	isFolder bool
}

func isMegaHost(host string) bool {
	host = strings.ToLower(host)
	return strings.HasSuffix(host, "mega.nz") || strings.HasSuffix(host, "mega.co.nz") || strings.HasSuffix(host, "mega.io")
}

func IsMegaURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return isMegaHost(u.Host)
}

func IsMegaFolderURL(raw string) bool {
	l, err := parseLink(raw)
	return err == nil && l.isFolder
}

func parseLink(raw string) (link, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !isMegaHost(u.Host) {
		return link{}, fmt.Errorf("that is not a MEGA link")
	}

	// Legacy links (mega.nz/#!file!key, mega.nz/#F!folder!key) keep the handle in
	// the fragment; current ones put it in the path.
	frag := u.Fragment
	if strings.HasPrefix(frag, "F!") {
		parts := strings.SplitN(frag[2:], "!", 2)
		if len(parts) < 2 {
			return link{}, ErrNoKey
		}
		key, err := decodeBase64(parts[1])
		if err != nil || len(key) < 16 {
			return link{}, ErrNoKey
		}
		return link{handle: parts[0], key: key, isFolder: true}, nil
	}
	if strings.HasPrefix(frag, "!") {
		parts := strings.SplitN(frag[1:], "!", 2)
		if len(parts) < 2 {
			return link{}, ErrNoKey
		}
		key, err := decodeBase64(parts[1])
		if err != nil || len(key) < 16 {
			return link{}, ErrNoKey
		}
		return link{handle: parts[0], key: key}, nil
	}

	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) < 2 || (segments[0] != "file" && segments[0] != "folder") {
		return link{}, fmt.Errorf("could not find a file or folder id in that MEGA link")
	}
	if frag == "" {
		return link{}, ErrNoKey
	}
	key, err := decodeBase64(frag)
	if err != nil || len(key) < 16 {
		return link{}, ErrNoKey
	}
	// /folder/<id>#<key>/file/<id> is treated as its folder: the key in the
	// fragment belongs to the folder, so the file is found by listing it.
	return link{handle: segments[1], key: key, isFolder: segments[0] == "folder"}, nil
}

// decodeBase64 accepts the URL-safe, unpadded alphabet MEGA uses in links and
// node keys, and the standard one it uses in attributes.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty key")
	}
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return base64.StdEncoding.DecodeString(s)
}

// fileKey unwraps the 32-byte key a file link carries: the AES key is its two
// halves XORed together and the counter starts at the upper half's first 8
// bytes.
func fileKey(obscured []byte) (key, iv []byte, err error) {
	if len(obscured) != 32 {
		return nil, nil, fmt.Errorf("this MEGA link has a malformed file key")
	}
	key = make([]byte, 16)
	for i := 0; i < 16; i++ {
		key[i] = obscured[i] ^ obscured[i+16]
	}
	iv = make([]byte, 16)
	copy(iv, obscured[16:24])
	return key, iv, nil
}

type mseAnswer struct {
	G  string `json:"g"`
	S  int64  `json:"s"`
	At string `json:"at"`
	F  []node `json:"f"`
}

type node struct {
	H string `json:"h"`
	P string `json:"p"`
	T int    `json:"t"`
	A string `json:"a"`
	K string `json:"k"`
	S int64  `json:"s"`
}

// callAPI runs one command anonymously and returns its answer. node is the
// folder handle a public folder link points at; MEGA needs it to resolve files
// that are not shared on their own.
func callAPI(client *http.Client, node string, command map[string]any) (*mseAnswer, error) {
	body, err := json.Marshal([]any{command})
	if err != nil {
		return nil, err
	}

	endpoint := apiEndpoint + "?id=0"
	if node != "" {
		endpoint += "&n=" + url.QueryEscape(node)
	}

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach MEGA: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read the MEGA answer: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MEGA returned HTTP %d", resp.StatusCode)
	}

	// A failure comes back as a bare number; a success as an array with one
	// answer object per command.
	var answers []json.RawMessage
	if err := json.Unmarshal(raw, &answers); err != nil || len(answers) == 0 {
		var code int
		if json.Unmarshal(raw, &code) == nil {
			return nil, apiError(code)
		}
		return nil, fmt.Errorf("could not read the MEGA answer")
	}

	var code int
	if err := json.Unmarshal(answers[0], &code); err == nil {
		return nil, apiError(code)
	}

	var answer mseAnswer
	if err := json.Unmarshal(answers[0], &answer); err != nil {
		return nil, fmt.Errorf("could not read the MEGA answer")
	}
	return &answer, nil
}

func apiError(code int) error {
	switch code {
	case -9:
		return fmt.Errorf("MEGA says that file no longer exists (deleted or blocked)")
	case -11:
		return fmt.Errorf("that MEGA link is password protected; remove the password and share it again")
	case -16:
		return fmt.Errorf("MEGA blocked access to that link")
	case -18:
		return fmt.Errorf("MEGA has that file unavailable right now; try again in a few minutes")
	case -17, -24:
		return fmt.Errorf("that MEGA link is over its transfer quota; try again later")
	}
	return fmt.Errorf("MEGA returned error %d", code)
}

type fileMeta struct {
	URL  string
	Size int64
	Name string
}

// fileInfo asks MEGA where a file can be downloaded from and what it is called.
// A file that is shared through a folder is addressed with "n" and needs the
// folder handle in the request; a file with its own link uses "p".
func fileInfo(client *http.Client, handle, folderHandle string, key []byte) (fileMeta, error) {
	command := map[string]any{"a": "g", "g": 1, "p": handle}
	if folderHandle != "" {
		command = map[string]any{"a": "g", "g": 1, "n": handle}
	}
	answer, err := callAPI(client, folderHandle, command)
	if err != nil {
		return fileMeta{}, err
	}
	if answer.G == "" {
		return fileMeta{}, fmt.Errorf("MEGA did not return a download location for that file")
	}

	meta := fileMeta{URL: answer.G, Size: answer.S}
	if answer.At != "" {
		meta.Name = attributesName(answer.At, key)
	}
	return meta, nil
}

// attributesName decrypts the node attributes MEGA ships alongside a file. They
// are AES-CBC with a zero IV and carry a "MEGA" marker in front of the JSON.
func attributesName(encoded string, key []byte) string {
	blob, err := decodeBase64(encoded)
	if err != nil || len(blob) == 0 || len(blob)%16 != 0 || len(key) < 16 {
		return ""
	}
	block, err := aes.NewCipher(key[:16])
	if err != nil {
		return ""
	}
	plain := make([]byte, len(blob))
	cipher.NewCBCDecrypter(block, make([]byte, 16)).CryptBlocks(plain, blob)

	text := strings.TrimPrefix(strings.TrimRight(string(plain), "\x00"), "MEGA")
	var attrs struct {
		N string `json:"n"`
	}
	if err := json.Unmarshal([]byte(text), &attrs); err != nil {
		return ""
	}
	return attrs.N
}

func fetchDecrypted(client *http.Client, downloadURL string, size int64, key, iv []byte) ([]byte, error) {
	if size > maxDownloadBytes {
		return nil, fmt.Errorf("file is larger than %d MB", maxDownloadBytes>>20)
	}
	if !strings.Contains(downloadURL, "://") {
		downloadURL = "https://" + downloadURL
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download from MEGA: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MEGA download failed with status %d", resp.StatusCode)
	}

	// One byte over the cap tells us it was too big rather than truncating it.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read the MEGA download: %v", err)
	}
	if int64(len(data)) > maxDownloadBytes {
		return nil, fmt.Errorf("file is larger than %d MB", maxDownloadBytes>>20)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to set up decryption: %v", err)
	}
	cipher.NewCTR(block, iv).XORKeyStream(data, data)
	return data, nil
}

// DownloadPublicFile downloads one file from a public MEGA file link.
func DownloadPublicFile(client *http.Client, rawURL string) ([]byte, string, error) {
	l, err := parseLink(rawURL)
	if err != nil {
		return nil, "", err
	}
	if l.isFolder {
		return nil, "", fmt.Errorf("that is a MEGA folder link, not a file")
	}

	key, iv, err := fileKey(l.key)
	if err != nil {
		return nil, "", err
	}
	info, err := fileInfo(client, l.handle, "", key)
	if err != nil {
		return nil, "", err
	}
	// The name travels with the download location, encrypted under the same key
	// as the file. Without it there is nothing honest to save the bytes as, so
	// stop before downloading instead of making up a name and an extension.
	if info.Name == "" {
		return nil, "", fmt.Errorf("MEGA did not report a name for that file")
	}
	data, err := fetchDecrypted(client, info.URL, info.Size, key, iv)
	if err != nil {
		return nil, "", err
	}
	return data, info.Name, nil
}

// Entry is one file found in a public MEGA folder, with the key material needed
// to download it.
type Entry struct {
	Name string
	Size int64

	handle string
	folder string
	key    []byte
	iv     []byte
}

// Download fetches and decrypts the entry, returning the bytes and their name.
func (e Entry) Download(client *http.Client) ([]byte, string, error) {
	info, err := fileInfo(client, e.handle, e.folder, e.key)
	if err != nil {
		return nil, "", err
	}
	data, err := fetchDecrypted(client, info.URL, info.Size, e.key, e.iv)
	if err != nil {
		return nil, "", err
	}
	name := info.Name
	if name == "" {
		name = e.Name
	}
	return data, name, nil
}

// nodeKey is the unwrapped key of one node: the AES key plus, for files, the
// counter start.
type nodeKey struct {
	key []byte
	iv  []byte
}

// ExtractFolderFiles lists every file in a public MEGA folder, subfolders
// included: MEGA hands over the whole tree in one answer.
func ExtractFolderFiles(client *http.Client, folderURL string) ([]Entry, error) {
	l, err := parseLink(folderURL)
	if err != nil {
		return nil, err
	}
	if !l.isFolder {
		return nil, fmt.Errorf("that is a MEGA file link, not a folder")
	}
	if len(l.key) < 16 {
		return nil, ErrNoKey
	}
	shareKey := l.key[:16]

	nodes, err := folderNodes(client, l.handle)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("that MEGA folder is empty")
	}

	keys := resolveKeys(nodes, shareKey)
	root := rootHandle(nodes)

	children := make(map[string][]node, len(nodes))
	for _, n := range nodes {
		children[n.P] = append(children[n.P], n)
	}

	var entries []Entry
	var walk func(handle string, depth int)
	walk = func(handle string, depth int) {
		if depth > maxFolderDepth || len(entries) >= maxFolderFiles {
			return
		}
		for _, n := range children[handle] {
			if n.T == 1 {
				walk(n.H, depth+1)
				continue
			}
			k, ok := keys[n.H]
			if !ok {
				continue
			}
			name := attributesName(n.A, k.key)
			if name == "" {
				name = n.H
			}
			entries = append(entries, Entry{Name: name, Size: n.S, handle: n.H, folder: l.handle, key: k.key, iv: k.iv})
			if len(entries) >= maxFolderFiles {
				return
			}
		}
	}
	walk(root, 0)

	if len(entries) == 0 {
		return nil, fmt.Errorf("no files found in the MEGA folder")
	}
	return entries, nil
}

func folderNodes(client *http.Client, handle string) ([]node, error) {
	answer, err := callAPI(client, handle, map[string]any{"a": "f", "c": 1, "r": 1, "ca": 1})
	if err != nil {
		return nil, err
	}
	return answer.F, nil
}

// rootHandle is the node the link points at: the folder whose parent is not part
// of the listing.
func rootHandle(nodes []node) string {
	present := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		present[n.H] = true
	}
	for _, n := range nodes {
		if n.T == 1 && !present[n.P] {
			return n.H
		}
	}
	return nodes[0].H
}

// resolveKeys unwraps each node key. The share key from the link encrypts every
// node in the tree, however deep it sits, so one key opens the lot.
func resolveKeys(nodes []node, shareKey []byte) map[string]nodeKey {
	keys := make(map[string]nodeKey, len(nodes))

	for pass := 0; pass < maxFolderDepth+2; pass++ {
		progress := false
		for _, n := range nodes {
			if _, done := keys[n.H]; done {
				continue
			}
			blob, err := decodeBase64(nodeKeyField(n))
			if err != nil || (len(blob) != 16 && len(blob) != 32) {
				// A node whose key we cannot read is skipped, not fatal: one
				// broken entry should not lose the rest of the folder.
				keys[n.H] = nodeKey{}
				progress = true
				continue
			}

			plain := ecbDecrypt(shareKey, blob)
			if len(plain) == 32 {
				// Files carry an obfuscated 32-byte key, exactly like a file
				// link's fragment.
				key, iv, keyErr := fileKey(plain)
				if keyErr != nil {
					keys[n.H] = nodeKey{}
				} else {
					keys[n.H] = nodeKey{key: key, iv: iv}
				}
			} else {
				keys[n.H] = nodeKey{key: plain}
			}
			progress = true
		}
		if !progress {
			break
		}
	}
	return keys
}

// nodeKeyField picks this node's encrypted key out of the "handle:key" chain
// MEGA stores, preferring the entry keyed by the node itself and falling back to
// the one keyed by its parent.
func nodeKeyField(n node) string {
	var fallback string
	for _, pair := range strings.Split(n.K, "/") {
		i := strings.IndexByte(pair, ':')
		if i <= 0 {
			continue
		}
		fallback = pair[i+1:]
		if pair[:i] == n.H {
			return pair[i+1:]
		}
		if pair[:i] == n.P {
			fallback = pair[i+1:]
		}
	}
	return fallback
}

// ecbDecrypt decrypts whole AES blocks with the key, which is how MEGA wraps
// node keys.
func ecbDecrypt(key, data []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	out := make([]byte, len(data))
	for i := 0; i+16 <= len(data); i += 16 {
		block.Decrypt(out[i:i+16], data[i:i+16])
	}
	return out
}
