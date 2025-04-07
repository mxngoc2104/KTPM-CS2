# Image Text Processor

A Go application that extracts text from images, translates it from English to Vietnamese, and generates a PDF with the translated content.

## Features

- **OCR (Optical Character Recognition)**: Extract text from images using Tesseract OCR
- **Translation**: Translate text from English to Vietnamese using Google Translate
- **PDF Generation**: Create well-formatted PDFs with proper Vietnamese character support

## Prerequisites

- Go 1.24 or higher
- Tesseract OCR installed on your system
- Internet connection for translation services

### Installing Tesseract OCR

#### Ubuntu/Debian
```bash
sudo apt-get update
sudo apt-get install tesseract-ocr
```

#### macOS
```bash
brew install tesseract
```

#### Windows
Download and install from [Tesseract GitHub page](https://github.com/UB-Mannheim/tesseract/wiki)

## Installation

1. Clone the repository
```bash
git clone https://github.com/mxngoc2104/KTPM-CS2.git
cd KTPM-CS2
```

2. Install Go dependencies
```bash
go mod download
```

## Usage

### Basic Usage
```bash
go run main.go <path-to-image>
```

### Example
```bash
go run main.go data/sample.png
```

If no image path is provided, the program will use the default image at `data/sample.png`.

## Project Structure

```
.
├── data/               # Sample images for testing
├── font/               # Font files for PDF generation
│   └── Roboto-Regular.ttf
├── output/             # Generated PDF files
├── pkg/                # Package directory
│   ├── ocr/            # OCR functionality
│   ├── translator/     # Translation services
│   └── pdf/            # PDF generation
├── go.mod              # Go module file
├── go.sum              # Go dependencies checksum
└── main.go             # Main application entry point
```

## How It Works

1. **Text Extraction**: The application uses Tesseract OCR to extract text from the provided image.
2. **Translation**: The extracted text is translated from English to Vietnamese using Google Translate's API.
3. **PDF Generation**: A PDF is generated with the translated text, using the Roboto font for proper Vietnamese character display.

## Technical Details

- **OCR Engine**: Tesseract (via command-line interface)
- **Translation**: Google Translate (unofficial API)
- **PDF Library**: gofpdf with UTF-8 support for Vietnamese characters
- **Font**: Roboto Regular for proper Vietnamese character rendering

## Error Handling

The application includes robust error handling for:
- Network connectivity issues during translation
- OCR processing errors
- PDF generation failures