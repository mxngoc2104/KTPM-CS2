package ocr

import (
	"os/exec"
	"strings"
)

// ImageToText converts an image to text using Tesseract OCR
func ImageToText(imagePath string) (string, error) {
	// Ensure Tesseract is installed
	// You can check with: tesseract --version
	cmd := exec.Command("tesseract", imagePath, "stdout", "-l", "eng")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	
	// Trim whitespace and return
	return strings.TrimSpace(string(output)), nil
}