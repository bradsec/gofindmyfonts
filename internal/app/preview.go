// internal/app/preview.go
package app

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bradsec/gofindmyfonts/internal/logging"
)

const (
	progressBufferSize = 10 // Smaller buffer size for better backpressure handling
)

// FontProcessError represents a custom error type for font processing operations
type FontProcessError struct {
	Op   string // Operation being performed
	Path string // File path if applicable
	Err  error  // Underlying error
}

func (e *FontProcessError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s failed for %s: %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("%s failed: %v", e.Op, e.Err)
}

// FontPreview represents a font and its preview information
type FontPreview struct {
	Name    string            `json:"name"`
	Preview string            `json:"preview"`
	Formats map[string]string `json:"formats"`
}

// FontVariant represents a font with its different format variations
type FontVariant struct {
	Name        string
	Location    map[string]string // Map of extension -> path
	PreviewPath string            // Path to WOFF2/WOFF preview file
}

// ConversionProgress represents the progress of font conversion
type ConversionProgress struct {
	Total       int    `json:"total"`
	Current     int    `json:"current"`
	CurrentFont string `json:"currentFont"`
	Stage       string `json:"stage"`
}

// ConversionJob represents a single font conversion job
type ConversionJob struct {
	variant      *FontVariant
	sourceFile   string
	sourceFormat string
	outputPath   string
}

// PreviewGenerator handles font preview generation
type PreviewGenerator struct {
	ctx          context.Context
	cancel       context.CancelFunc
	config       *Config
	previewCache sync.Map
	workerPool   chan struct{}
	progress     chan string
}

// NewPreviewGenerator creates a new PreviewGenerator instance
func NewPreviewGenerator(config *Config) *PreviewGenerator {
	ctx, cancel := context.WithCancel(context.Background())
	return &PreviewGenerator{
		ctx:          ctx,
		cancel:       cancel,
		config:       config,
		workerPool:   make(chan struct{}, config.MaxConcurrent),
		previewCache: sync.Map{},
		progress:     make(chan string, progressBufferSize),
	}
}

// GetProgressChan returns the progress channel
func (pg *PreviewGenerator) GetProgressChan() chan string {
	return pg.progress
}

// Close cleans up resources used by the generator
func (pg *PreviewGenerator) Close() {
	pg.cancel() // Cancel any ongoing operations
	close(pg.progress)
	// Clear the cache
	pg.previewCache.Range(func(key, value interface{}) bool {
		pg.previewCache.Delete(key)
		return true
	})
}

// sendProgress sends a progress update message in a non-blocking way
func (pg *PreviewGenerator) sendProgress(msg string) {
	select {
	case pg.progress <- msg:
		logging.Info("Progress update sent", "progress", msg)
	case <-pg.ctx.Done():
		// Context cancelled, stop sending progress
		return
	default:
		logging.Info("Progress update dropped", "progress", msg)
	}
}

func ensureConvertedDir(config *Config) error {
	convertedDir := filepath.Join(config.StaticDir, "converted")
	if err := os.MkdirAll(convertedDir, 0755); err != nil {
		logging.Error("Failed to create converted directory", "ensure_dir", convertedDir, err)
		return fmt.Errorf("failed to create converted directory: %w", err)
	}
	logging.Info("Ensured converted directory exists", "ensure_dir", convertedDir)
	return nil
}

func decodeFilePath(encodedPath string) (string, error) {
	path := strings.TrimPrefix(encodedPath, "/download?path=")
	decodedPath, err := url.QueryUnescape(path)
	if err != nil {
		logging.Error("Failed to decode file path", "decode_path", encodedPath, err)
		return "", fmt.Errorf("failed to decode file path: %w", err)
	}
	logging.Info("Successfully decoded file path", "decode_path", decodedPath)
	return decodedPath, nil
}

// copyFile safely copies a file from src to dst
func copyFile(src, dst string) error {
	logging.Info("Copying file", "copy_file", fmt.Sprintf("src: %s, dst: %s", src, dst))

	sourceFile, err := os.Open(src)
	if err != nil {
		logging.Error("Failed to open source file", "copy_file", src, err)
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		logging.Error("Failed to create destination directory", "copy_file", dst, err)
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destFile, err := os.Create(dst)
	if err != nil {
		logging.Error("Failed to create destination file", "copy_file", dst, err)
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	if _, err = io.Copy(destFile, sourceFile); err != nil {
		logging.Error("Failed to copy file content", "copy_file", dst, err)
		return fmt.Errorf("failed to copy file: %w", err)
	}

	logging.Info("Successfully copied file", "copy_file", dst)
	return destFile.Sync()
}

// convertToWoff2 converts a TTF/OTF file to WOFF2 format
func convertToWoff2(ttfPath string, outputPath string) (string, error) {
	logging.Info("Starting WOFF2 conversion", "convert_woff2", fmt.Sprintf("from: %s to: %s", ttfPath, outputPath))

	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		logging.Error("Failed to create output directory", "convert_woff2", outputDir, err)
		return "", &FontProcessError{Op: "create_dir", Path: outputDir, Err: err}
	}

	// Check if output already exists and is valid
	if info, err := os.Stat(outputPath); err == nil && info.Size() > 0 {
		logging.Info("WOFF2 file already exists", "convert_woff2", outputPath)
		return outputPath, nil
	}

	// Create temporary file for conversion
	tmpFile := strings.TrimSuffix(outputPath, ".woff2") + filepath.Ext(ttfPath)
	if err := copyFile(ttfPath, tmpFile); err != nil {
		logging.Error("Failed to create temporary file", "convert_woff2", tmpFile, err)
		return "", &FontProcessError{Op: "copy", Path: ttfPath, Err: err}
	}
	defer os.Remove(tmpFile)

	logging.Info("Running woff2_compress", "convert_woff2", tmpFile)
	cmd := exec.Command("woff2_compress", filepath.Base(tmpFile))
	cmd.Dir = outputDir

	if output, err := cmd.CombinedOutput(); err != nil {
		logging.Error("WOFF2 compression failed", "convert_woff2", tmpFile, fmt.Errorf("%v: %s", err, string(output)))
		return "", &FontProcessError{
			Op:   "woff2_compress",
			Path: tmpFile,
			Err:  fmt.Errorf("compression failed: %v, output: %s", err, string(output)),
		}
	}

	// Verify output file was created and is not empty
	if info, err := os.Stat(outputPath); err != nil || info.Size() == 0 {
		logging.Error("WOFF2 output verification failed", "convert_woff2", outputPath, fmt.Errorf("file not created or empty"))
		return "", &FontProcessError{
			Op:   "verify",
			Path: outputPath,
			Err:  fmt.Errorf("file not created or empty after compression"),
		}
	}

	logging.Info("Successfully converted to WOFF2", "convert_woff2", outputPath)
	return outputPath, nil
}

// convertToTTF converts a WOFF2 file to TTF format
func convertToTTF(woff2Path string, outputPath string) error {
	logging.Info("Starting TTF conversion", "convert_ttf", fmt.Sprintf("from: %s to: %s", woff2Path, outputPath))

	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		logging.Error("Failed to create output directory", "convert_ttf", outputDir, err)
		return &FontProcessError{Op: "create_dir", Path: outputDir, Err: err}
	}

	// Check if output already exists and is valid
	if info, err := os.Stat(outputPath); err == nil && info.Size() > 0 {
		logging.Info("TTF file already exists", "convert_ttf", outputPath)
		return nil
	}

	// Verify source file exists
	if _, err := os.Stat(woff2Path); err != nil {
		logging.Error("Source file not found", "convert_ttf", woff2Path, err)
		return &FontProcessError{
			Op:   "check_source",
			Path: woff2Path,
			Err:  fmt.Errorf("file not found or not accessible: %w", err),
		}
	}

	// Create temporary file for conversion
	tmpFile := filepath.Join(outputDir, filepath.Base(woff2Path))
	if err := copyFile(woff2Path, tmpFile); err != nil {
		logging.Error("Failed to create temporary file", "convert_ttf", tmpFile, err)
		return &FontProcessError{Op: "copy", Path: woff2Path, Err: err}
	}
	defer func() {
		if err := os.Remove(tmpFile); err != nil {
			logging.Error("Failed to remove temporary file", "convert_ttf", tmpFile, err)
		}
	}()

	logging.Info("Running woff2_decompress", "convert_ttf", tmpFile)
	cmd := exec.Command("woff2_decompress", filepath.Base(tmpFile))
	cmd.Dir = outputDir

	if output, err := cmd.CombinedOutput(); err != nil {
		logging.Error("TTF decompression failed", "convert_ttf", tmpFile, fmt.Errorf("%v: %s", err, string(output)))
		return &FontProcessError{
			Op:   "woff2_decompress",
			Path: tmpFile,
			Err:  fmt.Errorf("decompression failed: %v, output: %s", err, string(output)),
		}
	}

	// Verify output file was created and is not empty
	if info, err := os.Stat(outputPath); err != nil || info.Size() == 0 {
		logging.Error("TTF output verification failed", "convert_ttf", outputPath, fmt.Errorf("file not created or empty"))
		return &FontProcessError{
			Op:   "verify",
			Path: outputPath,
			Err:  fmt.Errorf("file not created or empty after decompression"),
		}
	}

	logging.Info("Successfully converted to TTF", "convert_ttf", outputPath)
	return nil
}

// findFonts finds all font files in a directory and groups them by base name
func findFonts(root string) (map[string]*FontVariant, error) {
	logging.Info("Starting font search", "find_fonts", root)

	fonts := make(map[string]*FontVariant)
	var mu sync.Mutex
	var walkErr error

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				logging.Error("Permission denied", "find_fonts", path, err)
				return filepath.SkipDir
			}
			if walkErr == nil {
				walkErr = &FontProcessError{
					Op:   "access",
					Path: path,
					Err:  err,
				}
			}
			return nil
		}

		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".ttf" || ext == ".otf" || ext == ".woff" || ext == ".woff2" {
				baseName := strings.TrimSuffix(filepath.Base(path), ext)

				mu.Lock()
				if _, exists := fonts[baseName]; !exists {
					fonts[baseName] = &FontVariant{
						Name:     baseName,
						Location: make(map[string]string),
					}
				}

				downloadURL := "/download?path=" + url.QueryEscape(path)
				fonts[baseName].Location[ext] = downloadURL

				if ext == ".woff2" ||
					(ext == ".woff" && fonts[baseName].PreviewPath == "") ||
					((ext == ".ttf" || ext == ".otf") && fonts[baseName].PreviewPath == "") {
					fonts[baseName].PreviewPath = downloadURL
				}
				mu.Unlock()

				logging.Info(fmt.Sprintf("Found font: %s (%s)", baseName, ext), "find_fonts", path)
			}
		}
		return nil
	})

	if walkErr != nil {
		logging.Error("Error during directory walk", "find_fonts", root, walkErr)
		return nil, walkErr
	}

	if err != nil {
		logging.Error("Error walking directory", "find_fonts", root, err)
		return nil, &FontProcessError{
			Op:   "walk",
			Path: root,
			Err:  fmt.Errorf("error walking directory: %w", err),
		}
	}

	if len(fonts) == 0 {
		logging.Error("No fonts found", "find_fonts", root, fmt.Errorf("no font files found"))
		return nil, &FontProcessError{
			Op:   "scan",
			Path: root,
			Err:  fmt.Errorf("no font files found in directory"),
		}
	}

	logging.Info(fmt.Sprintf("Found %d fonts", len(fonts)), "find_fonts", root)
	return fonts, nil
}

func (pg *PreviewGenerator) processConversions(
	jobs []ConversionJob,
	progress chan<- ConversionProgress,
	converter func(string, string) (string, error),
) {
	totalJobs := len(jobs)
	var completed int32

	logging.Info(fmt.Sprintf("Starting conversion batch: %d jobs", totalJobs), "process_conversions", "")

	jobsChan := make(chan ConversionJob, totalJobs)
	for _, job := range jobs {
		jobsChan <- job
	}
	close(jobsChan)

	// Calculate optimal number of workers
	numWorkers := runtime.NumCPU() / 2
	if numWorkers < 1 {
		numWorkers = 1
	}
	if numWorkers > pg.config.MaxConcurrent {
		numWorkers = pg.config.MaxConcurrent
	}

	logging.Info(fmt.Sprintf("Starting %d worker(s)", numWorkers), "process_conversions", "")

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobsChan {
				select {
				case <-pg.ctx.Done():
					logging.Info("Conversion cancelled", "process_conversions", job.variant.Name)
					return // Context cancelled, stop processing
				default:
				}

				conversionType := "WOFF2"
				if strings.HasSuffix(job.outputPath, ".ttf") {
					conversionType = "TTF"
				}

				logging.Info(fmt.Sprintf("Processing %s conversion", conversionType), "process_conversions", job.variant.Name)

				convertedPath, err := converter(job.sourceFile, job.outputPath)
				if err == nil {
					ext := filepath.Ext(job.outputPath)
					downloadURL := fmt.Sprintf("/download?path=%s&filename=%s%s",
						url.QueryEscape(convertedPath),
						url.QueryEscape(job.variant.Name),
						ext)

					job.variant.Location[ext] = downloadURL
					if ext == ".woff2" && job.variant.PreviewPath == "" {
						job.variant.PreviewPath = downloadURL
					}
					logging.Info(fmt.Sprintf("Successfully created %s version", conversionType), "process_conversions", job.variant.Name)
				} else {
					logging.Error(fmt.Sprintf("Error converting to %s", conversionType), "process_conversions", job.variant.Name, err)
				}

				current := atomic.AddInt32(&completed, 1)
				select {
				case progress <- ConversionProgress{
					Total:       totalJobs,
					Current:     int(current),
					CurrentFont: job.variant.Name,
					Stage:       fmt.Sprintf("Converting to %s", conversionType),
				}:
					logging.Info(fmt.Sprintf("Progress update: %d/%d", current, totalJobs), "process_conversions", job.variant.Name)
				case <-pg.ctx.Done():
					return
				default:
					logging.Info(fmt.Sprintf("Progress update skipped: %d/%d", current, totalJobs), "process_conversions", job.variant.Name)
				}
			}
		}()
	}

	wg.Wait()
	logging.Info("Conversion batch completed", "process_conversions", "")
}

// ProcessFonts processes all fonts in the given directory
func (pg *PreviewGenerator) ProcessFonts(fontDir string) ([]FontPreview, error) {
	logging.Info("Starting font processing", "process_fonts", fontDir)

	// Validate directory exists and is accessible
	if info, err := os.Stat(fontDir); err != nil {
		if os.IsNotExist(err) {
			logging.Error("Directory does not exist", "process_fonts", fontDir, err)
			return nil, &FontProcessError{Op: "validate", Path: fontDir, Err: fmt.Errorf("directory does not exist")}
		}
		logging.Error("Error accessing directory", "process_fonts", fontDir, err)
		return nil, &FontProcessError{Op: "validate", Path: fontDir, Err: err}
	} else if !info.IsDir() {
		logging.Error("Path is not a directory", "process_fonts", fontDir, fmt.Errorf("not a directory"))
		return nil, &FontProcessError{Op: "validate", Path: fontDir, Err: fmt.Errorf("path is not a directory")}
	}

	// Ensure directories exist
	if err := ensureConvertedDir(pg.config); err != nil {
		logging.Error("Failed to create directories", "process_fonts", fontDir, err)
		return nil, &FontProcessError{Op: "create_dirs", Err: err}
	}

	pg.sendProgress("Starting font processing...")
	pg.sendProgress("Scanning font directory...")

	// Find fonts
	fontVariants, err := findFonts(fontDir)
	if err != nil {
		logging.Error("Error finding fonts", "process_fonts", fontDir, err)
		return nil, &FontProcessError{Op: "scan", Path: fontDir, Err: err}
	}

	pg.sendProgress(fmt.Sprintf("Found %d fonts. Preparing for conversion...", len(fontVariants)))

	// Prepare conversion jobs
	var woff2Jobs []ConversionJob
	var ttfJobs []ConversionJob

	for _, variant := range fontVariants {
		select {
		case <-pg.ctx.Done():
			logging.Info("Processing cancelled", "process_fonts", fontDir)
			return nil, &FontProcessError{Op: "process", Err: fmt.Errorf("operation cancelled")}
		default:
		}

		if _, hasWoff2 := variant.Location[".woff2"]; !hasWoff2 {
			if ttfPath, hasTTF := variant.Location[".ttf"]; hasTTF {
				if decodedPath, err := decodeFilePath(ttfPath); err == nil {
					woff2Jobs = append(woff2Jobs, ConversionJob{
						variant:      variant,
						sourceFile:   decodedPath,
						sourceFormat: ".ttf",
						outputPath:   filepath.Join(pg.config.StaticDir, "converted", variant.Name+".woff2"),
					})
				}
			} else if otfPath, hasOTF := variant.Location[".otf"]; hasOTF {
				if decodedPath, err := decodeFilePath(otfPath); err == nil {
					woff2Jobs = append(woff2Jobs, ConversionJob{
						variant:      variant,
						sourceFile:   decodedPath,
						sourceFormat: ".otf",
						outputPath:   filepath.Join(pg.config.StaticDir, "converted", variant.Name+".woff2"),
					})
				}
			}
		}

		if woff2Path, hasWoff2 := variant.Location[".woff2"]; hasWoff2 {
			if _, hasTTF := variant.Location[".ttf"]; !hasTTF {
				if decodedPath, err := decodeFilePath(woff2Path); err == nil {
					ttfJobs = append(ttfJobs, ConversionJob{
						variant:      variant,
						sourceFile:   decodedPath,
						sourceFormat: ".woff2",
						outputPath:   filepath.Join(pg.config.StaticDir, "converted", variant.Name+".ttf"),
					})
				}
			}
		}
	}

	progressChan := make(chan ConversionProgress, progressBufferSize)
	done := make(chan struct{})

	// Progress forwarder
	go func() {
		defer close(done)
		for {
			select {
			case progress, ok := <-progressChan:
				if !ok {
					return
				}
				var message string
				if progress.Stage == "Converting to WOFF2" {
					message = fmt.Sprintf("WOFF2 conversion: %d/%d - Processing: %s",
						progress.Current, progress.Total, progress.CurrentFont)
				} else if progress.Stage == "Converting to TTF" {
					message = fmt.Sprintf("TTF conversion: %d/%d - Processing: %s",
						progress.Current, progress.Total, progress.CurrentFont)
				} else {
					message = fmt.Sprintf("%s: %d/%d - Current: %s",
						progress.Stage, progress.Current, progress.Total, progress.CurrentFont)
				}
				pg.sendProgress(message)
			case <-pg.ctx.Done():
				return
			}
		}
	}()

	// Process WOFF2 conversions
	if len(woff2Jobs) > 0 {
		logging.Info(fmt.Sprintf("Starting WOFF2 conversions (%d files)", len(woff2Jobs)), "process_fonts", fontDir)
		pg.sendProgress(fmt.Sprintf("Starting WOFF2 conversions (%d files)...", len(woff2Jobs)))
		pg.processConversions(woff2Jobs, progressChan, convertToWoff2)
	}

	// Process TTF conversions
	if len(ttfJobs) > 0 {
		logging.Info(fmt.Sprintf("Starting TTF conversions (%d files)", len(ttfJobs)), "process_fonts", fontDir)
		pg.sendProgress(fmt.Sprintf("Starting TTF conversions (%d files)...", len(ttfJobs)))
		pg.processConversions(ttfJobs, progressChan, func(src, dst string) (string, error) {
			err := convertToTTF(src, dst)
			if err != nil {
				return "", err
			}
			return dst, nil
		})
	}

	close(progressChan)
	<-done

	// Final completion message
	pg.sendProgress("All conversions complete! Preparing results...")
	logging.Info("All conversions complete", "process_fonts", fontDir)

	var results []FontPreview
	for _, variant := range fontVariants {
		preview := FontPreview{
			Name:    variant.Name,
			Preview: variant.PreviewPath,
			Formats: variant.Location,
		}
		results = append(results, preview)
	}

	return results, nil
}
