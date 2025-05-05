package pdf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
)

var (
	// ErrPDFCreationFailed is returned when PDF creation fails
	ErrPDFCreationFailed = errors.New("PDF creation failed")
)

// PDFConfig holds configuration for PDF generation
type PDFConfig struct {
	FontName        string  // Font name
	FontSize        float64 // Font size
	LineHeight      float64 // Line height
	MarginLeft      float64 // Left margin in mm
	MarginRight     float64 // Right margin in mm
	MarginTop       float64 // Top margin in mm
	MarginBottom    float64 // Bottom margin in mm
	PageOrientation string  // Page orientation (P for portrait, L for landscape)
	PageSize        string  // Page size (A4, Letter, etc.)
	Title           string  // Document title
	Author          string  // Document author
	CreationDate    string  // Creation date
}

// DefaultPDFConfig returns a default PDF configuration
func DefaultPDFConfig() PDFConfig {
	return PDFConfig{
		FontName:        "Roboto",
		FontSize:        11,
		LineHeight:      6,
		MarginLeft:      15,
		MarginRight:     15,
		MarginTop:       15,
		MarginBottom:    15,
		PageOrientation: "P", // Portrait
		PageSize:        "A4",
		Title:           "Translated Document",
		Author:          "Image Text Processor",
		CreationDate:    time.Now().Format("2006-01-02"),
	}
}

// CreatePDF generates a PDF file with the given text
func CreatePDF(text string) (string, error) {
	return CreatePDFWithConfig(text, DefaultPDFConfig())
}

// CreatePDFWithConfig generates a PDF file with the given text and configuration
func CreatePDFWithConfig(text string, config PDFConfig) (string, error) {
	// Create a new PDF document with UTF-8 encoding
	pdf := gofpdf.New(config.PageOrientation, "mm", config.PageSize, "")

	// Set document properties
	pdf.SetTitle(config.Title, true)
	pdf.SetAuthor(config.Author, true)
	pdf.SetCreator("Image Text Processor", true)

	// Set up font directory
	fontDir := "font"

	// Register the TrueType font for Vietnamese characters
	pdf.SetFontLocation(fontDir)
	pdf.AddUTF8Font(config.FontName, "", "Roboto-Regular.ttf")

	// Add a page
	pdf.AddPage()

	// Set font with UTF-8 encoding
	pdf.SetFont(config.FontName, "", config.FontSize)

	// Enable auto page break for better paragraph handling
	pdf.SetAutoPageBreak(true, config.MarginBottom)

	// Set margins for better readability
	pdf.SetLeftMargin(config.MarginLeft)
	pdf.SetRightMargin(config.MarginRight)
	pdf.SetTopMargin(config.MarginTop)

	// Add header with creation date
	pdf.SetX(config.MarginLeft)
	pdf.SetY(10)
	pdf.CellFormat(0, 10, fmt.Sprintf("Created: %s", config.CreationDate), "", 0, "R", false, 0, "")
	pdf.Ln(15)

	// Process text to handle paragraphs properly
	paragraphs := strings.Split(text, "\n\n")
	for i, paragraph := range paragraphs {
		// Replace single newlines with spaces for better flow
		paragraph = strings.ReplaceAll(paragraph, "\n", " ")

		// Write paragraph with UTF-8 encoding
		pdf.MultiCell(0, config.LineHeight, paragraph, "", "", false)

		// Add space between paragraphs
		if i < len(paragraphs)-1 {
			pdf.Ln(4)
		}
	}

	// Add page numbers
	nPages := pdf.PageCount()
	for pageNum := 1; pageNum <= nPages; pageNum++ {
		pdf.SetPage(pageNum)
		pdf.SetY(287) // Bottom of the page
		pdf.SetX(0)
		pdf.CellFormat(0, 10, fmt.Sprintf("Page %d of %d", pageNum, nPages), "", 0, "C", false, 0, "")
	}

	// Create output directory if it doesn't exist
	outputDir := "output"
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		os.Mkdir(outputDir, 0755)
	}

	// Generate unique filename based on timestamp
	timestamp := time.Now().Format("20060102-150405")
	outputPath := filepath.Join(outputDir, fmt.Sprintf("output-%s.pdf", timestamp))

	// Save the PDF
	err := pdf.OutputFileAndClose(outputPath)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPDFCreationFailed, err)
	}

	return outputPath, nil
}
