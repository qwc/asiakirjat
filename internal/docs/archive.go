package docs

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/ulikunitz/xz"
)

const maxFileSize = 100 << 20 // 100 MB per file

// ExtractArchive detects the archive format from the filename and extracts to destDir.
func ExtractArchive(r io.Reader, filename, destDir string) error {
	lower := strings.ToLower(filename)

	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(r, destDir)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(r, destDir)
	case strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2"):
		return extractTarBz2(r, destDir)
	case strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz"):
		return extractTarXz(r, destDir)
	case strings.HasSuffix(lower, ".7z"):
		return extract7z(r, destDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", filename)
	}
}

func extractZip(r io.Reader, destDir string) error {
	// zip.Reader needs io.ReaderAt, so we buffer to memory/disk
	data, err := io.ReadAll(io.LimitReader(r, maxFileSize*10))
	if err != nil {
		return fmt.Errorf("reading zip data: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("opening zip: %w", err)
	}

	// Detect single root directory for flattening
	prefix := detectSingleRoot(zr)

	for _, f := range zr.File {
		name := f.Name
		if prefix != "" {
			name = strings.TrimPrefix(name, prefix)
			if name == "" {
				continue
			}
		}

		target := filepath.Join(destDir, name)

		// Zip-slip protection
		if !isPathSafe(destDir, target) {
			return fmt.Errorf("zip-slip detected: %s", f.Name)
		}

		// Skip symlinks
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}

		if err := extractZipFile(f, target); err != nil {
			return err
		}
	}

	return nil
}

func extractZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("opening zip entry: %w", err)
	}
	defer rc.Close()

	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, io.LimitReader(rc, maxFileSize)); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

func detectSingleRoot(zr *zip.Reader) string {
	if len(zr.File) == 0 {
		return ""
	}

	// Check if all entries share a common root directory
	var root string
	for _, f := range zr.File {
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			return "" // file at root level
		}
		if root == "" {
			root = parts[0]
		} else if parts[0] != root {
			return "" // multiple roots
		}
	}

	if root != "" {
		return root + "/"
	}
	return ""
}

func extractTarGz(r io.Reader, destDir string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("opening gzip: %w", err)
	}
	defer gr.Close()

	return extractTar(gr, destDir)
}

func extractTarBz2(r io.Reader, destDir string) error {
	br := bzip2.NewReader(r)
	return extractTar(br, destDir)
}

func extractTarXz(r io.Reader, destDir string) error {
	xr, err := xz.NewReader(r)
	if err != nil {
		return fmt.Errorf("opening xz: %w", err)
	}
	return extractTar(xr, destDir)
}

func extract7z(r io.Reader, destDir string) error {
	// sevenzip.Reader needs io.ReaderAt, so we buffer to memory
	data, err := io.ReadAll(io.LimitReader(r, maxFileSize*10))
	if err != nil {
		return fmt.Errorf("reading 7z data: %w", err)
	}

	szr, err := sevenzip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("opening 7z: %w", err)
	}

	// Detect single root directory for flattening
	prefix := detectSingleRoot7z(szr)

	for _, f := range szr.File {
		name := f.Name
		if prefix != "" {
			name = strings.TrimPrefix(name, prefix)
			if name == "" {
				continue
			}
		}

		target := filepath.Join(destDir, name)

		// Path traversal protection
		if !isPathSafe(destDir, target) {
			return fmt.Errorf("path traversal detected: %s", f.Name)
		}

		// Skip symlinks
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}

		if err := extract7zFile(f, target); err != nil {
			return err
		}
	}

	return nil
}

func extract7zFile(f *sevenzip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("opening 7z entry: %w", err)
	}
	defer rc.Close()

	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, io.LimitReader(rc, maxFileSize)); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

func detectSingleRoot7z(szr *sevenzip.Reader) string {
	if len(szr.File) == 0 {
		return ""
	}

	var root string
	for _, f := range szr.File {
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			return "" // file at root level
		}
		if root == "" {
			root = parts[0]
		} else if parts[0] != root {
			return "" // multiple roots
		}
	}

	if root != "" {
		return root + "/"
	}
	return ""
}

func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		name := header.Name

		// Strip leading single root if present
		name = stripSingleRootTar(name)
		if name == "" || name == "." {
			continue
		}

		target := filepath.Join(destDir, name)

		// Path traversal protection
		if !isPathSafe(destDir, target) {
			return fmt.Errorf("path traversal detected: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}

			out, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("creating file: %w", err)
			}

			if _, err := io.Copy(out, io.LimitReader(tr, maxFileSize)); err != nil {
				out.Close()
				return fmt.Errorf("writing file: %w", err)
			}
			out.Close()
		default:
			// Skip symlinks and other special types
			continue
		}
	}

	return nil
}

// stripSingleRootTar is a simple heuristic: if the path starts with
// a directory name followed by /, strip that prefix.
// This handles the common case of tarballs with a single root directory.
func stripSingleRootTar(name string) string {
	// This is a simplified approach — strips first component if it looks like a single root
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 2 && parts[1] != "" {
		return parts[1]
	}
	return name
}

// WriteZipFromDir walks srcDir and streams its contents as a zip archive to w.
// Paths inside the zip are relative to srcDir, using forward slashes.
// Symlinks, directories, and non-regular files are skipped.
func WriteZipFromDir(w io.Writer, srcDir string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}

		fw, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return fmt.Errorf("creating zip entry %s: %w", rel, err)
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("opening %s: %w", rel, err)
		}
		defer f.Close()

		if _, err := io.Copy(fw, f); err != nil {
			return fmt.Errorf("writing %s: %w", rel, err)
		}
		return nil
	})
}

func isPathSafe(base, target string) bool {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absTarget, absBase+string(filepath.Separator)) || absTarget == absBase
}
