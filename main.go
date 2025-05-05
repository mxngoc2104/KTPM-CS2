package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"imageprocessor/pkg/cache"
	"imageprocessor/pkg/ocr"
	"imageprocessor/pkg/pdf"
	"imageprocessor/pkg/queue"
	"imageprocessor/pkg/translator"
	"imageprocessor/pkg/worker"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

var (
	// Command line flags
	serverPort  = flag.String("port", "8080", "Server port")
	workerMode  = flag.Bool("worker", false, "Run in worker mode")
	rabbitMQURL = flag.String("rabbitmq", "amqp://guest:guest@localhost:5672/", "RabbitMQ connection URL")
	redisURL    = flag.String("redis", "redis://localhost:6379/0", "Redis connection URL")
	useRedis    = flag.Bool("use-redis", true, "Use Redis for caching")
	resultsTTL  = flag.Duration("results-ttl", 7*24*time.Hour, "Results time-to-live")
	cacheTTL    = flag.Duration("cache-ttl", 24*time.Hour, "Cache time-to-live")
	uploadDir   = flag.String("upload-dir", "data/uploads", "Directory for uploaded files")
	outputDir   = flag.String("output-dir", "output", "Directory for output files")
	benchmark   = flag.Bool("benchmark", false, "Run in benchmark mode")
	numRequests = flag.Int("num-requests", 100, "Number of requests for benchmark")
	concurrency = flag.Int("concurrency", 10, "Number of concurrent requests for benchmark")
	useQueue    = flag.Bool("use-queue", true, "Use message queue for processing in benchmark")
)

// ProcessingResult represents the result of an image processing operation
type ProcessingResult struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	OriginalText   string    `json:"originalText,omitempty"`
	TranslatedText string    `json:"translatedText,omitempty"`
	PDFPath        string    `json:"pdfPath,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	CompletedAt    time.Time `json:"completedAt,omitempty"`
	Error          string    `json:"error,omitempty"`
}

// ProcessingRequest represents a request to process an image
type ProcessingRequest struct {
	ImageURL string `json:"imageUrl,omitempty"`
}

// ResultStore for storing processing results
var resultStore cache.ResultStore

func main() {
	flag.Parse()

	// Set up logging
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("Image Text Processor - Starting up")

	// Create upload and output directories
	ensureDir(*uploadDir)
	ensureDir(*outputDir)

	// Get environment variables if available (for Docker)
	if envRabbitMQURL := os.Getenv("RABBITMQ_URL"); envRabbitMQURL != "" {
		*rabbitMQURL = envRabbitMQURL
	}
	if envRedisURL := os.Getenv("REDIS_URL"); envRedisURL != "" {
		*redisURL = "redis://" + envRedisURL
		*useRedis = true
	}
	if envPort := os.Getenv("PORT"); envPort != "" {
		*serverPort = envPort
	}

	// Initialize caches and result store
	initCaches()
	initResultStore()

	// Run in worker mode if requested
	if *workerMode {
		runWorkerMode()
		return
	}

	// Run in benchmark mode if requested
	if *benchmark {
		runBenchmark()
		return
	}

	// Setup the HTTP server
	setupAndRunServer()
}

// initResultStore initializes the result store using Redis if enabled
func initResultStore() {
	var err error
	if *useRedis {
		// Initialize Redis result store
		resultStore, err = cache.NewRedisResultStore(*redisURL, *resultsTTL, "processing-results")
		if err != nil {
			log.Printf("Warning: Failed to initialize Redis result store: %v", err)
			log.Println("Falling back to in-memory result store")
			resultStore = cache.NewInMemoryResultStore()
		} else {
			log.Println("Using Redis for persistent result storage")
		}
	} else {
		// Initialize in-memory result store
		resultStore = cache.NewInMemoryResultStore()
		log.Println("Using in-memory result storage (non-persistent)")
	}
}

// initCaches initializes OCR and translation caches
func initCaches() {
	if *useRedis {
		// Initialize Redis caches
		redisAddr := *redisURL
		if err := ocr.InitRedisCache(redisAddr, *cacheTTL); err != nil {
			log.Printf("Warning: Failed to initialize Redis OCR cache: %v", err)
			log.Println("Falling back to in-memory OCR cache")
			ocr.InitCache(*cacheTTL)
		} else {
			log.Println("Using Redis for OCR cache")
		}

		if err := translator.InitRedisCache(redisAddr, *cacheTTL); err != nil {
			log.Printf("Warning: Failed to initialize Redis translation cache: %v", err)
			log.Println("Falling back to in-memory translation cache")
			translator.InitCache(*cacheTTL)
		} else {
			log.Println("Using Redis for translation cache")
		}
	} else {
		// Initialize in-memory caches
		ocr.InitCache(*cacheTTL)
		translator.InitCache(*cacheTTL)
		log.Println("Using in-memory caches (non-persistent)")
	}
}

// setupAndRunServer sets up the HTTP server with routes
func setupAndRunServer() {
	r := mux.NewRouter()

	// API routes
	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/process", handleProcessImage).Methods("POST")
	api.HandleFunc("/results/{id}", handleGetResult).Methods("GET")
	api.HandleFunc("/health", handleHealthCheck).Methods("GET")

	// Static file server for downloaded PDFs
	r.PathPrefix("/output/").Handler(http.StripPrefix("/output/", http.FileServer(http.Dir(*outputDir))))

	// Start the server
	serverAddr := fmt.Sprintf(":%s", *serverPort)
	log.Printf("Starting server on %s", serverAddr)
	log.Fatal(http.ListenAndServe(serverAddr, r))
}

// handleHealthCheck handles API health check requests
func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "up",
		"version": "1.0.0",
	})
}

// handleProcessImage handles requests to process an image
func handleProcessImage(w http.ResponseWriter, r *http.Request) {
	// Set response content type
	w.Header().Set("Content-Type", "application/json")

	// Parse multipart form (max 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Unable to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get the file from the request
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Error retrieving file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Generate a unique ID for this processing job
	jobID := uuid.New().String()

	// Create a new result record
	result := ProcessingResult{
		ID:        jobID,
		Status:    "processing",
		CreatedAt: time.Now(),
	}

	// Store the result
	if err := resultStore.Set(jobID, result); err != nil {
		log.Printf("Error storing result: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Save the file
	filename := filepath.Join(*uploadDir, fmt.Sprintf("%s-%s", jobID, header.Filename))
	out, err := os.Create(filename)
	if err != nil {
		http.Error(w, "Error saving file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		http.Error(w, "Error copying file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Process the image asynchronously (using queue or direct)
	if *useQueue {
		go processImageWithQueue(jobID, filename)
	} else {
		go processImageAsync(jobID, filename)
	}

	// Return the job ID to the client
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"id":     jobID,
		"status": "processing",
	})
}

// handleGetResult handles requests to get the result of a processing job
func handleGetResult(w http.ResponseWriter, r *http.Request) {
	// Set response content type
	w.Header().Set("Content-Type", "application/json")

	// Get the job ID from the URL
	vars := mux.Vars(r)
	jobID := vars["id"]

	// Get the result
	var result ProcessingResult
	found, err := resultStore.GetTyped(jobID, &result)
	if err != nil {
		log.Printf("Error retrieving result: %v", err)
		http.Error(w, "Error retrieving result", http.StatusInternalServerError)
		return
	}

	if !found {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Return the result
	json.NewEncoder(w).Encode(result)
}

// processImageAsync processes an image asynchronously
func processImageAsync(jobID, imagePath string) {
	var result ProcessingResult
	var err error

	// Get the current result
	found, err := resultStore.GetTyped(jobID, &result)
	if err != nil || !found {
		log.Printf("Error retrieving result for processing: %v", err)
		return
	}

	// Step 1: OCR - Convert image to text
	log.Printf("Job %s: Converting image to text...", jobID)
	text, err := ocr.ImageToText(imagePath)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("OCR error: %v", err)
		resultStore.Set(jobID, result)
		return
	}
	result.OriginalText = text

	// Step 2: Translate text from English to Vietnamese
	log.Printf("Job %s: Translating text...", jobID)
	translatedText, err := translator.Translate(text)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("Translation error: %v", err)
		resultStore.Set(jobID, result)
		return
	}
	result.TranslatedText = translatedText

	// Step 3: Generate PDF with the translated text
	log.Printf("Job %s: Creating PDF...", jobID)
	pdfPath, err := pdf.CreatePDF(translatedText)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("PDF creation error: %v", err)
		resultStore.Set(jobID, result)
		return
	}

	// Update the result
	result.Status = "completed"
	result.PDFPath = pdfPath
	result.CompletedAt = time.Now()
	resultStore.Set(jobID, result)

	log.Printf("Job %s: Processing completed successfully", jobID)
}

// processImageWithQueue processes the image using a message queue
func processImageWithQueue(jobID, imagePath string) {
	// Connect to RabbitMQ
	mq, err := queue.NewRabbitMQ(*rabbitMQURL)
	if err != nil {
		log.Printf("Job %s: Failed to connect to RabbitMQ: %v", jobID, err)

		// Update result status
		var result ProcessingResult
		found, _ := resultStore.GetTyped(jobID, &result)
		if found {
			result.Status = "failed"
			result.Error = fmt.Sprintf("Queue connection error: %v", err)
			resultStore.Set(jobID, result)
		}
		return
	}
	defer mq.Close()

	// Declare queues
	queues := []string{"ocr_queue", "translation_queue", "pdf_queue"}
	for _, q := range queues {
		if err := mq.DeclareQueue(q); err != nil {
			log.Printf("Job %s: Failed to declare queue %s: %v", jobID, q, err)

			// Update result status
			var result ProcessingResult
			found, _ := resultStore.GetTyped(jobID, &result)
			if found {
				result.Status = "failed"
				result.Error = fmt.Sprintf("Queue setup error: %v", err)
				resultStore.Set(jobID, result)
			}
			return
		}
	}

	// Create OCR task
	ocrTask := queue.ProcessingTask{
		Type:      queue.OCRTask,
		ImagePath: imagePath,
		ResultID:  jobID + "-ocr",
	}

	// Publish OCR task
	log.Printf("Job %s: Submitting OCR task to queue...", jobID)
	if err := mq.PublishMessage("ocr_queue", ocrTask); err != nil {
		log.Printf("Job %s: Failed to publish OCR task: %v", jobID, err)

		// Update result status
		var result ProcessingResult
		found, _ := resultStore.GetTyped(jobID, &result)
		if found {
			result.Status = "failed"
			result.Error = fmt.Sprintf("Queue publish error: %v", err)
			resultStore.Set(jobID, result)
		}
		return
	}

	// Ensure result has been initialized in Redis
	var result ProcessingResult
	found, _ := resultStore.GetTyped(jobID, &result)
	if !found {
		// If not found, initialize the result
		result = ProcessingResult{
			ID:        jobID,
			Status:    "processing",
			CreatedAt: time.Now(),
		}
		if err := resultStore.Set(jobID, result); err != nil {
			log.Printf("Job %s: Failed to initialize result: %v", jobID, err)
		}
	}

	log.Printf("Job %s: OCR task submitted to queue", jobID)
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
	log.Printf("Running benchmark with %d requests, %d concurrent, queue: %t", *numRequests, *concurrency, *useQueue)

	// Prepare benchmark image
	imagePath := filepath.Join("data", "sample.png")
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		log.Fatalf("Benchmark image not found: %s", imagePath)
	}

	// Initialize benchmark results
	type benchmarkResult struct {
		JobID        string
		StartTime    time.Time
		EndTime      time.Time
		Duration     time.Duration
		Success      bool
		ErrorMessage string
	}
	results := make([]benchmarkResult, *numRequests)

	// Create semaphore channel to limit concurrency
	sem := make(chan bool, *concurrency)

	// Start benchmark
	startTime := time.Now()

	// Prepare wait group to wait for all requests to complete
	var wg sync.WaitGroup
	wg.Add(*numRequests)

	// Setup timeout values based on queue mode
	queueWaitTimeout := 60 * time.Second
	if *useQueue {
		// Increase timeout when using queue
		queueWaitTimeout = 180 * time.Second
	}

	// Setup mechanism to track overall benchmark timeout
	benchmarkTimeout := make(chan bool, 1)
	overallTimeout := queueWaitTimeout + 30*time.Second // Extra time for overall benchmark

	go func() {
		time.Sleep(overallTimeout)
		benchmarkTimeout <- true
	}()

	// Track active jobs
	activeJobs := sync.WaitGroup{}

	for i := 0; i < *numRequests; i++ {
		sem <- true // Acquire semaphore

		go func(idx int) {
			defer func() {
				<-sem // Release semaphore
				wg.Done()
			}()

			// Start timing
			jobStartTime := time.Now()
			jobID := uuid.New().String()
			activeJobs.Add(1)

			// Initialize result in Redis
			result := ProcessingResult{
				ID:        jobID,
				Status:    "processing",
				CreatedAt: jobStartTime,
			}
			resultStore.Set(jobID, result)

			// Process the image
			if *useQueue {
				processImageWithQueue(jobID, imagePath)
			} else {
				processImageAsync(jobID, imagePath)
			}

			// Wait for job to complete (poll)
			success := false
			var errorMsg string
			var jobEndTime time.Time

			waitTimeout := time.After(queueWaitTimeout)
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-waitTimeout:
					// If job didn't complete in time
					jobEndTime = time.Now()
					errorMsg = "timeout waiting for job to complete"
					if *useQueue {
						log.Printf("Job %s: Timed out waiting for queue processing", jobID)
					}
					goto RESULT_PROCESSING

				case <-ticker.C:
					var currentResult ProcessingResult
					found, err := resultStore.GetTyped(jobID, &currentResult)

					if err != nil {
						log.Printf("Job %s: Error retrieving result: %v", jobID, err)
						continue
					}

					if found && (currentResult.Status == "completed" || currentResult.Status == "failed") {
						success = currentResult.Status == "completed"
						errorMsg = currentResult.Error
						jobEndTime = time.Now()
						goto RESULT_PROCESSING
					}
				}
			}

		RESULT_PROCESSING:
			// Store result
			results[idx] = benchmarkResult{
				JobID:        jobID,
				StartTime:    jobStartTime,
				EndTime:      jobEndTime,
				Duration:     jobEndTime.Sub(jobStartTime),
				Success:      success,
				ErrorMessage: errorMsg,
			}
			activeJobs.Done()
		}(i)
	}

	// Wait for all requests to complete with timeout
	doneChannel := make(chan bool, 1)
	go func() {
		wg.Wait()
		doneChannel <- true
	}()

	// Wait for completion or timeout
	select {
	case <-doneChannel:
		log.Println("All benchmark tasks completed normally")
	case <-benchmarkTimeout:
		log.Println("Benchmark timed out - some tasks may not have completed")
		// At this point we'll continue to process the results that did complete
	}

	totalDuration := time.Since(startTime)

	// Calculate statistics
	var totalDurationSum time.Duration
	var successCount int
	var completedCount int
	var minDuration, maxDuration time.Duration

	if len(results) > 0 && results[0].Duration > 0 {
		minDuration = results[0].Duration
		maxDuration = results[0].Duration
	}

	for _, r := range results {
		if r.Duration > 0 {
			completedCount++
			totalDurationSum += r.Duration

			if r.Success {
				successCount++
			}

			if r.Duration < minDuration {
				minDuration = r.Duration
			}

			if r.Duration > maxDuration {
				maxDuration = r.Duration
			}
		}
	}

	// Avoid division by zero
	avgDuration := time.Duration(0)
	if completedCount > 0 {
		avgDuration = totalDurationSum / time.Duration(completedCount)
	}

	successRate := 0.0
	if completedCount > 0 {
		successRate = float64(successCount) / float64(completedCount) * 100
	}

	requestsPerSecond := 0.0
	if totalDuration.Seconds() > 0 {
		requestsPerSecond = float64(completedCount) / totalDuration.Seconds()
	}

	// Print results
	log.Println("=== Benchmark Results ===")
	log.Printf("Total requests: %d", *numRequests)
	log.Printf("Completed requests: %d", completedCount)
	log.Printf("Concurrency: %d", *concurrency)
	log.Printf("Queue mode: %t", *useQueue)
	log.Printf("Total time: %v", totalDuration)
	log.Printf("Average duration: %v", avgDuration)
	log.Printf("Min duration: %v", minDuration)
	log.Printf("Max duration: %v", maxDuration)
	log.Printf("Success rate: %.2f%%", successRate)
	log.Printf("Requests per second: %.2f", requestsPerSecond)

	// Cache stats
	log.Printf("OCR cache items: %d", ocr.GetCacheSize())
	log.Printf("Translation cache items: %d", translator.GetCacheSize())

	// If there were errors, display them
	if successCount < completedCount {
		log.Println("\nErrors encountered:")
		for _, r := range results {
			if r.Duration > 0 && !r.Success {
				log.Printf("Job %s: %s", r.JobID, r.ErrorMessage)
			}
		}
	}
}

// ensureDir ensures that a directory exists
func ensureDir(dir string) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
}
