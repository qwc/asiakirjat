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

// ExtractTextFromPDF extracts text from a PDF file for search indexing.
// Tries pdftotext (poppler-utils) first for best quality, falls back to
// ledongthuc/pdf (pure Go) if pdftotext is not installed.
func ExtractTextFromPDF(filePath string) (title string, text string, err error) {
	pdftotextChecked.Do(checkPdftotext)

	if hasPdftotext {
		title, text, err = extractWithPdftotext(filePath)
		if err == nil {
			return title, text, nil
		}
		// Fall through to pure Go on error
	}

	return extractWithGoPDF(filePath)
}

func extractWithPdftotext(filePath string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", filePath, "-")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", "", err
	}

	text := out.String()
	title := firstNonEmptyLine(text)
	return title, text, nil
}

func extractWithGoPDF(filePath string) (string, string, error) {
	f, reader, err := gopdf.Open(filePath)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		pageText, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(pageText)
		buf.WriteByte('\n')
	}

	text := buf.String()
	title := firstNonEmptyLine(text)
	return title, text, nil
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
