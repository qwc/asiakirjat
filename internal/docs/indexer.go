package docs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/highlight/highlighter/html"
	"github.com/blevesearch/bleve/v2/search/query"
	xhtml "golang.org/x/net/html"
)

// SearchIndex wraps a bleve index for full-text search of documentation content.
type SearchIndex struct {
	index bleve.Index
	path  string
}

// indexDoc is the document structure stored in the bleve index.
type indexDoc struct {
	ProjectSlug string `json:"project_slug"`
	ProjectName string `json:"project_name"`
	VersionTag  string `json:"version_tag"`
	FilePath    string `json:"file_path"`
	PageTitle   string `json:"page_title"`
	TextContent string `json:"text_content"`
	ProjectID   int64  `json:"project_id"`
	VersionID   int64  `json:"version_id"`
}

// SearchQuery describes a full-text search request.
type SearchQuery struct {
	Query       string
	ProjectSlug string // empty = all projects
	VersionTag  string // empty = latest only (unless AllVersions)
	AllVersions bool
	Limit       int
	Offset      int
}

// SearchResult is a single search hit.
type SearchResult struct {
	ProjectSlug string `json:"project_slug"`
	ProjectName string `json:"project_name"`
	VersionTag  string `json:"version_tag"`
	FilePath    string `json:"file_path"`
	PageTitle   string `json:"page_title"`
	Snippet     string `json:"snippet"`
	URL         string `json:"url"`
}

// SearchResults contains paged search results.
type SearchResults struct {
	Results []SearchResult `json:"results"`
	Total   uint64         `json:"total"`
}

func buildIndexMapping() *mapping.IndexMappingImpl {
	indexMapping := bleve.NewIndexMapping()

	docMapping := bleve.NewDocumentMapping()

	textFieldMapping := bleve.NewTextFieldMapping()
	textFieldMapping.Store = true
	textFieldMapping.IncludeTermVectors = true

	keywordFieldMapping := bleve.NewKeywordFieldMapping()
	keywordFieldMapping.Store = true

	numericFieldMapping := bleve.NewNumericFieldMapping()
	numericFieldMapping.Store = true
	numericFieldMapping.Index = false

	docMapping.AddFieldMappingsAt("project_slug", keywordFieldMapping)
	docMapping.AddFieldMappingsAt("project_name", textFieldMapping)
	docMapping.AddFieldMappingsAt("version_tag", keywordFieldMapping)
	docMapping.AddFieldMappingsAt("file_path", keywordFieldMapping)
	docMapping.AddFieldMappingsAt("page_title", textFieldMapping)
	docMapping.AddFieldMappingsAt("text_content", textFieldMapping)
	docMapping.AddFieldMappingsAt("project_id", numericFieldMapping)
	docMapping.AddFieldMappingsAt("version_id", numericFieldMapping)

	indexMapping.DefaultMapping = docMapping

	return indexMapping
}

// NewSearchIndex opens or creates a bleve index at the given path.
func NewSearchIndex(basePath string) (*SearchIndex, error) {
	indexPath := filepath.Join(basePath, ".search-index")

	idx, err := bleve.Open(indexPath)
	if err == bleve.ErrorIndexPathDoesNotExist {
		m := buildIndexMapping()
		idx, err = bleve.New(indexPath, m)
		if err != nil {
			return nil, fmt.Errorf("creating search index: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("opening search index: %w", err)
	}

	return &SearchIndex{index: idx, path: indexPath}, nil
}

// Close closes the bleve index.
func (si *SearchIndex) Close() error {
	return si.index.Close()
}

// ExtractTextFromHTML reads an HTML file and returns the page title and plain text content.
// It skips script, style, and nav elements.
func ExtractTextFromHTML(filePath string) (title, text string, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	return extractTextFromReader(f)
}

func extractTextFromReader(r io.Reader) (title, text string, err error) {
	tokenizer := xhtml.NewTokenizer(r)

	var textBuilder strings.Builder
	var titleBuilder strings.Builder
	skipTags := map[string]bool{"script": true, "style": true, "nav": true}
	skipDepth := 0
	inTitle := false

	for {
		tt := tokenizer.Next()
		switch tt {
		case xhtml.ErrorToken:
			err := tokenizer.Err()
			if err == io.EOF {
				return strings.TrimSpace(titleBuilder.String()), strings.TrimSpace(textBuilder.String()), nil
			}
			return "", "", err

		case xhtml.StartTagToken:
			tn, _ := tokenizer.TagName()
			tagName := string(tn)
			if skipTags[tagName] {
				skipDepth++
			}
			if tagName == "title" {
				inTitle = true
			}

		case xhtml.EndTagToken:
			tn, _ := tokenizer.TagName()
			tagName := string(tn)
			if skipTags[tagName] && skipDepth > 0 {
				skipDepth--
			}
			if tagName == "title" {
				inTitle = false
			}
			// Add space after block elements
			if isBlockElement(tagName) && textBuilder.Len() > 0 {
				textBuilder.WriteByte(' ')
			}

		case xhtml.TextToken:
			if skipDepth > 0 {
				continue
			}
			content := strings.TrimSpace(string(tokenizer.Text()))
			if content == "" {
				continue
			}
			if inTitle {
				titleBuilder.WriteString(content)
			}
			textBuilder.WriteString(content)
			textBuilder.WriteByte(' ')
		}
	}
}

func isBlockElement(tag string) bool {
	switch tag {
	case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6",
		"li", "tr", "td", "th", "br", "hr", "blockquote",
		"pre", "section", "article", "aside", "main":
		return true
	}
	return false
}

// IndexVersion walks HTML files in a version's storage path and indexes them.
func (si *SearchIndex) IndexVersion(projectID, versionID int64, projectSlug, projectName, versionTag, storagePath string) error {
	batch := si.index.NewBatch()

	err := filepath.Walk(storagePath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // skip files we can't access
		}
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".html" && ext != ".htm" {
			return nil
		}

		relPath, relErr := filepath.Rel(storagePath, path)
		if relErr != nil {
			return nil
		}

		pageTitle, textContent, extractErr := ExtractTextFromHTML(path)
		if extractErr != nil {
			return nil // skip files we can't parse
		}

		if textContent == "" {
			return nil
		}

		docID := fmt.Sprintf("%d/%d/%s", projectID, versionID, relPath)
		doc := indexDoc{
			ProjectSlug: projectSlug,
			ProjectName: projectName,
			VersionTag:  versionTag,
			FilePath:    relPath,
			PageTitle:   pageTitle,
			TextContent: textContent,
			ProjectID:   projectID,
			VersionID:   versionID,
		}

		batch.Index(docID, doc)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking version directory: %w", err)
	}

	if err := si.index.Batch(batch); err != nil {
		return fmt.Errorf("indexing batch: %w", err)
	}

	return nil
}

// DeleteVersion removes all indexed documents for a given version.
func (si *SearchIndex) DeleteVersion(projectID, versionID int64) error {
	prefix := fmt.Sprintf("%d/%d/", projectID, versionID)

	q := bleve.NewMatchAllQuery()
	req := bleve.NewSearchRequest(q)
	req.Size = 10000
	req.Fields = []string{}

	results, err := si.index.Search(req)
	if err != nil {
		return fmt.Errorf("searching for version docs: %w", err)
	}

	batch := si.index.NewBatch()
	for _, hit := range results.Hits {
		if strings.HasPrefix(hit.ID, prefix) {
			batch.Delete(hit.ID)
		}
	}

	if err := si.index.Batch(batch); err != nil {
		return fmt.Errorf("deleting version docs: %w", err)
	}

	return nil
}

// Search performs a full-text search across indexed documentation.
func (si *SearchIndex) Search(sq SearchQuery, latestVersionTags map[string]string) (*SearchResults, error) {
	if sq.Limit <= 0 {
		sq.Limit = 20
	}

	// Build the text query across content and title
	matchQ := bleve.NewMatchQuery(sq.Query)

	contentPhraseQ := bleve.NewMatchPhraseQuery(sq.Query)
	contentPhraseQ.SetField("text_content")
	contentPhraseQ.SetBoost(2.0)

	titlePhraseQ := bleve.NewMatchPhraseQuery(sq.Query)
	titlePhraseQ.SetField("page_title")
	titlePhraseQ.SetBoost(5.0)

	// Fuzzy query for typo tolerance (low boost as fallback)
	fuzzyContentQ := bleve.NewFuzzyQuery(sq.Query)
	fuzzyContentQ.SetField("text_content")
	fuzzyContentQ.SetFuzziness(1) // Allow 1 edit distance
	fuzzyContentQ.SetBoost(0.5)

	fuzzyTitleQ := bleve.NewFuzzyQuery(sq.Query)
	fuzzyTitleQ.SetField("page_title")
	fuzzyTitleQ.SetFuzziness(1)
	fuzzyTitleQ.SetBoost(0.8)

	textQuery := bleve.NewDisjunctionQuery(matchQ, contentPhraseQ, titlePhraseQ, fuzzyContentQ, fuzzyTitleQ)

	// Build filter queries
	var filters []query.Query
	filters = append(filters, textQuery)

	if sq.ProjectSlug != "" {
		pq := bleve.NewTermQuery(sq.ProjectSlug)
		pq.SetField("project_slug")
		filters = append(filters, pq)
	}

	if sq.VersionTag != "" {
		vq := bleve.NewTermQuery(sq.VersionTag)
		vq.SetField("version_tag")
		filters = append(filters, vq)
	} else if !sq.AllVersions && latestVersionTags != nil && len(latestVersionTags) > 0 {
		var versionQueries []query.Query
		for _, tag := range latestVersionTags {
			vq := bleve.NewTermQuery(tag)
			vq.SetField("version_tag")
			versionQueries = append(versionQueries, vq)
		}
		if len(versionQueries) > 0 {
			filters = append(filters, bleve.NewDisjunctionQuery(versionQueries...))
		}
	}

	var finalQuery query.Query
	if len(filters) == 1 {
		finalQuery = filters[0]
	} else {
		finalQuery = bleve.NewConjunctionQuery(filters...)
	}

	searchReq := bleve.NewSearchRequestOptions(finalQuery, sq.Limit, sq.Offset, false)
	searchReq.Fields = []string{"project_slug", "project_name", "version_tag", "file_path", "page_title"}
	searchReq.Highlight = bleve.NewHighlightWithStyle(html.Name)
	searchReq.Highlight.AddField("text_content")
	searchReq.Highlight.AddField("page_title")

	searchResult, err := si.index.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	results := &SearchResults{
		Total:   searchResult.Total,
		Results: make([]SearchResult, 0, len(searchResult.Hits)),
	}

	for _, hit := range searchResult.Hits {
		sr := SearchResult{
			ProjectSlug: fieldString(hit.Fields, "project_slug"),
			ProjectName: fieldString(hit.Fields, "project_name"),
			VersionTag:  fieldString(hit.Fields, "version_tag"),
			FilePath:    fieldString(hit.Fields, "file_path"),
			PageTitle:   fieldString(hit.Fields, "page_title"),
		}

		if fragments, ok := hit.Fragments["text_content"]; ok && len(fragments) > 0 {
			sr.Snippet = fragments[0]
		} else if fragments, ok := hit.Fragments["page_title"]; ok && len(fragments) > 0 {
			sr.Snippet = fragments[0]
		}

		sr.URL = "/project/" + sr.ProjectSlug + "/" + sr.VersionTag + "/" + sr.FilePath

		results.Results = append(results.Results, sr)
	}

	return results, nil
}

// ReindexProject holds project data for reindexing.
type ReindexProject struct {
	ID   int64
	Slug string
	Name string
}

// ReindexVersion holds version data for reindexing.
type ReindexVersion struct {
	ID          int64
	ProjectID   int64
	Tag         string
	StoragePath string
}

// ReindexProgress reports reindexing progress.
type ReindexProgress struct {
	Current int
	Total   int
	Project string
	Version string
}

// ReindexProgressFunc is called for each version during reindexing.
type ReindexProgressFunc func(progress ReindexProgress)

// ReindexAll rebuilds the entire search index from scratch.
func (si *SearchIndex) ReindexAll(projects []ReindexProject, versions []ReindexVersion) error {
	return si.ReindexAllWithProgress(projects, versions, nil)
}

// ReindexAllWithProgress rebuilds the index with progress reporting.
func (si *SearchIndex) ReindexAllWithProgress(projects []ReindexProject, versions []ReindexVersion, progressFn ReindexProgressFunc) error {
	// Delete all existing documents
	q := bleve.NewMatchAllQuery()
	req := bleve.NewSearchRequest(q)
	req.Size = 10000
	req.Fields = []string{}

	results, err := si.index.Search(req)
	if err == nil {
		batch := si.index.NewBatch()
		for _, hit := range results.Hits {
			batch.Delete(hit.ID)
		}
		si.index.Batch(batch)
	}

	projectMap := make(map[int64]ReindexProject)
	for _, p := range projects {
		projectMap[p.ID] = p
	}

	total := len(versions)
	for i, v := range versions {
		p, ok := projectMap[v.ProjectID]
		if !ok {
			continue
		}

		if progressFn != nil {
			progressFn(ReindexProgress{
				Current: i + 1,
				Total:   total,
				Project: p.Slug,
				Version: v.Tag,
			})
		}

		si.IndexVersion(p.ID, v.ID, p.Slug, p.Name, v.Tag, v.StoragePath)
	}

	return nil
}

func fieldString(fields map[string]interface{}, key string) string {
	val, ok := fields[key]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}
