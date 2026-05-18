package docs

import (
	"archive/tar"
	"archive/zip"
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

// Extraction quotas. The per-file cap was already present; the archive,
// total, and entry caps were added to defuse decompression bombs (audit
// finding H-5). All four can be exceeded under reasonable use only with
// a malicious or accidentally-pathological upload.
const (
	maxFileSize    = 100 << 20  // 100 MB per extracted file
	maxArchiveSize = 1 << 30    // 1 GB input archive (after MaxBytesReader)
	maxTotalSize   = 1 << 30    // 1 GB total extracted bytes per archive
	maxEntries     = 10000      // entry-count cap per archive
)

// extractStats tracks the running quotas during a single extraction.
type extractStats struct {
	entries int
	bytes   int64
}

// nextEntry bumps the entry counter; returns an error once the cap is
// exceeded so the extraction aborts before opening another stream.
func (s *extractStats) nextEntry() error {
	s.entries++
	if s.entries > maxEntries {
		return fmt.Errorf("archive contains too many entries (limit %d)", maxEntries)
	}
	return nil
}

// addBytes credits n bytes against the total; returns an error if the
// cumulative total would exceed maxTotalSize. Called from copyEntry's
// incremental writer so a bomb aborts mid-stream rather than after
// writing GB to disk.
func (s *extractStats) addBytes(n int64) error {
	s.bytes += n
	if s.bytes > maxTotalSize {
		return fmt.Errorf("archive expands beyond total extraction limit (%d bytes)", maxTotalSize)
	}
	return nil
}

// limitedWriter wraps an underlying writer and aborts the copy as soon as
// the per-archive byte budget is exhausted.
type limitedWriter struct {
	w     io.Writer
	stats *extractStats
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if err := lw.stats.addBytes(int64(len(p))); err != nil {
		return 0, err
	}
	return lw.w.Write(p)
}

// bufferArchiveToTempFile copies r into a temporary file on disk so the
// archive readers (zip, 7z) can satisfy their io.ReaderAt + size contract
// without holding the whole archive in memory.
//
// The caller is responsible for both Close and os.Remove on the returned
// file; the helper at archive call sites uses a single closure to do both.
func bufferArchiveToTempFile(r io.Reader) (*os.File, int64, error) {
	tmp, err := os.CreateTemp("", "asiakirjat-archive-*")
	if err != nil {
		return nil, 0, fmt.Errorf("creating temp file for archive: %w", err)
	}
	n, err := io.Copy(tmp, io.LimitReader(r, maxArchiveSize))
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, 0, fmt.Errorf("buffering archive: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, 0, fmt.Errorf("seeking buffered archive: %w", err)
	}
	return tmp, n, nil
}

// closeAndRemove is a defer-friendly helper that cleans up the temp file.
func closeAndRemove(f *os.File) {
	name := f.Name()
	f.Close()
	os.Remove(name)
}

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
	// zip.Reader needs io.ReaderAt + size, so buffer to a temp file rather
	// than RAM (H-6: previously io.ReadAll up to 1 GB per concurrent upload).
	tmp, size, err := bufferArchiveToTempFile(r)
	if err != nil {
		return err
	}
	defer closeAndRemove(tmp)

	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return fmt.Errorf("opening zip: %w", err)
	}

	prefix := detectSingleRoot(zr)
	stats := &extractStats{}

	for _, f := range zr.File {
		name := f.Name
		if prefix != "" {
			name = strings.TrimPrefix(name, prefix)
			if name == "" {
				continue
			}
		}

		target := filepath.Join(destDir, name)

		if !isPathSafe(destDir, target) {
			return fmt.Errorf("zip-slip detected: %s", f.Name)
		}

		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}

		if err := stats.nextEntry(); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}
		if err := extractZipFile(f, target, stats); err != nil {
			return err
		}
	}

	return nil
}

func extractZipFile(f *zip.File, target string, stats *extractStats) error {
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

	lw := &limitedWriter{w: out, stats: stats}
	if _, err := io.Copy(lw, io.LimitReader(rc, maxFileSize)); err != nil {
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
	tmp, size, err := bufferArchiveToTempFile(r)
	if err != nil {
		return err
	}
	defer closeAndRemove(tmp)

	szr, err := sevenzip.NewReader(tmp, size)
	if err != nil {
		return fmt.Errorf("opening 7z: %w", err)
	}

	prefix := detectSingleRoot7z(szr)
	stats := &extractStats{}

	for _, f := range szr.File {
		name := f.Name
		if prefix != "" {
			name = strings.TrimPrefix(name, prefix)
			if name == "" {
				continue
			}
		}

		target := filepath.Join(destDir, name)

		if !isPathSafe(destDir, target) {
			return fmt.Errorf("path traversal detected: %s", f.Name)
		}

		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}

		if err := stats.nextEntry(); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}
		if err := extract7zFile(f, target, stats); err != nil {
			return err
		}
	}

	return nil
}

func extract7zFile(f *sevenzip.File, target string, stats *extractStats) error {
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

	lw := &limitedWriter{w: out, stats: stats}
	if _, err := io.Copy(lw, io.LimitReader(rc, maxFileSize)); err != nil {
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
	stats := &extractStats{}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		name := stripSingleRootTar(header.Name)
		if name == "" || name == "." {
			continue
		}

		target := filepath.Join(destDir, name)

		if !isPathSafe(destDir, target) {
			return fmt.Errorf("path traversal detected: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0755)
		case tar.TypeReg:
			if err := stats.nextEntry(); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}

			out, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("creating file: %w", err)
			}

			lw := &limitedWriter{w: out, stats: stats}
			if _, err := io.Copy(lw, io.LimitReader(tr, maxFileSize)); err != nil {
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
