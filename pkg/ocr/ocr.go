package ocr

import (
	"errors"
	"fmt"
	"imageprocessor/pkg/cache"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	// ErrOCRFailed is returned when OCR processing fails
	ErrOCRFailed = errors.New("OCR processing failed")

	// Cache instance for OCR results
	ocrCache *cache.Cache
)

// OCRConfig holds configuration for OCR processing
type OCRConfig struct {
	CacheTTL        time.Duration // Cache time-to-live
	ApplyPreprocess bool          // Whether to apply preprocessing
	NumThreads      int           // Number of threads to use
	DPI             int           // DPI for processing
}

// DefaultOCRConfig returns a default OCR configuration optimized for most systems
func DefaultOCRConfig() OCRConfig {
	return OCRConfig{
		CacheTTL:        24 * time.Hour, // Cache results for 24 hours
		ApplyPreprocess: true,           // Apply image preprocessing by default
		NumThreads:      runtime.NumCPU(),
		DPI:             300, // Higher DPI for better quality
	}
}

// InitCache initializes the OCR cache
func InitCache(ttl time.Duration) {
	ocrCache = cache.NewCache(ttl)
}

// ImageToText converts an image to text using Tesseract OCR
func ImageToText(imagePath string) (string, error) {
	// Use default config
	return ImageToTextWithConfig(imagePath, DefaultOCRConfig())
}

// ImageToTextWithConfig converts an image to text using Tesseract OCR with custom config
func ImageToTextWithConfig(imagePath string, config OCRConfig) (string, error) {
	// Check if image exists
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		return "", fmt.Errorf("image does not exist: %s", imagePath)
	}

	// Initialize cache if not already initialized
	if ocrCache == nil {
		InitCache(config.CacheTTL)
	}

	// Try to get from cache
	imageHash, err := cache.GetImageHash(imagePath)
	if err != nil {
		log.Printf("Warning: Failed to generate image hash for caching: %v", err)
	} else {
		if text, found := ocrCache.Get(imageHash); found {
			log.Printf("Cache hit for image: %s", imagePath)
			return text, nil
		}
	}

	// If not in cache or couldn't get hash, process the image
	log.Printf("Cache miss for image: %s, processing...", imagePath)

	// Apply preprocessing if enabled
	var processedImagePath string
	if config.ApplyPreprocess {
		processedImagePath, err = preprocessImage(imagePath)
		if err != nil {
			log.Printf("Warning: Image preprocessing failed: %v, using original image", err)
			processedImagePath = imagePath
		} else {
			defer os.Remove(processedImagePath) // Clean up temporary file
		}
	} else {
		processedImagePath = imagePath
	}

	// Set up Tesseract command with optimized parameters
	args := []string{
		processedImagePath,
		"stdout",
		"-l", "eng",
		"--oem", "1", // Use LSTM OCR Engine only
		"--psm", "6", // Assume a single uniform block of text
		"-c", fmt.Sprintf("tessedit_thread_count=%d", config.NumThreads),
		"-c", fmt.Sprintf("tessdit_create_pdf=0"),
		"-c", fmt.Sprintf("tessdit_create_hocr=0"),
		"-c", fmt.Sprintf("tessdit_pageseg_mode=6"),
	}

	// Add DPI parameter if specified
	if config.DPI > 0 {
		args = append(args, "--dpi", fmt.Sprintf("%d", config.DPI))
	}

	cmd := exec.Command("tesseract", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOCRFailed, err)
	}

	// Trim whitespace and process text
	text := strings.TrimSpace(string(output))

	// Store in cache if we have a hash
	if imageHash != "" {
		ocrCache.Set(imageHash, text)
	}

	return text, nil
}

// preprocessImage applies preprocessing filters to improve OCR results
// Returns path to processed image (temporary file)
func preprocessImage(imagePath string) (string, error) {
	// Create temporary file for output
	ext := filepath.Ext(imagePath)
	tempFile, err := os.CreateTemp("", "ocr-preprocess-*"+ext)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempFile.Close()
	tempPath := tempFile.Name()

	// Use ImageMagick for preprocessing - essential filters for OCR optimization
	cmd := exec.Command(
		"convert", imagePath,
		"-respect-parenthesis",
		"\\(",
		"-clone", "0",
		"-colorspace", "gray",
		"-normalize",
		"-blur", "0x1",
		"-contrast-stretch", "0%",
		"\\)",
		"-compose", "over",
		"-composite",
		"-sharpen", "0x1",
		"-black-threshold", "50%",
		"-white-threshold", "50%",
		"-resize", "200%",
		"-define", "png:color-type=0",
		"-define", "png:bit-depth=8",
		tempPath,
	)

	if err := cmd.Run(); err != nil {
		os.Remove(tempPath) // Clean up on error
		return "", fmt.Errorf("image preprocessing failed: %w", err)
	}

	return tempPath, nil
}

// GetCacheSize returns the number of items in the OCR cache
func GetCacheSize() int {
	if ocrCache == nil {
		return 0
	}
	return ocrCache.Size()
}

// ClearCache clears the OCR cache
func ClearCache() {
	if ocrCache != nil {
		ocrCache.Clear()
	}
}
