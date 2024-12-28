// internal/app/download.go
package app

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bradsec/gofindmyfonts/internal/logging"
)

func (s *Server) handleDownloadAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the JSON request containing font URLs
	var request struct {
		Fonts []struct {
			Name    string            `json:"name"`
			Formats map[string]string `json:"formats"`
		} `json:"fonts"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		logging.Error("Failed to decode request", "download_all", "", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Create temporary directory for zip creation
	tempDir, err := os.MkdirTemp("", "fontdownload-*")
	if err != nil {
		logging.Error("Failed to create temp directory", "download_all", "", err)
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	// Create zip file with a unique filename
	timestamp := time.Now().Format("20060102-150405.000")
	zipPath := filepath.Join(tempDir, fmt.Sprintf("fonts-%s.zip", timestamp))
	zipFile, err := os.Create(zipPath)
	if err != nil {
		logging.Error("Failed to create zip file", "download_all", zipPath, err)
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}
	defer zipFile.Close()

	// Check for existing files in the zip before adding
	existingFiles := make(map[string]bool)
	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Process each font
	for _, font := range request.Fonts {
		for format, encodedPath := range font.Formats {
			// Extract the actual path from the download URL
			u, err := url.Parse(encodedPath)
			if err != nil {
				logging.Error("Failed to parse URL", "download_all", encodedPath, err)
				continue
			}

			// Extract the path parameter
			pathParam := u.Query().Get("path")
			if pathParam == "" {
				logging.Error("No path parameter found", "download_all", encodedPath, fmt.Errorf("empty path"))
				continue
			}

			// Decode the path
			decodedPath, err := url.QueryUnescape(pathParam)
			if err != nil {
				logging.Error("Failed to decode path", "download_all", pathParam, err)
				continue
			}

			// Attempt to handle both original and converted paths
			potentialPaths := []string{
				decodedPath,                     // Original decoded path
				filepath.Clean(decodedPath),     // Cleaned path
				filepath.Join(".", decodedPath), // Relative to current directory
				filepath.Join(s.config.StaticDir, filepath.Base(decodedPath)),              // In static directory
				filepath.Join(s.config.StaticDir, "converted", filepath.Base(decodedPath)), // In converted directory
			}

			var fontFile *os.File
			var foundPath string
			for _, path := range potentialPaths {
				logging.Info("Trying path", "download_all", path)
				if file, err := os.Open(path); err == nil {
					fontFile = file
					foundPath = path
					break
				}
			}

			if fontFile == nil {
				logging.Error("Failed to open font file", "download_all", "Could not find file in any location",
					fmt.Errorf("paths tried: %v", potentialPaths))
				continue
			}
			defer fontFile.Close()

			// Create a clean, unique filename for the zip entry
			sanitizedName := strings.ReplaceAll(font.Name, " ", "_")
			zipEntryName := fmt.Sprintf("%s%s", sanitizedName, format)

			// Check if the file already exists in the zip
			if existingFiles[zipEntryName] {
				continue
			}
			existingFiles[zipEntryName] = true

			// Create zip entry
			zipEntry, err := zipWriter.Create(zipEntryName)
			if err != nil {
				logging.Error("Failed to create zip entry", "download_all", zipEntryName, err)
				continue
			}

			// Copy font file to zip
			if _, err := io.Copy(zipEntry, fontFile); err != nil {
				logging.Error("Failed to copy font to zip", "download_all", zipEntryName, err)
				continue
			}

			logging.Info("Added to zip", "download_all", fmt.Sprintf("File: %s, Entry: %s", foundPath, zipEntryName))
		}
	}

	// Close the zip writer before sending
	zipWriter.Close()

	// Read the zip file
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		logging.Error("Failed to read zip file", "download_all", zipPath, err)
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="fonts-%s.zip"`, timestamp))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(zipData)))

	// Send the zip file
	if _, err := w.Write(zipData); err != nil {
		logging.Error("Failed to send zip file", "download_all", "", err)
	}
}
