package main

import (
	"flag"
	"fmt"
	"imageprocessor/pkg/ocr"
	"imageprocessor/pkg/pdf"
	"imageprocessor/pkg/queue"
	"imageprocessor/pkg/translator"
	"imageprocessor/pkg/worker"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

var (
	// Command line flags
	imagePath     = flag.String("image", "data/sample.png", "Path to image for processing")
	useQueue      = flag.Bool("queue", false, "Use message queue for processing")
	rabbitMQURL   = flag.String("rabbitmq", "amqp://guest:guest@localhost:5672/", "RabbitMQ connection URL")
	workerMode    = flag.Bool("worker", false, "Run in worker mode")
	benchmarkMode = flag.Bool("benchmark", false, "Run in benchmark mode")
)

func main() {
	flag.Parse()

	// Set up logging
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("Image Text Processor - Starting up")

	// Run in worker mode if requested
	if *workerMode {
		runWorkerMode()
		return
	}

	// Run in benchmark mode if requested
	if *benchmarkMode {
		runBenchmark()
		return
	}

	// Check if image exists
	if _, err := os.Stat(*imagePath); os.IsNotExist(err) {
		log.Fatalf("Error: Image does not exist: %s\nUse -image flag to specify a different image.", *imagePath)
	}

	// Process the image (either direct or via queue)
	if *useQueue {
		processImageWithQueue(*imagePath)
	} else {
		processImageDirect(*imagePath)
	}
}

// processImageDirect processes the image using direct function calls
func processImageDirect(imagePath string) {
	var startTime, endTime time.Time

	// Ensure the output directory exists
	outputDir := "output"
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		os.Mkdir(outputDir, 0755)
	}

	// Step 1: OCR - Convert image to text
	fmt.Println("Step 1: Converting image to text...")
	startTime = time.Now()
	text, err := ocr.ImageToText(imagePath)
	endTime = time.Now()
	if err != nil {
		log.Fatalf("OCR error: %v", err)
	}
	fmt.Printf("Extracted text (completed in %v):\n", endTime.Sub(startTime))
	fmt.Println("-------------------")
	fmt.Println(text)
	fmt.Println("-------------------")

	// Step 2: Translate text from English to Vietnamese
	fmt.Println("\nStep 2: Translating text to Vietnamese...")
	startTime = time.Now()
	translatedText, err := translator.Translate(text)
	endTime = time.Now()
	if err != nil {
		log.Fatalf("Translation error: %v", err)
	}
	fmt.Printf("Translated text (completed in %v):\n", endTime.Sub(startTime))
	fmt.Println("-------------------")
	fmt.Println(translatedText)
	fmt.Println("-------------------")

	// Step 3: Generate PDF with the translated text
	fmt.Println("\nStep 3: Creating PDF with translated text...")
	startTime = time.Now()
	pdfPath, err := pdf.CreatePDF(translatedText)
	endTime = time.Now()
	if err != nil {
		log.Fatalf("PDF creation error: %v", err)
	}
	fmt.Printf("PDF creation completed in %v\n", endTime.Sub(startTime))

	absPath, _ := filepath.Abs(pdfPath)
	fmt.Printf("\nProcess completed successfully!\nOutput PDF: %s\n", absPath)

	// Display cache statistics
	fmt.Println("\nCache Statistics:")
	fmt.Printf("OCR cache size: %d items\n", ocr.GetCacheSize())
	fmt.Printf("Translation cache size: %d items\n", translator.GetCacheSize())
}

// processImageWithQueue processes the image using a message queue
func processImageWithQueue(imagePath string) {
	// Connect to RabbitMQ
	mq, err := queue.NewRabbitMQ(*rabbitMQURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer mq.Close()

	// Declare queues
	queues := []string{"ocr_queue", "translation_queue", "pdf_queue"}
	for _, q := range queues {
		if err := mq.DeclareQueue(q); err != nil {
			log.Fatalf("Failed to declare queue %s: %v", q, err)
		}
	}

	// Generate a unique ID for this processing job
	jobID := uuid.New().String()

	// Create OCR task
	ocrTask := queue.ProcessingTask{
		Type:      queue.OCRTask,
		ImagePath: imagePath,
		ResultID:  jobID + "-ocr",
	}

	// Publish OCR task
	fmt.Println("Step 1: Submitting OCR task to queue...")
	startTime := time.Now()
	if err := mq.PublishMessage("ocr_queue", ocrTask); err != nil {
		log.Fatalf("Failed to publish OCR task: %v", err)
	}

	// Wait for OCR task to complete
	resultStore := worker.NewResultStore()
	var ocrResult string
	var found bool
	for i := 0; i < 60; i++ { // Wait up to 60 seconds
		// Check if result is available in local store
		if ocrResult, found = resultStore.Get(ocrTask.ResultID); found {
			break
		}

		time.Sleep(1 * time.Second)
	}

	if !found {
		log.Fatalf("Timed out waiting for OCR task to complete")
	}

	endTime := time.Now()
	fmt.Printf("OCR task completed in %v\n", endTime.Sub(startTime))
	fmt.Println("Extracted text:")
	fmt.Println("-------------------")
	fmt.Println(ocrResult)
	fmt.Println("-------------------")

	// Create translation task
	translationTask := queue.ProcessingTask{
		Type:     queue.TranslationTask,
		Text:     ocrResult,
		ResultID: jobID + "-translation",
	}

	// Publish translation task
	fmt.Println("\nStep 2: Submitting translation task to queue...")
	startTime = time.Now()
	if err := mq.PublishMessage("translation_queue", translationTask); err != nil {
		log.Fatalf("Failed to publish translation task: %v", err)
	}

	// Wait for translation task to complete
	var translationResult string
	for i := 0; i < 60; i++ { // Wait up to 60 seconds
		if translationResult, found = resultStore.Get(translationTask.ResultID); found {
			break
		}

		time.Sleep(1 * time.Second)
	}

	if !found {
		log.Fatalf("Timed out waiting for translation task to complete")
	}

	endTime = time.Now()
	fmt.Printf("Translation task completed in %v\n", endTime.Sub(startTime))
	fmt.Println("Translated text:")
	fmt.Println("-------------------")
	fmt.Println(translationResult)
	fmt.Println("-------------------")

	// Create PDF task
	pdfTask := queue.ProcessingTask{
		Type:     queue.PDFTask,
		Text:     translationResult,
		ResultID: jobID + "-pdf",
	}

	// Publish PDF task
	fmt.Println("\nStep 3: Submitting PDF creation task to queue...")
	startTime = time.Now()
	if err := mq.PublishMessage("pdf_queue", pdfTask); err != nil {
		log.Fatalf("Failed to publish PDF task: %v", err)
	}

	// Wait for PDF task to complete
	var pdfPath string
	for i := 0; i < 60; i++ { // Wait up to 60 seconds
		if pdfPath, found = resultStore.Get(pdfTask.ResultID); found {
			break
		}

		time.Sleep(1 * time.Second)
	}

	if !found {
		log.Fatalf("Timed out waiting for PDF task to complete")
	}

	endTime = time.Now()
	fmt.Printf("PDF task completed in %v\n", endTime.Sub(startTime))

	absPath, _ := filepath.Abs(pdfPath)
	fmt.Printf("\nProcess completed successfully!\nOutput PDF: %s\n", absPath)
}

// runWorkerMode runs the application in worker mode
func runWorkerMode() {
	log.Println("Starting in worker mode")

	// Start workers
	mq, _, err := worker.StartWorkers(*rabbitMQURL)
	if err != nil {
		log.Fatalf("Failed to start workers: %v", err)
	}
	defer mq.Close()

	// Keep the workers running
	log.Println("Workers started. Press Ctrl+C to exit.")
	select {}
}

// runBenchmark runs a benchmark of the processing pipeline
func runBenchmark() {
	log.Println("Running benchmark...")

	// Initialize caches
	ocr.InitCache(24 * time.Hour)
	translator.InitCache(24 * time.Hour)

	// Clear caches to ensure clean benchmark
	ocr.ClearCache()
	translator.ClearCache()

	// Test image
	if _, err := os.Stat(*imagePath); os.IsNotExist(err) {
		log.Fatalf("Error: Benchmark image does not exist: %s", *imagePath)
	}

	// Run direct processing twice
	// First run (cold, no cache)
	log.Println("\n=== Cold Run (No Cache) ===")
	coldStart := time.Now()
	processImageDirect(*imagePath)
	coldDuration := time.Since(coldStart)

	// Second run (warm, with cache)
	log.Println("\n=== Warm Run (With Cache) ===")
	warmStart := time.Now()
	processImageDirect(*imagePath)
	warmDuration := time.Since(warmStart)

	// Print results
	log.Println("\n=== Benchmark Results ===")
	log.Printf("Cold run (no cache): %v", coldDuration)
	log.Printf("Warm run (with cache): %v", warmDuration)
	log.Printf("Improvement: %.2f%%", 100*(1-float64(warmDuration)/float64(coldDuration)))

	// Cache stats
	log.Printf("OCR cache items: %d", ocr.GetCacheSize())
	log.Printf("Translation cache items: %d", translator.GetCacheSize())
}
