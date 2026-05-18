package docs

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// makeZipWithEntries returns an in-memory zip with the given number of
// tiny entries. Each entry has a 1-byte body so the test is fast and
// deterministic — we're exercising the entry count, not byte volume.
func makeZipWithEntries(t *testing.T, count int) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for i := 0; i < count; i++ {
		w, err := zw.Create(zipEntryName(i))
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("x"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipEntryName(i int) string {
	// Distinct filenames so duplicate-name short-circuits don't hide bugs.
	return "f" + strings.Repeat("0", 4) + itoa(i)
}

func itoa(i int) string {
	// Avoid importing strconv just for this; the test is small.
	if i == 0 {
		return "0"
	}
	var b [12]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

// Regression for H-5 entry-count cap: an archive over maxEntries entries
// must fail before extraction completes.
func TestExtractZipRejectsTooManyEntries(t *testing.T) {
	dest := t.TempDir()
	// One more than the cap.
	data := makeZipWithEntries(t, maxEntries+1)

	err := ExtractArchive(bytes.NewReader(data), "many.zip", dest)
	if err == nil {
		t.Fatal("expected entry-count cap to reject this archive")
	}
	if !strings.Contains(err.Error(), "too many entries") {
		t.Errorf("expected entry-count error, got: %v", err)
	}
}

// Regression for H-5 total-bytes cap: an archive whose entries sum to more
// than maxTotalSize must fail mid-stream. Using a small synthetic cap by
// crafting entries with a known-size payload that sums above the limit.
func TestExtractZipRejectsTotalSizeBomb(t *testing.T) {
	dest := t.TempDir()

	// Each entry holds a buffer just under the per-file cap to grow the
	// total quickly. 11 entries × 100MB > 1GB total — but we never need to
	// actually write 1GB because the limiter aborts mid-Write.
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	const perEntry = 100 << 20 // 100 MB, the per-file cap
	const entries = 11         // 11 × 100MB = 1.1 GB, over the 1 GB total cap
	chunk := make([]byte, 1<<20) // 1 MB pattern reused per entry

	for i := 0; i < entries; i++ {
		w, err := zw.Create(zipEntryName(i))
		if err != nil {
			t.Fatal(err)
		}
		// Write 100 MB of compressible zeros so the zip stays small but the
		// uncompressed total exceeds maxTotalSize.
		for written := 0; written < perEntry; written += len(chunk) {
			w.Write(chunk)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	err := ExtractArchive(bytes.NewReader(buf.Bytes()), "bomb.zip", dest)
	if err == nil {
		t.Fatal("expected total-size cap to reject this archive")
	}
	if !strings.Contains(err.Error(), "expands beyond total extraction limit") {
		t.Errorf("expected total-size error, got: %v", err)
	}
}

// Sanity: a normal small zip still extracts after the limit changes.
func TestExtractZipNormalUploadStillWorks(t *testing.T) {
	dest := t.TempDir()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	w, _ := zw.Create("index.html")
	w.Write([]byte("<h1>hello</h1>"))
	w, _ = zw.Create("css/style.css")
	w.Write([]byte("body{}"))

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ExtractArchive(bytes.NewReader(buf.Bytes()), "ok.zip", dest); err != nil {
		t.Fatalf("normal zip should extract: %v", err)
	}
}
