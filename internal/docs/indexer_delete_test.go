package docs

import (
	"fmt"
	"testing"
)

// docsPastOldSearchCap is one more than the 10000-hit search request the
// deletion paths used to rely on. Both DeleteVersion and ReindexAll fetched a
// single page of that size and ignored everything past it, so on an index this
// large they deleted the wrong documents, or none at all.
const docsPastOldSearchCap = 10001

// seedIndex puts n documents into the index without going through
// IndexVersion, so the test does not need n files on disk. Document ids follow
// the same "projectID/versionID/path" shape the real indexer uses, because
// DeleteVersion identifies a version's documents by that prefix.
func seedIndex(t *testing.T, si *SearchIndex, projectID, versionID int64, n int) {
	t.Helper()

	batch := si.index.NewBatch()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%d/%d/page-%d.html", projectID, versionID, i)
		doc := indexDoc{
			ProjectSlug: fmt.Sprintf("proj-%d", projectID),
			VersionTag:  fmt.Sprintf("v%d", versionID),
			FilePath:    fmt.Sprintf("page-%d.html", i),
			PageTitle:   "Page",
			TextContent: "needle",
			ProjectID:   projectID,
			VersionID:   versionID,
		}
		if err := batch.Index(id, doc); err != nil {
			t.Fatal(err)
		}
		if batch.Size() >= 1000 {
			if err := si.index.Batch(batch); err != nil {
				t.Fatal(err)
			}
			batch = si.index.NewBatch()
		}
	}
	if batch.Size() > 0 {
		if err := si.index.Batch(batch); err != nil {
			t.Fatal(err)
		}
	}
}

func indexedDocCount(t *testing.T, si *SearchIndex) uint64 {
	t.Helper()
	n, err := si.index.DocCount()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestDeleteVersionRemovesEveryDocumentPastTheOldCap deletes a version from an
// index holding more documents than the old 10000-hit page. Every document
// belongs to the version being deleted, so a complete deletion empties the
// index. The old implementation left the remainder behind, which is how pages
// of a deleted version kept appearing in search results.
func TestDeleteVersionRemovesEveryDocumentPastTheOldCap(t *testing.T) {
	si, err := NewSearchIndex(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer si.Close()

	seedIndex(t, si, 1, 1, docsPastOldSearchCap)
	if got := indexedDocCount(t, si); got != docsPastOldSearchCap {
		t.Fatalf("seeded %d documents, index holds %d", docsPastOldSearchCap, got)
	}

	if err := si.DeleteVersion(1, 1); err != nil {
		t.Fatal(err)
	}

	if got := indexedDocCount(t, si); got != 0 {
		t.Errorf("index still holds %d documents after deleting the only version", got)
	}
}

// TestDeleteVersionLeavesOtherVersionsAlone is the other half: a complete
// deletion must still be a selective one.
func TestDeleteVersionLeavesOtherVersionsAlone(t *testing.T) {
	si, err := NewSearchIndex(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer si.Close()

	seedIndex(t, si, 1, 1, 50)
	seedIndex(t, si, 1, 2, 50)
	seedIndex(t, si, 2, 1, 50)

	if err := si.DeleteVersion(1, 1); err != nil {
		t.Fatal(err)
	}

	if got := indexedDocCount(t, si); got != 100 {
		t.Errorf("index holds %d documents, want the 100 belonging to other versions", got)
	}

	// Version 1/2 shares the "1/" project prefix and 2/1 shares the "/1/"
	// version id: neither may be mistaken for the deleted 1/1.
	for _, id := range []string{"1/2/page-0.html", "2/1/page-0.html"} {
		doc, err := si.index.Document(id)
		if err != nil {
			t.Fatal(err)
		}
		if doc == nil {
			t.Errorf("document %s was deleted along with version 1/1", id)
		}
	}
}

// TestReindexAllClearsEveryDocumentPastTheOldCap covers the same cap in
// ReindexAllWithProgress. Rebuilding from scratch with no versions to index
// must leave an empty index; the old code left everything past the first page
// in place, so a reindex could not clear out documents whose files are gone.
func TestReindexAllClearsEveryDocumentPastTheOldCap(t *testing.T) {
	si, err := NewSearchIndex(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer si.Close()

	seedIndex(t, si, 1, 1, docsPastOldSearchCap)

	if err := si.ReindexAll(nil, nil); err != nil {
		t.Fatal(err)
	}

	if got := indexedDocCount(t, si); got != 0 {
		t.Errorf("index still holds %d documents after a full reindex with no versions", got)
	}
}
