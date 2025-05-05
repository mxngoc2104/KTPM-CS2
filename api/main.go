package main

import (
	"context"       // Thêm context cho Redis/Kafka
	"encoding/json" // Thêm để marshal Kafka message
	"fmt"
	"log" // Thêm để ghi log lỗi
	"net/http"
	"path/filepath"
	"time" // Thêm để đặt TTL cho Redis key

	"github.com/gin-contrib/cors" // Import CORS middleware
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8" // Import Redis client
	"github.com/google/uuid"
	"github.com/segmentio/kafka-go" // Import Kafka client

	"github.com/mxngoc2104/KTPM-CS2/pkg/messaging" // Import JobMessage từ package chung
)

// TODO: Di chuyển cấu hình ra nơi khác (ví dụ: env vars, file config)
const (
	kafkaBroker = "localhost:9092"
	kafkaTopic  = "image_processing_jobs"
	redisAddr   = "localhost:6379"
	uploadDir   = "../output/uploads" // Thư mục tạm lưu ảnh upload
	pdfDir      = "../output/pdfs"    // Thư mục lưu trữ PDF kết quả
	jobTTL      = time.Hour * 24      // Thời gian sống của thông tin job trong Redis (1 ngày)
)

// Biến toàn cục cho Redis client và Kafka writer (để đơn giản)
var (
	redisClient *redis.Client
	kafkaWriter *kafka.Writer
)

// Struct cho message gửi vào Kafka - Đã chuyển vào pkg/messaging
/*
type JobMessage struct {
	JobID     string `json:"job_id"`
	ImagePath string `json:"image_path"`
}
*/

func main() {
	// Khởi tạo Redis Client
	redisClient = redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   0, // Sử dụng DB mặc định
	})
	// Kiểm tra kết nối Redis
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Could not connect to Redis: %v", err)
	}
	fmt.Println("Connected to Redis")

	// Khởi tạo Kafka Writer (Producer)
	kafkaWriter = &kafka.Writer{
		Addr:     kafka.TCP(kafkaBroker),
		Topic:    kafkaTopic,
		Balancer: &kafka.LeastBytes{},
	}
	// Không cần kiểm tra kết nối Kafka ngay lập tức, writer sẽ tự động kết nối khi gửi message
	fmt.Println("Kafka writer configured")

	// Đảm bảo đóng Kafka writer khi ứng dụng thoát
	defer func() {
		if err := kafkaWriter.Close(); err != nil {
			log.Printf("Failed to close Kafka writer: %v", err)
		}
	}()

	router := gin.Default()

	// --- Thêm CORS Middleware ---
	config := cors.DefaultConfig()
	// Cho phép tất cả origins (chỉ dùng cho dev, cần cấu hình chặt hơn cho production)
	config.AllowAllOrigins = true
	// Hoặc chỉ định origin của frontend: config.AllowOrigins = []string{"http://localhost:5173"}
	config.AllowHeaders = append(config.AllowHeaders, "Authorization") // Thêm header nếu cần
	router.Use(cors.New(config))
	// --------------------------

	// Định tuyến
	router.POST("/api/upload", handleUpload)
	router.GET("/api/status/:job_id", handleStatus)     // Thêm route status
	router.GET("/api/download/:job_id", handleDownload) // Thêm route download

	fmt.Println("API Server starting on :8080")
	router.Run(":8080") // Chạy server trên cổng 8080
}

func handleUpload(c *gin.Context) {
	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Image file is required"})
		return
	}

	jobID := uuid.New().String()
	uploadPath := filepath.Join(uploadDir, fmt.Sprintf("%s-%s", jobID, filepath.Base(file.Filename))) // Sử dụng filepath.Base để tránh path traversal

	// Đảm bảo thư mục tồn tại (an toàn hơn)
	if err := c.SaveUploadedFile(file, uploadPath); err != nil {
		log.Printf("Error saving upload file for job %s: %v", jobID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save uploaded file"})
		return
	}

	fmt.Printf("Received file: %s, JobID: %s, Saved to: %s\n", file.Filename, jobID, uploadPath)

	// 1. Lưu trạng thái ban đầu vào Redis (jobID:status -> "queued")
	statusKey := fmt.Sprintf("%s:status", jobID)
	ctx := c.Request.Context() // Sử dụng context từ request
	err = redisClient.Set(ctx, statusKey, "queued", jobTTL).Err()
	if err != nil {
		log.Printf("Error setting initial status in Redis for job %s: %v", jobID, err)
		// Cân nhắc: Có nên xóa file đã upload nếu không lưu được status?
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initiate job processing (Redis error)"})
		return
	}
	fmt.Printf("Set initial status 'queued' for job %s in Redis\n", jobID)

	// 2. Chuẩn bị và gửi message vào Kafka
	jobMsg := messaging.JobMessage{ // Sử dụng struct từ package messaging
		JobID:     jobID,
		ImagePath: uploadPath, // Worker sẽ đọc file từ đường dẫn này
	}
	msgBytes, err := json.Marshal(jobMsg)
	if err != nil {
		log.Printf("Error marshaling Kafka message for job %s: %v", jobID, err)
		// Cân nhắc: Cập nhật status trong Redis thành "failed"? Xóa file?
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare job message"})
		return
	}

	err = kafkaWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(jobID), // Sử dụng jobID làm key để phân phối message (tùy chọn)
		Value: msgBytes,
	})
	if err != nil {
		log.Printf("Error sending message to Kafka for job %s: %v", jobID, err)
		// Cân nhắc: Cập nhật status trong Redis thành "failed"? Xóa file?
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to queue job for processing (Kafka error)"})
		return
	}
	fmt.Printf("Sent job %s to Kafka topic %s\n", jobID, kafkaTopic)

	c.JSON(http.StatusOK, gin.H{
		"message": "File uploaded successfully. Processing queued.", // Cập nhật message
		"job_id":  jobID,
	})
}

// --- Handler để kiểm tra trạng thái Job ---
func handleStatus(c *gin.Context) {
	jobID := c.Param("job_id")
	ctx := c.Request.Context()

	statusKey := fmt.Sprintf("%s:status", jobID)
	// pdfPathKey := fmt.Sprintf("%s:pdfpath", jobID) // Không dùng trực tiếp nữa
	errorKey := fmt.Sprintf("%s:error", jobID)
	detailsKey := fmt.Sprintf("%s:details", jobID) // Key chứa thông tin chi tiết

	// Lấy trạng thái cơ bản trước
	status, err := redisClient.Get(ctx, statusKey).Result()
	if err == redis.Nil {
		// Không tìm thấy key status -> Job không tồn tại
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}
	if err != nil {
		log.Printf("Error getting base status from Redis for job %s: %v", jobID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get job status"})
		return
	}

	response := gin.H{"job_id": jobID, "status": status}

	// Nếu hoàn thành hoặc thất bại, lấy thêm thông tin
	if status == "completed" || status == "failed" {
		// Lấy thông tin chi tiết (dạng hash map)
		details, err := redisClient.HGetAll(ctx, detailsKey).Result()
		if err != nil && err != redis.Nil {
			log.Printf("Warning: Error getting details from Redis for job %s: %v", jobID, err)
			// Tiếp tục trả về trạng thái cơ bản nếu không lấy được details
		} else if err == nil && len(details) > 0 {
			// Thêm các thông tin chi tiết vào response
			if val, ok := details["pdf_path"]; ok {
				response["pdf_path"] = val
			}
			if val, ok := details["cached"]; ok {
				response["cached"] = val == "true"
			}
			if val, ok := details["filter_ms"]; ok {
				response["filter_ms"] = val
			}
			if val, ok := details["ocr_ms"]; ok {
				response["ocr_ms"] = val
			}
			if val, ok := details["translate_ms"]; ok {
				response["translate_ms"] = val
			}
			if val, ok := details["pdf_ms"]; ok {
				response["pdf_ms"] = val
			}
		}

		// Lấy lỗi nếu thất bại (vẫn lấy từ key riêng)
		if status == "failed" {
			errorMsg, err := redisClient.Get(ctx, errorKey).Result()
			if err != nil && err != redis.Nil {
				log.Printf("Warning: Error getting error message from Redis for failed job %s: %v", jobID, err)
			} else if err == nil {
				response["error_message"] = errorMsg
			}
		}
	}

	c.JSON(http.StatusOK, response)
}

// --- Handler để tải file PDF kết quả ---
func handleDownload(c *gin.Context) {
	jobID := c.Param("job_id")
	ctx := c.Request.Context()

	statusKey := fmt.Sprintf("%s:status", jobID)
	// pdfPathKey := fmt.Sprintf("%s:pdfpath", jobID) // Không dùng trực tiếp nữa

	// Lấy trạng thái và đường dẫn PDF từ Redis
	vals, err := redisClient.MGet(ctx, statusKey).Result()
	if err != nil {
		log.Printf("Error getting download info from Redis for job %s: %v", jobID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get job details"})
		return
	}

	statusVal := vals[0]

	if statusVal == nil {
		// Không tìm thấy job
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	status := statusVal.(string)
	if status != "completed" {
		// Job chưa hoàn thành hoặc bị lỗi
		response := gin.H{"error": "Job not completed", "status": status}
		if status == "failed" {
			errorKey := fmt.Sprintf("%s:error", jobID)
			errorMsg, _ := redisClient.Get(ctx, errorKey).Result()
			if errorMsg != "" {
				response["error_message"] = errorMsg
			}
		}
		c.JSON(http.StatusBadRequest, response)
		return
	}

	// Gửi file PDF cho client
	// Đặt tên file tải về là jobID.pdf
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.pdf\"", jobID))
	c.File(pdfDir + "/" + jobID + ".pdf")
}
 