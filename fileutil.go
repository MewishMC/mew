package main

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	rar "github.com/nwaples/rardecode/v2"
)

// safeArchiveName reduces a file name that came from outside the app (a
// download, a Content-Disposition header, an archive entry) to a name that can
// only ever create a file in the folder it is joined with. A name carrying
// "..\\" or a drive letter would otherwise let a remote server choose where the
// app writes.
func safeArchiveName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\\|?*`, r) {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		return "pack.zip"
	}
	return name
}

func isRarFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".rar")
}

func isZipFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".zip")
}

func isArchive(path string) bool {
	return isZipFile(path) || isRarFile(path)
}

func extractArchive(src, dest string) error {
	if isRarFile(src) {
		return unrar(src, dest)
	}
	return unzip(src, dest)
}

func unrar(src, dest string) error {
	r, err := rar.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for {
		header, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		fpath := filepath.Join(dest, header.Name)

		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return &os.PathError{Op: "extract", Path: fpath, Err: os.ErrInvalid}
		}

		if header.IsDir {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, header.Mode())
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, r)
		outFile.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return &os.PathError{Op: "extract", Path: fpath, Err: os.ErrInvalid}
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyDirAll(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		dstPath := filepath.Join(dst, rel)
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(dstPath, info.Mode())
		}
		return copyFile(path, dstPath)
	})
}

func createZipFromFolder(srcDir, zipPath string) error {
	outFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	w := zip.NewWriter(outFile)
	defer w.Close()

	return filepath.Walk(srcDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// Zip entry names are always slash separated; on Windows filepath.Rel
		// returns backslashes, which some readers (and Bedrock) will not open.
		f, err := w.Create(filepath.ToSlash(relPath))
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}

		// Close each file before the next entry: a defer here would hold one
		// handle per file for the whole walk and exhaust the limit on big packs.
		_, err = io.Copy(f, file)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
