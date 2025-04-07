package pdf

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jung-kurt/gofpdf"
)

// CreatePDF generates a PDF file with the given text
func CreatePDF(text string) (string, error) {
	// Create a new PDF document with UTF-8 encoding
	pdf := gofpdf.New("P", "mm", "A4", "")
	
	// Set up font directory
	fontDir := "font"
	fontName := "Roboto"
	
	// Register the TrueType font for Vietnamese characters
	pdf.SetFontLocation(fontDir)
	pdf.AddUTF8Font(fontName, "", "Roboto-Regular.ttf")
	
	// Add a page
	pdf.AddPage()
	
	// Set font with UTF-8 encoding
	pdf.SetFont(fontName, "", 11)
	
	// Enable auto page break for better paragraph handling
	pdf.SetAutoPageBreak(true, 15)
	
	// Set margins for better readability
	pdf.SetLeftMargin(15)
	pdf.SetRightMargin(15)
	pdf.SetTopMargin(15)
	
	// Process text to handle paragraphs properly
	paragraphs := strings.Split(text, "\n\n")
	for i, paragraph := range paragraphs {
		// Replace single newlines with spaces for better flow
		paragraph = strings.ReplaceAll(paragraph, "\n", " ")
		
		// Write paragraph with UTF-8 encoding
		pdf.MultiCell(0, 6, paragraph, "", "", false)
		
		// Add space between paragraphs
		if i < len(paragraphs)-1 {
			pdf.Ln(4)
		}
	}
	
	// Create output directory if it doesn't exist
	outputDir := "output"
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		os.Mkdir(outputDir, 0755)
	}
	
	// Save the PDF
	outputPath := filepath.Join(outputDir, "output.pdf")
	err := pdf.OutputFileAndClose(outputPath)
	
	return outputPath, err
}