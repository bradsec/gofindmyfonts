// internal/app/server.go
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bradsec/gofindmyfonts/internal/logging"
	"github.com/bradsec/gofindmyfonts/internal/templates"
)

// FontError represents a custom error type for font-related operations
type FontError struct {
	Op   string // Operation that failed
	Path string // File path if relevant
	Err  error  // Original error
}

func (e *FontError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s: %s - %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

type Server struct {
	generator *PreviewGenerator
	config    *Config
}

// NewServer creates a new Server instance
func NewServer(generator *PreviewGenerator) *Server {
	return &Server{
		generator: generator,
		config:    LoadConfig(),
	}
}

func (s *Server) Start() error {
	// Set up MIME types
	mime.AddExtensionType(".woff", "font/woff")
	mime.AddExtensionType(".woff2", "font/woff2")
	mime.AddExtensionType(".ttf", "font/ttf")
	mime.AddExtensionType(".otf", "font/otf")

	// Initialize routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/favicon.ico", s.handleFavicon)
	mux.HandleFunc("/generate", s.handleGenerate)
	mux.HandleFunc("/progress", s.handleProgress)
	mux.HandleFunc("/download", s.handleFontDownload)
	mux.HandleFunc("/download-all", s.handleDownloadAll)

	// Serve static files
	fs := http.FileServer(http.Dir(s.config.StaticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Start server with increased timeouts
	addr := ":" + s.config.Port
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	url := fmt.Sprintf("http://localhost%s", addr)
	logging.Info(fmt.Sprintf("Server starting on %s", url), "server_start", "")

	return server.ListenAndServe()
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		logging.Info(fmt.Sprintf("Not found: %s", r.URL.Path), "handle_index", r.URL.Path)
		http.NotFound(w, r)
		return
	}
	if err := templates.RenderIndex(w); err != nil {
		logging.Error("Error rendering index", "handle_index", "", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	if err := templates.ServeFavicon(w); err != nil {
		logging.Error("Error serving favicon", "handle_favicon", "", err)
		http.Error(w, "Favicon not found", http.StatusNotFound)
		return
	}
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		logging.Info(fmt.Sprintf("Invalid method: %s", r.Method), "handle_generate", "")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Method not allowed",
		})
		return
	}

	fontDir := r.URL.Query().Get("fontDir")
	if fontDir == "" {
		logging.Info("Missing font directory in request", "handle_generate", "")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please enter a directory path",
		})
		return
	}

	// Validate directory exists and is accessible
	if err := ValidateFontDirectory(fontDir); err != nil {
		logging.Error("Invalid font directory", "handle_generate", fontDir, err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Invalid directory: %v", err),
		})
		return
	}

	// Process fonts
	previews, err := s.generator.ProcessFonts(fontDir)
	if err != nil {
		logging.Error("Error processing fonts", "handle_generate", fontDir, err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Error processing fonts: %v", err),
		})
		return
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(previews); err != nil {
		logging.Error("Error encoding response", "handle_generate", "", err)
		if !isConnectionClosed(err) {
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Error encoding response",
			})
		}
	}
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		logging.Info("Streaming not supported", "handle_progress", "")
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Get progress channel from generator
	progressChan := s.generator.GetProgressChan()

	// Send initial message
	fmt.Fprintf(w, "data: Initializing progress monitoring...\n\n")
	flusher.Flush()

	// Stream updates to client
	for {
		select {
		case msg, ok := <-progressChan:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleFontDownload(w http.ResponseWriter, r *http.Request) {
	fontPath := r.URL.Query().Get("path")
	if fontPath == "" {
		logging.Info("Download attempted with empty path", "handle_download", "")
		http.Error(w, "No font path specified", http.StatusBadRequest)
		return
	}

	// Clean and validate the path
	fontPath = filepath.Clean(fontPath)
	if !isPathAllowed(fontPath) {
		logging.Info("Access denied to path", "handle_download", fontPath)
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Open the file
	file, err := os.Open(fontPath)
	if err != nil {
		logging.Error("Failed to open font file", "handle_download", fontPath, err)
		http.Error(w, "Font file not found or not accessible", http.StatusNotFound)
		return
	}
	defer file.Close()

	// Get file info
	fileInfo, err := file.Stat()
	if err != nil {
		logging.Error("Error reading font file stats", "handle_download", fontPath, err)
		http.Error(w, "Error reading font file", http.StatusInternalServerError)
		return
	}

	// Set filename for download
	fileName := filepath.Base(fontPath)
	if qFileName := r.URL.Query().Get("filename"); qFileName != "" {
		if decodedName, err := url.QueryUnescape(qFileName); err == nil {
			fileName = decodedName
		}
	}

	// Set headers for download
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	w.Header().Set("Content-Type", getMIMEType(filepath.Ext(fileName)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(s.config.PreviewCacheTime.Seconds())))

	// Stream the file
	if _, err := io.Copy(w, file); err != nil {
		if !isConnectionClosed(err) {
			logging.Error("Error streaming file", "handle_download", fontPath, err)
		}
	}
}

func getMIMEType(ext string) string {
	switch strings.ToLower(ext) {
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}

func isConnectionClosed(err error) bool {
	if err == nil {
		return false
	}
	str := err.Error()
	return strings.Contains(str, "broken pipe") ||
		strings.Contains(str, "reset by peer") ||
		strings.Contains(str, "client disconnected") ||
		strings.Contains(str, "i/o timeout") ||
		strings.Contains(str, "connection refused")
}
