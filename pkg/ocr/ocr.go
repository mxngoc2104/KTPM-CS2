package ocr

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ImageToText converts an image to text using Tesseract OCR
func ImageToText(imagePath string) (string, error) {
	// Find the full path to the tesseract executable Go is using
	tesseractPath, err := exec.LookPath("tesseract")
	if err != nil {
		return "", fmt.Errorf("tesseract executable not found in PATH: %w", err)
	}
	log.Printf("OCR: Using tesseract at: %s", tesseractPath)

	// Tạo tên file output tạm thời (không bao gồm .txt)
	ext := filepath.Ext(imagePath)
	baseName := strings.TrimSuffix(imagePath, ext)
	tempOutputFileBase := baseName + "_ocr_temp"
	tempOutputFilePath := tempOutputFileBase + ".txt" // Tên file Tesseract sẽ tạo

	// Xóa file output cũ nếu tồn tại (phòng trường hợp lần chạy trước lỗi)
	os.Remove(tempOutputFilePath)

	// Lệnh Tesseract: output vào file tạm, dùng PSM mặc định
	cmd := exec.Command(tesseractPath, imagePath, tempOutputFileBase, "-l", "eng")
	log.Printf("OCR: Executing command: %s", cmd.String())

	// Chạy lệnh và lấy lỗi (bao gồm cả stderr nếu có)
	outputBytes, err := cmd.CombinedOutput() // Dùng CombinedOutput để vẫn thấy stderr nếu lỗi
	if err != nil {
		// Ghi log lỗi chi tiết bao gồm cả output (thường chứa stderr)
		log.Printf("OCR: Tesseract command failed for image %s. Error: %v, Output: %s", imagePath, err, string(outputBytes))
		return "", fmt.Errorf("tesseract command failed: %w. Output: %s", err, string(outputBytes))
	}

	// Đọc nội dung từ file output .txt
	ocrBytes, err := os.ReadFile(tempOutputFilePath)
	if err != nil {
		log.Printf("OCR: Failed to read Tesseract output file %s: %v", tempOutputFilePath, err)
		return "", fmt.Errorf("failed to read tesseract output file: %w", err)
	}

	// Xóa file .txt tạm thời
	defer os.Remove(tempOutputFilePath)

	// Trim whitespace and return
	return strings.TrimSpace(string(ocrBytes)), nil
}
