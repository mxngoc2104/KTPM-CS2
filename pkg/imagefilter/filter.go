package imagefilter

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anthonynsimon/bild/effect"
	"github.com/anthonynsimon/bild/imgio"
	// "github.com/anthonynsimon/bild/transform"
	// "github.com/anthonynsimon/bild/blur"
)

// ApplyFilters applies pre-processing filters using the bild library.
// Implements ONLY Grayscale conversion.
// Returns the path to the filtered grayscale image.
func ApplyFilters(imagePath string) (string, error) {
	fmt.Printf("Applying bild Grayscale filter ONLY to: %s\n", imagePath)

	// Mở ảnh gốc sử dụng bild
	srcImage, err := imgio.Open(imagePath)
	if err != nil {
		return "", fmt.Errorf("bild: failed to open image %s: %w", imagePath, err)
	}

	// 1. Chuyển sang ảnh xám
	grayImage := effect.Grayscale(srcImage)

	// Bỏ qua các bước khác

	// Tạo đường dẫn cho file output
	ext := filepath.Ext(imagePath)
	baseName := strings.TrimSuffix(imagePath, ext)
	// Đổi hậu tố
	filteredImagePath := fmt.Sprintf("%s_gray%s", baseName, ext) // Chỉ gray

	// Lưu ảnh đã xử lý (ảnh xám)
	encoder := imgio.PNGEncoder()
	if err := imgio.Save(filteredImagePath, grayImage, encoder); err != nil { // Lưu grayImage
		return "", fmt.Errorf("bild: failed to save grayscale image %s: %w", filteredImagePath, err)
	}

	fmt.Printf("Saved Grayscale image to: %s\n", filteredImagePath)
	return filteredImagePath, nil
}
