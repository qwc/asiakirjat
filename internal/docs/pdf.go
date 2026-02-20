package docs

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	gopdf "github.com/ledongthuc/pdf"
)

var (
	hasPdftotext     bool
	pdftotextChecked sync.Once
)

func checkPdftotext() {
	_, err := exec.LookPath("pdftotext")
	hasPdftotext = err == nil
}

// PDFPage holds the text content for a single PDF page.
type PDFPage struct {
	Number int
	Text   string
}

// ExtractPDFPages extracts text from a PDF file, returning one PDFPage per page.
// Tries pdftotext (poppler-utils) first for best quality, falls back to
// ledongthuc/pdf (pure Go) if pdftotext is not installed.
func ExtractPDFPages(filePath string) (title string, pages []PDFPage, err error) {
	pdftotextChecked.Do(checkPdftotext)

	if hasPdftotext {
		title, pages, err = extractPagesWithPdftotext(filePath)
		if err == nil {
			return title, pages, nil
		}
		// Fall through to pure Go on error
	}

	return extractPagesWithGoPDF(filePath)
}

func extractPagesWithPdftotext(filePath string) (string, []PDFPage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", filePath, "-")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", nil, err
	}

	text := out.String()
	// pdftotext separates pages with form-feed (\f)
	rawPages := strings.Split(text, "\f")

	var pages []PDFPage
	for i, pageText := range rawPages {
		trimmed := strings.TrimSpace(pageText)
		if trimmed == "" {
			continue
		}
		pages = append(pages, PDFPage{
			Number: i + 1,
			Text:   trimmed,
		})
	}

	title := ""
	if len(pages) > 0 {
		title = firstNonEmptyLine(pages[0].Text)
	}
	return title, pages, nil
}

func extractPagesWithGoPDF(filePath string) (string, []PDFPage, error) {
	f, reader, err := gopdf.Open(filePath)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()

	var pages []PDFPage
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		pageText, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		trimmed := strings.TrimSpace(pageText)
		if trimmed == "" {
			continue
		}
		pages = append(pages, PDFPage{
			Number: i,
			Text:   trimmed,
		})
	}

	title := ""
	if len(pages) > 0 {
		title = firstNonEmptyLine(pages[0].Text)
	}
	return title, pages, nil
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
