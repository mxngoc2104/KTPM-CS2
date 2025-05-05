package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/segmentio/kafka-go"

	"github.com/mxngoc2104/KTPM-CS2/pkg/imagefilter"
	"github.com/mxngoc2104/KTPM-CS2/pkg/messaging"
	"github.com/mxngoc2104/KTPM-CS2/pkg/ocr"
	"github.com/mxngoc2104/KTPM-CS2/pkg/pdf"
	"github.com/mxngoc2104/KTPM-CS2/pkg/translator"
	// Thêm để xử lý đường dẫn file PDF
)

// TODO: Di chuyển cấu hình ra nơi khác
const (
	kafkaBroker  = "localhost:9092"
	kafkaTopic   = "image_processing_jobs"
	kafkaGroupID = "image-processor-group" // Consumer group ID
	redisAddr    = "localhost:6379"
	pdfDir       = "../output/pdfs"             // Thư mục lưu PDF (cần khớp với API)
	fontPath     = "../font/Roboto-Regular.ttf" // Đường dẫn font (cần khớp với logic PDF)
	jobTTL       = time.Hour * 24
	cacheTTL     = time.Hour * 24 * 7 // Thời gian cache hash ảnh (7 ngày)
)

// TODO: Di chuyển struct này vào package chung pkg/messaging hoặc tương tự
/*
type JobMessage struct {
	JobID     string `json:"job_id"`
	ImagePath string `json:"image_path"`
}
*/

var (
	redisClient *redis.Client
)

// --- Hàm tính SHA256 hash của file ---
func calculateFileHash(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func main() {
	// --- Khởi tạo Redis Client ---
	redisClient = redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   0,
	})
	ctxRedis, cancelRedis := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRedis()
	_, err := redisClient.Ping(ctxRedis).Result()
	if err != nil {
		log.Fatalf("WORKER: Could not connect to Redis: %v", err)
	}
	fmt.Println("WORKER: Connected to Redis")

	// --- Khởi tạo Kafka Reader (Consumer) ---
	kReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{kafkaBroker},
		GroupID:  kafkaGroupID,
		Topic:    kafkaTopic,
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})
	fmt.Printf("WORKER: Kafka reader configured for topic '%s', group '%s'\n", kafkaTopic, kafkaGroupID)

	// --- Xử lý tín hiệu OS để dừng worker một cách an toàn ---
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	ctxWorker, cancelWorker := context.WithCancel(context.Background())
	go func() {
		<-signals
		fmt.Println("\nWORKER: Received termination signal, shutting down...")
		cancelWorker() // Hủy context để dừng vòng lặp đọc Kafka
		if err := kReader.Close(); err != nil {
			log.Printf("WORKER: Failed to close Kafka reader: %v", err)
		}
	}()

	// --- Vòng lặp đọc message từ Kafka ---
	fmt.Println("WORKER: Starting message consumption loop...")
	for {
		// Sử dụng context của worker để có thể dừng vòng lặp từ bên ngoài
		m, err := kReader.ReadMessage(ctxWorker)
		if err != nil {
			if ctxWorker.Err() != nil {
				// Context bị hủy (worker đang dừng), thoát vòng lặp
				break
			}
			// Lỗi khác khi đọc message
			log.Printf("WORKER: Error reading message: %v", err)
			continue // Bỏ qua message lỗi và thử đọc message tiếp theo
		}

		fmt.Printf("WORKER: Received message at offset %d: %s = %s\n", m.Offset, string(m.Key), string(m.Value))

		var job messaging.JobMessage // Sử dụng struct từ package messaging
		if err := json.Unmarshal(m.Value, &job); err != nil {
			log.Printf("WORKER: Error unmarshaling message for key %s: %v. Skipping.", string(m.Key), err)
			// Commit message lỗi để không xử lý lại
			if err := kReader.CommitMessages(ctxWorker, m); err != nil {
				log.Printf("WORKER: failed to commit message offset %d: %v", m.Offset, err)
			}
			continue
		}

		fmt.Printf("WORKER: Processing job %s for image %s\n", job.JobID, job.ImagePath)

		// Xử lý job và lấy thông tin chi tiết
		details, processErr := processImage(ctxWorker, job.ImagePath, job.JobID)

		if processErr != nil {
			// Lỗi đã được log và trạng thái đã được cập nhật thành 'failed' bên trong processImage
			log.Printf("WORKER: Job %s failed to process.", job.JobID)
		} else {
			// Trạng thái đã được cập nhật thành 'completed' bên trong processImage
			// Lưu thêm thông tin chi tiết vào Redis
			if err := saveJobDetails(ctxWorker, job.JobID, details); err != nil {
				log.Printf("WORKER: Failed to save details for completed job %s: %v", job.JobID, err)
			}
			log.Printf("WORKER: Job %s processed successfully. Cached: %t", job.JobID, details["cached"] == "true")
		}

		// Commit message sau khi xử lý
		if err := kReader.CommitMessages(ctxWorker, m); err != nil {
			log.Printf("WORKER: failed to commit message offset %d: %v", m.Offset, err)
		}
	}

	fmt.Println("WORKER: Shut down complete.")
}

// --- Hàm xử lý chính cho một job ---
// Trả về map chứa thông tin chi tiết và lỗi nếu có
func processImage(ctx context.Context, imagePath string, jobID string) (map[string]string, error) {
	details := make(map[string]string)
	var err error

	// Đảm bảo thư mục output/pdfs tồn tại
	if err = os.MkdirAll(pdfDir, os.ModePerm); err != nil {
		errMsg := fmt.Sprintf("Cannot create PDF output directory %s: %v", pdfDir, err)
		updateJobStatus(ctx, jobID, "failed", errMsg) // Cập nhật lỗi
		return nil, fmt.Errorf(errMsg)
	}

	// --- Cache Check ---
	imageHash, err := calculateFileHash(imagePath)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to calculate image hash: %v", err)
		updateJobStatus(ctx, jobID, "failed", errMsg)
		return nil, fmt.Errorf("failed to calculate hash for job %s: %w", jobID, err)
	}
	cacheKey := fmt.Sprintf("imagehash:%s", imageHash)
	log.Printf("WORKER: Calculated image hash for job %s: %s", jobID, imageHash)

	cachedPdfPath, err := redisClient.Get(ctx, cacheKey).Result()
	if err == nil && cachedPdfPath != "" { // Cache hit!
		log.Printf("WORKER: Cache hit for job %s (image hash: %s). Using cached PDF: %s", jobID, imageHash, cachedPdfPath)
		details["pdf_path"] = cachedPdfPath
		details["cached"] = "true"
		// Cập nhật trạng thái thành công và lưu đường dẫn PDF từ cache
		if err := updateJobStatus(ctx, jobID, "completed", cachedPdfPath); err != nil {
			log.Printf("WORKER: Failed to update Redis status for cached job %s: %v", jobID, err)
			// Vẫn trả về thành công vì đã có PDF
		}
		return details, nil // Trả về thành công từ cache
	}
	if err != redis.Nil {
		// Lỗi khi truy cập Redis (không phải cache miss), log nhưng vẫn tiếp tục xử lý
		log.Printf("WORKER: Error checking image cache for job %s: %v. Proceeding without cache.", jobID, err)
	}
	// Cache miss hoặc lỗi Redis -> tiếp tục xử lý
	details["cached"] = "false"
	log.Printf("WORKER: Cache miss for job %s (image hash: %s). Processing image.", jobID, imageHash)
	// --- End Cache Check ---

	// Cập nhật trạng thái: processing
	if err = updateJobStatus(ctx, jobID, "processing", ""); err != nil {
		log.Printf("WORKER: Failed to set processing status for job %s: %v", jobID, err)
		// Tiếp tục xử lý nếu có thể
	}
	log.Printf("WORKER: Starting image processing for job %s", jobID)

	// 1. Image Filtering
	filterStartTime := time.Now()
	filteredImagePath, err := imagefilter.ApplyFilters(imagePath)
	filterDuration := time.Since(filterStartTime)
	if err != nil {
		errMsg := fmt.Sprintf("Image filtering error: %v", err)
		updateJobStatus(ctx, jobID, "failed", errMsg)
		return nil, fmt.Errorf("image filtering failed for job %s: %w", jobID, err)
	}
	details["filter_ms"] = strconv.FormatInt(filterDuration.Milliseconds(), 10)
	log.Printf("WORKER: Image filtering completed for job %s (%v). Filtered path: %s", jobID, filterDuration, filteredImagePath)

	// 2. OCR
	ocrStartTime := time.Now()
	ocrResult, err := ocr.ImageToText(filteredImagePath)
	ocrDuration := time.Since(ocrStartTime)
	if err != nil {
		ocrErrMsg := fmt.Sprintf("OCR error: %v", err)
		log.Printf("WORKER: Job %s failed at OCR step. Error: %s", jobID, ocrErrMsg)
		updateJobStatus(ctx, jobID, "failed", ocrErrMsg)
		return nil, fmt.Errorf("OCR failed for job %s: %w", jobID, err)
	}
	details["ocr_ms"] = strconv.FormatInt(ocrDuration.Milliseconds(), 10)
	log.Printf("WORKER: OCR completed for job %s (%v). Text length: %d", jobID, ocrDuration, len(ocrResult))

	// 3. Translation
	transStartTime := time.Now()
	translatedText, err := translator.Translate(ocrResult)
	transDuration := time.Since(transStartTime)
	if err != nil {
		errMsg := fmt.Sprintf("Translation error: %v", err)
		updateJobStatus(ctx, jobID, "failed", errMsg)
		return nil, fmt.Errorf("translation failed for job %s: %w", jobID, err)
	}
	details["translate_ms"] = strconv.FormatInt(transDuration.Milliseconds(), 10)
	log.Printf("WORKER: Translation completed for job %s (%v). Translated length: %d", jobID, transDuration, len(translatedText))

	// 4. PDF Generation
	pdfStartTime := time.Now()
	pdfOutputPath := filepath.Join(pdfDir, fmt.Sprintf("%s.pdf", jobID))
	tempPdfPath, err := pdf.CreatePDF(translatedText)
	if err != nil {
		errMsg := fmt.Sprintf("PDF generation error: %v", err)
		updateJobStatus(ctx, jobID, "failed", errMsg)
		return nil, fmt.Errorf("PDF generation failed for job %s: %w", jobID, err)
	}
	if tempPdfPath != pdfOutputPath {
		if err := os.Rename(tempPdfPath, pdfOutputPath); err != nil {
			errMsg := fmt.Sprintf("Failed to rename/move PDF: %v", err)
			updateJobStatus(ctx, jobID, "failed", errMsg)
			os.Remove(tempPdfPath)
			return nil, fmt.Errorf("failed to rename/move PDF for job %s: %w", jobID, err)
		}
	}
	pdfDuration := time.Since(pdfStartTime)
	details["pdf_ms"] = strconv.FormatInt(pdfDuration.Milliseconds(), 10)
	details["pdf_path"] = pdfOutputPath // Lưu đường dẫn cuối cùng
	log.Printf("WORKER: PDF generation completed for job %s (%v). Output: %s", jobID, pdfDuration, pdfOutputPath)

	// 5. Update Redis on Success
	if err = updateJobStatus(ctx, jobID, "completed", pdfOutputPath); err != nil {
		log.Printf("WORKER: Failed to update final status in Redis for job %s after success: %v", jobID, err)
		// Vẫn trả về thành công vì đã có PDF
	}

	// Lưu cache hash ảnh -> pdfPath
	if err := redisClient.Set(ctx, cacheKey, pdfOutputPath, cacheTTL).Err(); err != nil {
		log.Printf("WORKER: Failed to save image hash cache for job %s (hash: %s): %v", jobID, imageHash, err)
	}

	log.Printf("WORKER: Finished processing job %s successfully.", jobID)
	return details, nil
}

// --- Hàm cập nhật trạng thái Job cơ bản vào Redis ---
// Chỉ cập nhật status, pdfpath, error
func updateJobStatus(ctx context.Context, jobID, status, result string) error {
	pipe := redisClient.Pipeline()
	statusKey := fmt.Sprintf("%s:status", jobID)
	pdfPathKey := fmt.Sprintf("%s:pdfpath", jobID)
	errorKey := fmt.Sprintf("%s:error", jobID)

	pipe.Set(ctx, statusKey, status, jobTTL)

	if status == "completed" {
		pipe.Set(ctx, pdfPathKey, result, jobTTL)
		pipe.Del(ctx, errorKey)
	} else if status == "failed" {
		pipe.Set(ctx, errorKey, result, jobTTL)
		pipe.Del(ctx, pdfPathKey)
	} else {
		// Xóa các kết quả cũ nếu trạng thái là processing/queued
		pipe.Del(ctx, pdfPathKey, errorKey)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Printf("WORKER: Error executing Redis status pipeline for job %s: %v", jobID, err)
	}
	log.Printf("WORKER: Updated job %s status to '%s' in Redis", jobID, status)
	return err
}

// --- Hàm lưu thông tin chi tiết của Job vào Redis ---
func saveJobDetails(ctx context.Context, jobID string, details map[string]string) error {
	if details == nil {
		return nil // Không có gì để lưu
	}
	pipe := redisClient.Pipeline()
	// Sử dụng HMSet để lưu map vào một hash key duy nhất cho gọn
	detailsKey := fmt.Sprintf("%s:details", jobID)
	pipe.HMSet(ctx, detailsKey, details)
	pipe.Expire(ctx, detailsKey, jobTTL) // Đặt TTL cho hash key

	/* // Cách cũ: Lưu từng key riêng lẻ
	for key, value := range details {
		redisKey := fmt.Sprintf("%s:%s", jobID, key) // Ví dụ: jobID:ocr_ms
		pipe.Set(ctx, redisKey, value, jobTTL)
	}
	*/

	_, err := pipe.Exec(ctx)
	return err
}
