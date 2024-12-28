// internal/app/security.go
package app

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bradsec/gofindmyfonts/internal/logging"
)

// Map of allowed file extensions
var allowedExts = map[string]bool{
	".ttf":   true,
	".otf":   true,
	".woff":  true,
	".woff2": true,
}

// isPathAllowed performs basic safety checks on a file path
func isPathAllowed(path string) bool {
	// Remove any URL query parameters and URL-encoded characters
	path = strings.Split(path, "?")[0]
	path, _ = url.QueryUnescape(path)

	// Clean the path
	path = filepath.Clean(path)

	// Prevent directory traversal
	if strings.Contains(path, "..") {
		logging.Error("Potential directory traversal attempt", "security_check", path, fmt.Errorf("path contains '..'"))
		return false
	}

	// Check path base to ensure it's a font file
	fileName := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(fileName))
	if !allowedExts[ext] {
		logging.Error("Invalid file extension", "security_check", path,
			fmt.Errorf("extension %s not allowed", ext))
		return false
	}

	return true
}

// isRootPath checks if a path is a root directory
func isRootPath(path string) bool {
	cleanPath := filepath.Clean(path)

	if runtime.GOOS == "windows" {
		// Check for Windows root paths like "C:", "C:\", etc.
		if len(cleanPath) <= 3 && strings.HasSuffix(cleanPath, ":") || strings.HasSuffix(cleanPath, `:\`) {
			return true
		}
	} else {
		// Check for Unix root path "/"
		if cleanPath == "/" {
			return true
		}
	}
	return false
}

// ValidateFontDirectory performs basic checks on a directory
func ValidateFontDirectory(dir string) error {
	// Check for empty path
	if dir == "" {
		logging.Error("Empty directory path provided", "validate_dir", dir, fmt.Errorf("empty path"))
		return fmt.Errorf("please specify a directory path")
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(dir)
	if err != nil {
		logging.Error("Failed to get absolute path for directory", "validate_dir", dir, err)
		return fmt.Errorf("invalid directory path: %v", err)
	}

	// Check if it's a root path
	if isRootPath(absPath) {
		logging.Error("Root directory specified", "validate_dir", dir, fmt.Errorf("root directory not allowed"))
		return fmt.Errorf("root directory paths (e.g., '/', 'C:\\') are not allowed")
	}

	// Check path length - prevent empty subdirectories
	if len(strings.TrimSpace(filepath.Base(absPath))) == 0 {
		logging.Error("Invalid directory name", "validate_dir", dir, fmt.Errorf("invalid directory name"))
		return fmt.Errorf("invalid directory name")
	}

	// Check if directory exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			logging.Error("Directory does not exist", "validate_dir", dir, err)
			return fmt.Errorf("directory does not exist")
		}
		logging.Error("Error accessing directory", "validate_dir", dir, err)
		return fmt.Errorf("error accessing directory: %v", err)
	}

	// Verify it's a directory
	if !info.IsDir() {
		logging.Error("Path is not a directory", "validate_dir", dir, fmt.Errorf("not a directory"))
		return fmt.Errorf("specified path is not a directory")
	}

	// Check if directory is readable
	file, err := os.Open(absPath)
	if err != nil {
		logging.Error("Directory is not readable", "validate_dir", dir, err)
		return fmt.Errorf("directory is not accessible: %v", err)
	}
	file.Close()

	logging.Info("Directory validation passed", "validate_dir", dir)
	return nil
}
