package templates

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var classAttr = regexp.MustCompile(`class="([^"{}]*)"`)

// A class name in a template is only half a promise; the other half is a rule
// in the stylesheet. Issue #157 recoloured the version badges and added
// .version-badge-forever and .btn-download, and a typo in either file would
// leave an unstyled chip that still renders — and still passes every handler
// test, since those assert on the markup. So cross-check the two files.
func TestVersionBadgeAndDownloadClassesAreStyled(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "static", "css", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	sheet := string(css)

	used := map[string]string{} // class -> the template that names it
	err = fs.WalkDir(templateFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range classAttr.FindAllStringSubmatch(string(body), -1) {
			for _, class := range strings.Fields(m[1]) {
				if strings.HasPrefix(class, "version-badge-") || class == "btn-download" {
					used[class] = path
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(used) == 0 {
		t.Fatal("found no badge classes in the templates; the scan is broken, not the CSS")
	}

	for class, path := range used {
		if !strings.Contains(sheet, "."+class+" {") {
			t.Errorf("%s uses .%s, which style.css does not define", path, class)
		}
	}
}
