package worker

import (
	"fmt"
	"imageprocessor/pkg/cache"
	"imageprocessor/pkg/ocr"
	"imageprocessor/pkg/pdf"
	"imageprocessor/pkg/queue"
	"imageprocessor/pkg/translator"
	"log"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// WorkerConfig holds configuration for workers
type WorkerConfig struct {
	OCRConfig         ocr.OCRConfig
	TranslationConfig translator.TranslationConfig
	PDFConfig         pdf.PDFConfig
	RedisURL          string
	UseRedis          bool
	ResultsTTL        time.Duration
}

// DefaultWorkerConfig returns a default worker configuration
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		OCRConfig:         ocr.DefaultOCRConfig(),
		TranslationConfig: translator.DefaultTranslationConfig(),
		PDFConfig:         pdf.DefaultPDFConfig(),
		RedisURL:          "redis://localhost:6379/0",
		UseRedis:          true,
		ResultsTTL:        7 * 24 * time.Hour,
	}
}

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

// OCRWorker represents a worker for OCR tasks
type OCRWorker struct {
	mq          *queue.RabbitMQ
	queueName   string
	resultStore cache.ResultStore
	config      ocr.OCRConfig
	redisClient *redis.Client
}

// NewOCRWorker creates a new OCR worker
func NewOCRWorker(mq *queue.RabbitMQ, queueName string, resultStore cache.ResultStore, config ocr.OCRConfig) *OCRWorker {
	return &OCRWorker{
		mq:          mq,
		queueName:   queueName,
		resultStore: resultStore,
		config:      config,
	}
}

// Start starts the OCR worker
func (w *OCRWorker) Start() error {
	log.Printf("Starting OCR worker for queue: %s", w.queueName)

	// Declare the queue if it doesn't exist
	if err := w.mq.DeclareQueue(w.queueName); err != nil {
		return fmt.Errorf("failed to declare OCR queue: %w", err)
	}

	// Start consuming messages
	return w.mq.ConsumeMessages(w.queueName, func(task queue.ProcessingTask) error {
		log.Printf("Processing OCR task: %s", task.ImagePath)

		// Extract job ID from result ID (remove -ocr suffix)
		jobID := strings.TrimSuffix(task.ResultID, "-ocr")

		// Get current processing result
		var result ProcessingResult
		found, err := w.resultStore.GetTyped(jobID, &result)
		if err != nil {
			log.Printf("Warning: Error retrieving result for job %s: %v", jobID, err)
		}

		if !found {
			// Create a new result if not found
			result = ProcessingResult{
				ID:        jobID,
				Status:    "processing",
				CreatedAt: time.Now(),
			}
		}

		// Update result to show processing started
		result.Status = "processing"
		if err := w.resultStore.Set(jobID, result); err != nil {
			log.Printf("Warning: Failed to update result status: %v", err)
		}

		// Process the OCR task
		text, err := ocr.ImageToTextWithConfig(task.ImagePath, w.config)
		if err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("OCR error: %v", err)
			if err := w.resultStore.Set(jobID, result); err != nil {
				log.Printf("Error storing result: %v", err)
			}
			return fmt.Errorf("OCR processing failed: %w", err)
		}

		// Update result with original text
		result.OriginalText = text
		if err := w.resultStore.Set(jobID, result); err != nil {
			log.Printf("Error updating result: %v", err)
		}

		// Store intermediate result for the OCR worker
		if err := w.resultStore.Set(task.ResultID, text); err != nil {
			log.Printf("Warning: Failed to store OCR result: %v", err)
		}

		// Create translation task
		translationTask := queue.ProcessingTask{
			Type:     queue.TranslationTask,
			Text:     text,
			ResultID: jobID + "-translation",
		}

		// Publish translation task
		if err := w.mq.PublishMessage("translation_queue", translationTask); err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("Failed to queue translation: %v", err)
			if err := w.resultStore.Set(jobID, result); err != nil {
				log.Printf("Error storing result: %v", err)
			}
			return fmt.Errorf("failed to publish translation task: %w", err)
		}

		log.Printf("OCR task completed for ID: %s", task.ResultID)
		return nil
	})
}

// TranslationWorker represents a worker for translation tasks
type TranslationWorker struct {
	mq          *queue.RabbitMQ
	queueName   string
	resultStore cache.ResultStore
	config      translator.TranslationConfig
}

// NewTranslationWorker creates a new translation worker
func NewTranslationWorker(mq *queue.RabbitMQ, queueName string, resultStore cache.ResultStore, config translator.TranslationConfig) *TranslationWorker {
	return &TranslationWorker{
		mq:          mq,
		queueName:   queueName,
		resultStore: resultStore,
		config:      config,
	}
}

// Start starts the translation worker
func (w *TranslationWorker) Start() error {
	log.Printf("Starting translation worker for queue: %s", w.queueName)

	// Declare the queue if it doesn't exist
	if err := w.mq.DeclareQueue(w.queueName); err != nil {
		return fmt.Errorf("failed to declare translation queue: %w", err)
	}

	// Start consuming messages
	return w.mq.ConsumeMessages(w.queueName, func(task queue.ProcessingTask) error {
		log.Printf("Processing translation task: %s", task.ResultID)

		// Extract job ID from result ID (remove -translation suffix)
		jobID := strings.TrimSuffix(task.ResultID, "-translation")

		// Get current processing result
		var result ProcessingResult
		found, err := w.resultStore.GetTyped(jobID, &result)
		if err != nil {
			log.Printf("Warning: Error retrieving result for job %s: %v", jobID, err)
			// Create a default result if error occurred
			result = ProcessingResult{
				ID:        jobID,
				Status:    "processing",
				CreatedAt: time.Now(),
			}
			found = true
		}

		if !found {
			return fmt.Errorf("failed to retrieve result for job %s: result not found", jobID)
		}

		// Process the translation task
		translatedText, err := translator.TranslateWithConfig(task.Text, w.config)
		if err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("Translation error: %v", err)
			if err := w.resultStore.Set(jobID, result); err != nil {
				log.Printf("Error storing result: %v", err)
			}
			return fmt.Errorf("translation failed: %w", err)
		}

		// Update result with translated text
		result.TranslatedText = translatedText
		if err := w.resultStore.Set(jobID, result); err != nil {
			log.Printf("Error updating result: %v", err)
		}

		// Store intermediate result for the translation worker
		if err := w.resultStore.Set(task.ResultID, translatedText); err != nil {
			log.Printf("Warning: Failed to store translation result: %v", err)
		}

		// Create PDF task
		pdfTask := queue.ProcessingTask{
			Type:     queue.PDFTask,
			Text:     translatedText,
			ResultID: jobID + "-pdf",
		}

		// Publish PDF task
		if err := w.mq.PublishMessage("pdf_queue", pdfTask); err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("Failed to queue PDF creation: %v", err)
			if err := w.resultStore.Set(jobID, result); err != nil {
				log.Printf("Error storing result: %v", err)
			}
			return fmt.Errorf("failed to publish PDF task: %w", err)
		}

		log.Printf("Translation task completed for ID: %s", task.ResultID)
		return nil
	})
}

// PDFWorker represents a worker for PDF tasks
type PDFWorker struct {
	mq          *queue.RabbitMQ
	queueName   string
	resultStore cache.ResultStore
	config      pdf.PDFConfig
}

// NewPDFWorker creates a new PDF worker
func NewPDFWorker(mq *queue.RabbitMQ, queueName string, resultStore cache.ResultStore, config pdf.PDFConfig) *PDFWorker {
	return &PDFWorker{
		mq:          mq,
		queueName:   queueName,
		resultStore: resultStore,
		config:      config,
	}
}

// Start starts the PDF worker
func (w *PDFWorker) Start() error {
	log.Printf("Starting PDF worker for queue: %s", w.queueName)

	// Declare the queue if it doesn't exist
	if err := w.mq.DeclareQueue(w.queueName); err != nil {
		return fmt.Errorf("failed to declare PDF queue: %w", err)
	}

	// Start consuming messages
	return w.mq.ConsumeMessages(w.queueName, func(task queue.ProcessingTask) error {
		log.Printf("Processing PDF task: %s", task.ResultID)

		// Extract job ID from result ID (remove -pdf suffix)
		jobID := strings.TrimSuffix(task.ResultID, "-pdf")

		// Get current processing result
		var result ProcessingResult
		found, err := w.resultStore.GetTyped(jobID, &result)
		if err != nil {
			log.Printf("Warning: Error retrieving result for job %s: %v", jobID, err)
			// Create a default result if error occurred
			result = ProcessingResult{
				ID:        jobID,
				Status:    "processing",
				CreatedAt: time.Now(),
			}
			found = true
		}

		if !found {
			return fmt.Errorf("failed to retrieve result for job %s: result not found", jobID)
		}

		// Process the PDF task
		pdfPath, err := pdf.CreatePDFWithConfig(task.Text, w.config)
		if err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("PDF creation error: %v", err)
			if err := w.resultStore.Set(jobID, result); err != nil {
				log.Printf("Error storing result: %v", err)
			}
			return fmt.Errorf("PDF creation failed: %w", err)
		}

		// Mark task as completed
		result.Status = "completed"
		result.PDFPath = pdfPath
		result.CompletedAt = time.Now()

		log.Printf("Job %s completed successfully", jobID)

		if err := w.resultStore.Set(jobID, result); err != nil {
			log.Printf("Error storing final result: %v", err)
		}

		// Store intermediate result for the PDF worker
		if err := w.resultStore.Set(task.ResultID, pdfPath); err != nil {
			log.Printf("Warning: Failed to store PDF result: %v", err)
		}

		log.Printf("PDF task completed: %s", pdfPath)
		return nil
	})
}

// StartWorkers starts all workers with Redis or in-memory result store
func StartWorkers(rabbitmqURL string) (*queue.RabbitMQ, cache.ResultStore, error) {
	return StartWorkersWithConfig(rabbitmqURL, DefaultWorkerConfig())
}

// StartWorkersWithConfig starts all workers with custom configuration
func StartWorkersWithConfig(rabbitmqURL string, config WorkerConfig) (*queue.RabbitMQ, cache.ResultStore, error) {
	// Connect to RabbitMQ
	mq, err := queue.NewRabbitMQ(rabbitmqURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Create result store (Redis or in-memory)
	var resultStore cache.ResultStore
	if config.UseRedis {
		resultStore, err = cache.NewRedisResultStore(config.RedisURL, config.ResultsTTL, "processing-results")
		if err != nil {
			log.Printf("Warning: Failed to create Redis result store: %v, falling back to in-memory", err)
			resultStore = cache.NewInMemoryResultStore()
		} else {
			log.Println("Using Redis for persistent result storage")
		}
	} else {
		resultStore = cache.NewInMemoryResultStore()
		log.Println("Using in-memory result storage (non-persistent)")
	}

	// Initialize caches
	if config.UseRedis {
		if err := ocr.InitRedisCache(config.RedisURL, config.OCRConfig.CacheTTL); err != nil {
			log.Printf("Warning: Failed to initialize Redis OCR cache: %v", err)
			ocr.InitCache(config.OCRConfig.CacheTTL)
		}

		if err := translator.InitRedisCache(config.RedisURL, config.TranslationConfig.CacheTTL); err != nil {
			log.Printf("Warning: Failed to initialize Redis translation cache: %v", err)
			translator.InitCache(config.TranslationConfig.CacheTTL)
		}
	} else {
		ocr.InitCache(config.OCRConfig.CacheTTL)
		translator.InitCache(config.TranslationConfig.CacheTTL)
	}

	// Create and start OCR worker
	ocrWorker := NewOCRWorker(mq, "ocr_queue", resultStore, config.OCRConfig)
	go func() {
		if err := ocrWorker.Start(); err != nil {
			log.Printf("OCR worker error: %v", err)
			os.Exit(1)
		}
	}()

	// Create and start translation worker
	translationWorker := NewTranslationWorker(mq, "translation_queue", resultStore, config.TranslationConfig)
	go func() {
		if err := translationWorker.Start(); err != nil {
			log.Printf("Translation worker error: %v", err)
			os.Exit(1)
		}
	}()

	// Create and start PDF worker
	pdfWorker := NewPDFWorker(mq, "pdf_queue", resultStore, config.PDFConfig)
	go func() {
		if err := pdfWorker.Start(); err != nil {
			log.Printf("PDF worker error: %v", err)
			os.Exit(1)
		}
	}()

	// Wait for workers to be initialized
	time.Sleep(1 * time.Second)

	return mq, resultStore, nil
}
