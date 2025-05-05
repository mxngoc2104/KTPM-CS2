# Image Text Processor API

Ứng dụng Image Text Processor API cung cấp một RESTful API để xử lý hình ảnh, trích xuất văn bản, dịch từ tiếng Anh sang tiếng Việt và tạo tệp PDF. Ứng dụng sử dụng công nghệ OCR (Optical Character Recognition) với Tesseract, RabbitMQ cho quản lý hàng đợi tin nhắn và Redis cho bộ nhớ đệm.

## Tính năng chính

- Trích xuất văn bản từ hình ảnh (OCR) sử dụng Tesseract
- Dịch văn bản từ tiếng Anh sang tiếng Việt
- Tạo tệp PDF từ văn bản đã dịch
- Xử lý bất đồng bộ với RabbitMQ
- Lưu trữ cache bền vững với Redis
- RESTful API để tích hợp dễ dàng
- Xử lý nhiều yêu cầu đồng thời
- Công cụ benchmark để đánh giá hiệu năng

## Yêu cầu hệ thống

- Docker và Docker Compose
- Hoặc cài đặt:
  - Go 1.20 trở lên
  - Tesseract OCR
  - Python 3 với OpenCV
  - RabbitMQ
  - Redis

## Cài đặt và chạy

### Sử dụng Docker Compose (khuyến nghị)

1. Clone repository:
   ```bash
   git clone <repository-url>
   cd image-text-processor
   ```

2. Khởi động dịch vụ bằng Docker Compose:
   ```bash
   docker compose up -d
   ```

3. Truy cập API tại: http://localhost:8080

### Cài đặt thủ công

1. Cài đặt các phụ thuộc:
   ```bash
   # Ubuntu/Debian
   sudo apt-get update
   sudo apt-get install -y tesseract-ocr tesseract-ocr-eng python3-opencv redis-server
   
   # Cài đặt RabbitMQ
   sudo apt-get install -y rabbitmq-server
   ```

2. Cài đặt Go dependencies:
   ```bash
   go mod download
   ```

3. Biên dịch và chạy ứng dụng:
   ```bash
   go build -o imageprocessor
   ./imageprocessor
   ```

4. Khởi động worker trong một terminal khác:
   ```bash
   ./imageprocessor -worker
   ```

## Kiến trúc hệ thống

Hệ thống gồm các thành phần chính:

- **API Server**: Cung cấp RESTful endpoints để tải lên hình ảnh và truy xuất kết quả
- **Worker Processes**: Xử lý các công việc từ hàng đợi
- **RabbitMQ**: Quản lý hàng đợi tin nhắn để xử lý bất đồng bộ
- **Redis**: Lưu trữ bộ nhớ đệm kết quả OCR, dịch thuật và kết quả xử lý
- **Tesseract OCR**: Trích xuất văn bản từ hình ảnh
- **OpenCV**: Xử lý tiền xử lý hình ảnh để cải thiện kết quả OCR

### Redis Persistent Cache

Hệ thống sử dụng Redis làm bộ nhớ đệm bền vững cho:

1. **OCR Cache**: Lưu trữ kết quả OCR dựa trên hash của hình ảnh
2. **Translation Cache**: Lưu trữ kết quả dịch thuật dựa trên hash của văn bản
3. **Processing Results**: Lưu trữ kết quả xử lý cho các yêu cầu

Redis được cấu hình với AOF (Append Only File) và RDB snapshots để đảm bảo dữ liệu được lưu trữ bền vững ngay cả khi Redis khởi động lại. Cấu hình này được định nghĩa trong file `redis.conf`.

## REST API

### Xử lý Hình ảnh

**Yêu cầu:**

```http
POST /api/process
Content-Type: multipart/form-data

[form-data: image=@path/to/image.jpg]
```

**Phản hồi:**

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "status": "processing"
}
```

### Lấy kết quả xử lý

**Yêu cầu:**

```http
GET /api/results/{id}
```

**Phản hồi:**

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "status": "completed",
  "originalText": "This is the extracted text from the image.",
  "translatedText": "Đây là văn bản được trích xuất từ hình ảnh.",
  "pdfPath": "/output/translated-123e4567.pdf",
  "createdAt": "2023-06-15T10:30:00Z",
  "completedAt": "2023-06-15T10:30:05Z"
}
```

Các trạng thái có thể:
- `processing`: Đang xử lý
- `completed`: Hoàn thành thành công
- `failed`: Xử lý thất bại (trường `error` sẽ chứa thông tin lỗi)

### Kiểm tra trạng thái API

**Yêu cầu:**

```http
GET /api/health
```

**Phản hồi:**

```json
{
  "status": "up",
  "version": "1.0.0"
}
```

## Ví dụ sử dụng

### Sử dụng cURL

```bash
# Xử lý hình ảnh
curl -X POST -F "image=@/path/to/image.jpg" http://localhost:8080/api/process

# Lấy kết quả
curl http://localhost:8080/api/results/123e4567-e89b-12d3-a456-426614174000

# Tải xuống PDF kết quả
curl -O http://localhost:8080/output/translated-123e4567.pdf
```

### Sử dụng JavaScript/Fetch API

```javascript
// Tải lên hình ảnh
const formData = new FormData();
formData.append('image', imageFile);

fetch('http://localhost:8080/api/process', {
  method: 'POST',
  body: formData
})
.then(response => response.json())
.then(data => {
  const jobId = data.id;
  
  // Kiểm tra kết quả sau mỗi 2 giây
  const checkResult = setInterval(() => {
    fetch(`http://localhost:8080/api/results/${jobId}`)
      .then(response => response.json())
      .then(result => {
        if (result.status === 'completed') {
          clearInterval(checkResult);
          console.log('Kết quả:', result);
          // Hiển thị liên kết tải xuống PDF
          const pdfUrl = `http://localhost:8080${result.pdfPath}`;
          console.log('PDF URL:', pdfUrl);
        } else if (result.status === 'failed') {
          clearInterval(checkResult);
          console.error('Lỗi:', result.error);
        }
      });
  }, 2000);
});
```

## Benchmark và Hiệu năng

Ứng dụng bao gồm công cụ benchmark để đánh giá hiệu năng xử lý hình ảnh với và không có hàng đợi tin nhắn.

### Chạy Benchmark

```bash
# Benchmark không sử dụng hàng đợi
docker compose exec app /app/imageprocessor -benchmark -use-queue=false -num-requests=100 -concurrency=10

# Benchmark sử dụng hàng đợi
docker compose exec app /app/imageprocessor -benchmark -use-queue=true -num-requests=100 -concurrency=10
```

```bash
# Benchmark không dùng hàng đợi tin nhắn
./imageprocessor -benchmark -use-queue=false -num-requests=100 -concurrency=10

# Benchmark sử dụng hàng đợi tin nhắn
./imageprocessor -benchmark -use-queue=true -num-requests=100 -concurrency=10
```

### Tham số Benchmark

- `-num-requests`: Số lượng yêu cầu xử lý ảnh (mặc định: 100)
- `-concurrency`: Số lượng yêu cầu đồng thời (mặc định: 10)
- `-use-queue`: Sử dụng hàng đợi tin nhắn cho xử lý (mặc định: true)

### Kết quả Benchmark

Benchmark sẽ báo cáo các thông số:

- Tổng thời gian thực hiện
- Thời gian xử lý trung bình
- Thời gian xử lý tối thiểu và tối đa
- Tỷ lệ thành công
- Số yêu cầu trên giây
- Kích thước cache

### Kết quả Benchmark Mẫu

#### Không sử dụng Hàng đợi (Xử lý Trực tiếp)

```
=== Benchmark Results ===
Total requests: 100
Concurrency: 10
Queue mode: false
Total time: 20.5s
Average duration: 1.9s
Min duration: 1.2s
Max duration: 2.8s
Success rate: 100.00%
Requests per second: 4.88
OCR cache items: 1
Translation cache items: 1
```

#### Sử dụng Hàng đợi RabbitMQ

```
=== Benchmark Results ===
Total requests: 100
Concurrency: 10
Queue mode: true
Total time: 10.2s
Average duration: 1.0s
Min duration: 0.8s
Max duration: 1.5s
Success rate: 100.00%
Requests per second: 9.80
OCR cache items: 1
Translation cache items: 1
```

## Kiểm thử và gỡ lỗi

### Kiểm tra trạng thái dịch vụ

```bash
# Kiểm tra Redis
docker compose exec redis redis-cli ping

# Kiểm tra Redis AOF hoạt động
docker compose exec redis redis-cli info persistence

# Kiểm tra RabbitMQ
docker compose exec rabbitmq rabbitmqctl status

# Xem logs của ứng dụng
docker compose logs -f app

# Xem logs của worker
docker compose logs -f worker
```

### Các lệnh kiểm thử

```bash
# Thử nghiệm xử lý một hình ảnh mẫu
curl -X POST -F "image=@./data/sample.png" http://localhost:8080/api/process
```

## Tùy chỉnh và cấu hình

Ứng dụng có thể được cấu hình thông qua các biến môi trường hoặc cờ dòng lệnh:

| Tham số | Mô tả | Mặc định |
|--------|-------------|---------|
| `PORT` | Cổng máy chủ HTTP | 8080 |
| `RABBITMQ_URL` | URL kết nối RabbitMQ | amqp://guest:guest@localhost:5672/ |
| `REDIS_URL` | URL kết nối Redis | localhost:6379 |
| `USE_REDIS` | Sử dụng Redis cho bộ nhớ đệm | true |
| `CACHE_TTL` | Thời gian sống của bộ nhớ đệm | 24h |
| `RESULTS_TTL` | Thời gian sống của kết quả | 168h (7 ngày) |
