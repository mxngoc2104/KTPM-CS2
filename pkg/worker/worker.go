package worker

import (
	"fmt"
	"imageprocessor/pkg/ocr"
	"imageprocessor/pkg/pdf"
	"imageprocessor/pkg/queue"
	"imageprocessor/pkg/translator"
	"log"
	"os"
	"sync"
	"time"
)

// ResultStore stores processing results
type ResultStore struct {
	results map[string]string
	mutex   sync.RWMutex
}

// NewResultStore creates a new result store
func NewResultStore() *ResultStore {
	return &ResultStore{
		results: make(map[string]string),
	}
}

// Set adds a result to the store
func (s *ResultStore) Set(id string, result string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.results[id] = result
}

// Get retrieves a result from the store
func (s *ResultStore) Get(id string) (string, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	result, ok := s.results[id]
	return result, ok
}

// OCRWorker represents a worker for OCR tasks
type OCRWorker struct {
	mq          *queue.RabbitMQ
	queueName   string
	resultStore *ResultStore
	config      ocr.OCRConfig
}

// NewOCRWorker creates a new OCR worker
func NewOCRWorker(mq *queue.RabbitMQ, queueName string, resultStore *ResultStore, config ocr.OCRConfig) *OCRWorker {
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

		// Process the OCR task
		text, err := ocr.ImageToTextWithConfig(task.ImagePath, w.config)
		if err != nil {
			return fmt.Errorf("OCR processing failed: %w", err)
		}

		// Store the result
		w.resultStore.Set(task.ResultID, text)
		log.Printf("OCR task completed for ID: %s", task.ResultID)

		return nil
	})
}

// TranslationWorker represents a worker for translation tasks
type TranslationWorker struct {
	mq          *queue.RabbitMQ
	queueName   string
	resultStore *ResultStore
	config      translator.TranslationConfig
}

// NewTranslationWorker creates a new translation worker
func NewTranslationWorker(mq *queue.RabbitMQ, queueName string, resultStore *ResultStore, config translator.TranslationConfig) *TranslationWorker {
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

		// Process the translation task
		translatedText, err := translator.TranslateWithConfig(task.Text, w.config)
		if err != nil {
			return fmt.Errorf("translation failed: %w", err)
		}

		// Store the result
		w.resultStore.Set(task.ResultID, translatedText)
		log.Printf("Translation task completed for ID: %s", task.ResultID)

		return nil
	})
}

// PDFWorker represents a worker for PDF tasks
type PDFWorker struct {
	mq          *queue.RabbitMQ
	queueName   string
	resultStore *ResultStore
	config      pdf.PDFConfig
}

// NewPDFWorker creates a new PDF worker
func NewPDFWorker(mq *queue.RabbitMQ, queueName string, resultStore *ResultStore, config pdf.PDFConfig) *PDFWorker {
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

		// Process the PDF task
		pdfPath, err := pdf.CreatePDFWithConfig(task.Text, w.config)
		if err != nil {
			return fmt.Errorf("PDF creation failed: %w", err)
		}

		// Store the result (path to the PDF)
		w.resultStore.Set(task.ResultID, pdfPath)
		log.Printf("PDF task completed: %s", pdfPath)

		return nil
	})
}

// StartWorkers starts all workers
func StartWorkers(rabbitmqURL string) (*queue.RabbitMQ, *ResultStore, error) {
	// Connect to RabbitMQ
	mq, err := queue.NewRabbitMQ(rabbitmqURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Create result store
	resultStore := NewResultStore()

	// Create and start OCR worker
	ocrWorker := NewOCRWorker(mq, "ocr_queue", resultStore, ocr.DefaultOCRConfig())
	go func() {
		if err := ocrWorker.Start(); err != nil {
			log.Printf("OCR worker error: %v", err)
			os.Exit(1)
		}
	}()

	// Create and start translation worker
	translationWorker := NewTranslationWorker(mq, "translation_queue", resultStore, translator.DefaultTranslationConfig())
	go func() {
		if err := translationWorker.Start(); err != nil {
			log.Printf("Translation worker error: %v", err)
			os.Exit(1)
		}
	}()

	// Create and start PDF worker
	pdfWorker := NewPDFWorker(mq, "pdf_queue", resultStore, pdf.DefaultPDFConfig())
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
