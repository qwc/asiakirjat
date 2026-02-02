package docs

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ServeDoc serves a documentation file from the storage path.
// If the path points to a directory, it serves index.html from that directory.
func ServeDoc(w http.ResponseWriter, r *http.Request, storagePath, filePath string) {
	fullPath := filepath.Join(storagePath, filepath.Clean(filePath))

	// Security: ensure the resolved path is within the storage path
	absStorage, err := filepath.Abs(storagePath)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	absFile, err := filepath.Abs(fullPath)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if !strings.HasPrefix(absFile, absStorage) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If directory, serve index.html
	if info.IsDir() {
		indexPath := filepath.Join(fullPath, "index.html")
		if _, err := os.Stat(indexPath); err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		fullPath = indexPath
	}

	http.ServeFile(w, r, fullPath)
}
