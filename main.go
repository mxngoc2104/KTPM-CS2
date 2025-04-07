package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"imageprocessor/pkg/ocr"
	"imageprocessor/pkg/pdf"
	"imageprocessor/pkg/translator"
)

func main() {
	// Check if image path is provided
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <image_path>")
		fmt.Println("Using default image: data/sample.png")
		// Use default image
		processImage("data/sample.png")
	} else {
		// Use provided image path
		processImage(os.Args[1])
	}
}

func processImage(imagePath string) {
	// Ensure the output directory exists
	outputDir := "output"
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		os.Mkdir(outputDir, 0755)
	}

	// Step 1: OCR - Convert image to text
	fmt.Println("Step 1: Converting image to text...")
	text, err := ocr.ImageToText(imagePath)
	if err != nil {
		log.Fatalf("OCR error: %v", err)
	}
	fmt.Println("Extracted text:")
	fmt.Println("-------------------")
	fmt.Println(text)
	fmt.Println("-------------------")

	// Step 2: Translate text from English to Vietnamese
	fmt.Println("\nStep 2: Translating text to Vietnamese...")
	translatedText, err := translator.Translate(text)
	if err != nil {
		log.Fatalf("Translation error: %v", err)
	}
	fmt.Println("Translated text:")
	fmt.Println("-------------------")
	fmt.Println(translatedText)
	fmt.Println("-------------------")

	// Step 3: Generate PDF with the translated text
	fmt.Println("\nStep 3: Creating PDF with translated text...")
	pdfPath, err := pdf.CreatePDF(translatedText)
	if err != nil {
		log.Fatalf("PDF creation error: %v", err)
	}

	absPath, _ := filepath.Abs(pdfPath)
	fmt.Printf("\nProcess completed successfully!\nOutput PDF: %s\n", absPath)
}