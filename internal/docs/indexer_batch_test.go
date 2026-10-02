package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestIndexVersionIndexesEveryFileAcrossBatchFlushes guards the batch flushing
// in IndexVersion. The batch is flushed and replaced mid-walk to keep peak
// memory bounded, which is exactly the kind of change that can drop the
// documents sitting in a batch at a boundary. Each case straddles one:
// indexBatchSize-1 fits in a single batch, indexBatchSize flushes with nothing
// left over, and indexBatchSize+1 leaves a remainder for the final flush.
func TestIndexVersionIndexesEveryFileAcrossBatchFlushes(t *testing.T) {
	for _, files := range []int{indexBatchSize - 1, indexBatchSize, indexBatchSize + 1} {
		t.Run(fmt.Sprintf("%d_files", files), func(t *testing.T) {
			storageDir := t.TempDir()
			si, err := NewSearchIndex(storageDir)
			if err != nil {
				t.Fatal(err)
			}
			defer si.Close()

			versionDir := filepath.Join(storageDir, "proj", "v1")
			if err := os.MkdirAll(versionDir, 0755); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < files; i++ {
				body := fmt.Sprintf("<html><body><h1>Page %d</h1><p>needle %d</p></body></html>", i, i)
				name := filepath.Join(versionDir, fmt.Sprintf("page-%d.html", i))
				if err := os.WriteFile(name, []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}

			if err := si.IndexVersion(1, 1, "proj", "Proj", "v1", versionDir); err != nil {
				t.Fatal(err)
			}

			got, err := si.index.DocCount()
			if err != nil {
				t.Fatal(err)
			}
			if got != uint64(files) {
				t.Errorf("indexed %d documents, want %d", got, files)
			}

			// The last file written is the one most likely to be stranded in
			// an unflushed batch.
			last := fmt.Sprintf("1/1/page-%d.html", files-1)
			doc, err := si.index.Document(last)
			if err != nil {
				t.Fatal(err)
			}
			if doc == nil {
				t.Errorf("document %s is missing", last)
			}
		})
	}
}

// TestIndexVersionOnEmptyDirectory covers the path where nothing is indexed at
// all: with no documents there is no batch left to flush.
func TestIndexVersionOnEmptyDirectory(t *testing.T) {
	storageDir := t.TempDir()
	si, err := NewSearchIndex(storageDir)
	if err != nil {
		t.Fatal(err)
	}
	defer si.Close()

	versionDir := filepath.Join(storageDir, "proj", "v1")
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := si.IndexVersion(1, 1, "proj", "Proj", "v1", versionDir); err != nil {
		t.Fatal(err)
	}

	got, err := si.index.DocCount()
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("indexed %d documents from an empty directory, want 0", got)
	}
}
