package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSearchSnippetEscapesHTML guards against the regression the audit report
// (C-3) feared: that search result snippets could contain unescaped HTML/JS
// from indexed document text. Bleve's html highlighter currently escapes the
// surrounding text and only emits <mark>...</mark> unescaped, but the search
// page template uses {{safe .Snippet}} to render snippets — if a future
// highlighter swap (or a custom formatter) ever skips escaping, this test
// catches it.
func TestSearchSnippetEscapesHTML(t *testing.T) {
	storageDir := t.TempDir()
	si, err := NewSearchIndex(storageDir)
	if err != nil {
		t.Fatal(err)
	}
	defer si.Close()

	// Write an HTML document whose visible text contains an XSS-shaped payload.
	// After tokenizer.Text() decodes entities, the indexed text will contain
	// the literal "<script>" and friends.
	versionDir := filepath.Join(storageDir, "proj", "v1")
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		t.Fatal(err)
	}
	body := `<html><body><pre>Search me: &lt;script&gt;alert(&#34;XSS&#34;)&lt;/script&gt; and an &amp; ampersand.</pre></body></html>`
	if err := os.WriteFile(filepath.Join(versionDir, "index.html"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	if err := si.IndexVersion(1, 1, "proj", "Proj", "v1", versionDir); err != nil {
		t.Fatal(err)
	}

	res, err := si.Search(SearchQuery{Query: "script", Limit: 10}, map[string]string{"proj": "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) == 0 {
		t.Fatal("expected at least one hit; index/search produced none")
	}

	snippet := res.Results[0].Snippet
	if snippet == "" {
		t.Fatal("expected a non-empty snippet")
	}

	// The unescaped HTML must NOT be present.
	for _, danger := range []string{
		"<script>",
		"</script>",
		`alert("XSS")`,
	} {
		if strings.Contains(snippet, danger) {
			t.Errorf("snippet must escape %q but contains it raw. Snippet: %s", danger, snippet)
		}
	}

	// The escaped forms SHOULD be present (proof we actually found the match
	// and that escaping is what's happening, not just absence of the term).
	if !strings.Contains(snippet, "&lt;") || !strings.Contains(snippet, "&gt;") {
		t.Errorf("expected the snippet to contain escaped < and >, got: %s", snippet)
	}

	// The <mark> tags must be emitted unescaped — that's the whole point of
	// highlighting in HTML output and the reason the template uses {{safe}}.
	if !strings.Contains(snippet, "<mark>") {
		t.Errorf("expected the snippet to contain <mark> tags around the match, got: %s", snippet)
	}
}
