package ocr

import (
	"errors"
	"fmt"
	"imageprocessor/pkg/cache"
	"io/ioutil"
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
	ocrCache cache.Cache
)

// OCRConfig holds configuration for OCR processing
type OCRConfig struct {
	CacheTTL        time.Duration // Cache time-to-live
	ApplyPreprocess bool          // Whether to apply preprocessing
	NumThreads      int           // Number of threads to use
	DPI             int           // DPI for processing
	UsePythonOCR    bool          // Whether to use Python OCR API
}

// DefaultOCRConfig returns a default OCR configuration optimized for most systems
func DefaultOCRConfig() OCRConfig {
	return OCRConfig{
		CacheTTL:        24 * time.Hour, // Cache results for 24 hours
		ApplyPreprocess: true,           // Apply image preprocessing by default
		NumThreads:      runtime.NumCPU(),
		DPI:             300, // Higher DPI for better quality
		UsePythonOCR:    false,
	}
}

// InitCache initializes the OCR cache with in-memory storage
func InitCache(ttl time.Duration) {
	ocrCache = cache.NewInMemoryCache(ttl)
}

// InitRedisCache initializes the OCR cache with Redis
func InitRedisCache(redisURL string, ttl time.Duration) error {
	var err error
	ocrCache, err = cache.NewRedisCache(redisURL, ttl, "ocr")
	return err
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
		processedImagePath, err = preprocessImageOpenCV(imagePath)
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
		if err := ocrCache.Set(imageHash, text); err != nil {
			log.Printf("Warning: Failed to cache OCR result: %v", err)
		}
	}

	return text, nil
}

// preprocessImageOpenCV applies preprocessing filters using OpenCV
// Returns path to processed image (temporary file)
func preprocessImageOpenCV(imagePath string) (string, error) {
	// Create temporary file for output
	ext := filepath.Ext(imagePath)
	tempFile, err := ioutil.TempFile("", "ocr-preprocess-*"+ext)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempFile.Close()
	tempPath := tempFile.Name()

	// Preprocessing Python script
	pythonScript := `
import cv2
import sys
import numpy as np

# Read image
img = cv2.imread(sys.argv[1])
if img is None:
    sys.exit(1)

# Convert to grayscale
gray = cv2.cvtColor(img, cv2.COLOR_BGR2GRAY)

# Apply Gaussian blur to reduce noise
blur = cv2.GaussianBlur(gray, (5, 5), 0)

# Apply thresholding
_, thresh = cv2.threshold(blur, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)

# Apply adaptive thresholding as a second approach
adaptive_thresh = cv2.adaptiveThreshold(blur, 255, cv2.ADAPTIVE_THRESH_GAUSSIAN_C, 
                                       cv2.THRESH_BINARY, 11, 2)

# Combine both thresholding results
combined = cv2.bitwise_or(thresh, adaptive_thresh)

# Perform dilation to make text clearer
kernel = np.ones((1, 1), np.uint8)
dilated = cv2.dilate(combined, kernel, iterations=1)

# Save the processed image
cv2.imwrite(sys.argv[2], dilated)
`

	// Write script to temp file
	scriptFile, err := ioutil.TempFile("", "ocr-script-*.py")
	if err != nil {
		return "", fmt.Errorf("failed to create temp script file: %w", err)
	}
	defer os.Remove(scriptFile.Name())

	if _, err := scriptFile.WriteString(pythonScript); err != nil {
		return "", fmt.Errorf("failed to write script: %w", err)
	}
	scriptFile.Close()

	// Run Python script
	cmd := exec.Command("python3", scriptFile.Name(), imagePath, tempPath)
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
	size, _ := ocrCache.Size()
	return size
}

// ClearCache clears the OCR cache
func ClearCache() {
	if ocrCache != nil {
		ocrCache.Clear()
	}
}
