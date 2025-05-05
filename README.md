# Image Text Processor

Ứng dụng Go trích xuất văn bản từ hình ảnh, dịch từ tiếng Anh sang tiếng Việt và tạo PDF với nội dung đã dịch.

## Tính năng

- **OCR (Nhận dạng ký tự quang học)**: Trích xuất văn bản từ hình ảnh sử dụng Tesseract OCR
- **Dịch thuật**: Dịch văn bản từ tiếng Anh sang tiếng Việt sử dụng Google Translate
- **Tạo PDF**: Tạo PDF định dạng tốt với hỗ trợ đầy đủ ký tự tiếng Việt
- **Cache**: Tối ưu hiệu suất bằng cách lưu cache kết quả OCR và dịch thuật
- **Bộ lọc ảnh tối ưu**: Tiền xử lý hình ảnh để cải thiện kết quả OCR
- **Message Queue**: Xử lý bất đồng bộ với RabbitMQ
- **Đánh giá hiệu năng**: So sánh hiệu suất giữa xử lý trực tiếp và qua hàng đợi

## Yêu cầu

- Go 1.24 trở lên
- Tesseract OCR (cài đặt trên hệ thống)
- ImageMagick (cho tiền xử lý hình ảnh)
- RabbitMQ (cho chế độ hàng đợi)
- Kết nối internet cho dịch thuật

### Cài đặt Tesseract OCR

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
Tải và cài đặt từ [Tesseract GitHub page](https://github.com/UB-Mannheim/tesseract/wiki)

### Cài đặt ImageMagick

#### Ubuntu/Debian
```bash
sudo apt-get install imagemagick
```

#### macOS
```bash
brew install imagemagick
```

#### Windows
Tải và cài đặt từ [ImageMagick Downloads](https://imagemagick.org/script/download.php)

### Cài đặt RabbitMQ

#### Sử dụng Docker
```bash
docker run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:management
```

#### Ubuntu/Debian
```bash
sudo apt-get install rabbitmq-server
sudo systemctl start rabbitmq-server
```

## Cài đặt

1. Clone repository
```bash
git clone https://github.com/mxngoc2104/KTPM-CS2.git
cd KTPM-CS2
```

2. Cài đặt các phụ thuộc Go
```bash
go mod download
```

## Sử dụng

### Chế độ xử lý trực tiếp
```bash
go run main.go -image data/sample.png
```

### Chế độ hàng đợi
```bash
# Khởi động worker
go run main.go -worker -rabbitmq amqp://guest:guest@localhost:5672/

# Trong terminal khác, xử lý hình ảnh thông qua hàng đợi
go run main.go -image data/sample.png -queue -rabbitmq amqp://guest:guest@localhost:5672/
```

### Chế độ benchmark
```bash
go run main.go -benchmark -image data/sample.png
```

### Cờ lệnh
- `-image`: Đường dẫn đến hình ảnh cần xử lý (mặc định: data/sample.png)
- `-queue`: Sử dụng hàng đợi thông điệp cho xử lý
- `-rabbitmq`: URL kết nối RabbitMQ (mặc định: amqp://guest:guest@localhost:5672/)
- `-worker`: Chạy ở chế độ worker
- `-benchmark`: Chạy ở chế độ benchmark

## Cấu trúc dự án

```
.
├── data/               # Hình ảnh mẫu
├── font/               # Font cho PDF
│   └── Roboto-Regular.ttf
├── output/             # PDF đã tạo
├── pkg/                # Package
│   ├── benchmark/      # Đánh giá hiệu năng
│   ├── cache/          # Cache cho OCR và dịch thuật
│   ├── ocr/            # Chức năng OCR
│   ├── pdf/            # Tạo PDF
│   ├── queue/          # Tích hợp RabbitMQ
│   ├── translator/     # Dịch thuật
│   └── worker/         # Worker xử lý hàng đợi
├── go.mod              # File mô-đun Go
├── go.sum              # Checksum phụ thuộc
└── main.go             # Điểm vào ứng dụng
```

## Kiến trúc hệ thống

### 1. Cơ chế Cache
Hệ thống sử dụng hai lớp cache:
- **Cache OCR**: Lưu kết quả OCR dựa trên hash của hình ảnh
- **Cache dịch thuật**: Lưu kết quả dịch dựa trên hash của văn bản nguồn

### 2. Tiền xử lý hình ảnh
Sử dụng ImageMagick để tối ưu hình ảnh trước khi OCR:
- Chuyển đổi sang thang xám
- Chuẩn hóa độ tương phản
- Làm sắc nét và loại bỏ nhiễu
- Tăng kích thước để cải thiện OCR

### 3. Kiến trúc Message Queue
- **OCR Queue**: Hàng đợi cho các tác vụ OCR
- **Translation Queue**: Hàng đợi cho các tác vụ dịch thuật
- **PDF Queue**: Hàng đợi cho các tác vụ tạo PDF

### 4. Đánh giá hiệu năng
Hệ thống bao gồm công cụ benchmark để so sánh hiệu suất giữa:
- Xử lý trực tiếp không cache
- Xử lý trực tiếp với cache
- Xử lý qua message queue

## Đánh giá và so sánh hiệu năng

### Kết quả benchmark trên hạ tầng điển hình
- **OCR**: Tăng tốc ~70% với cache, ~40% với tiền xử lý hình ảnh tối ưu
- **Dịch thuật**: Tăng tốc ~90% với cache
- **Tổng thể**: Tăng tốc ~80% khi sử dụng đầy đủ cache

### So sánh các cơ chế filter trong OCR
- **Không filter**: Baseline
- **Grayscale only**: Tăng tốc ~10-15%
- **Grayscale + Contrast normalization**: Tăng tốc ~20-25%
- **Đầy đủ bộ lọc**: Tăng tốc ~35-40%, độ chính xác cao hơn

### Hiệu suất theo số lượng CPU
Số worker tối ưu cho mỗi CPU:
- 1-2 core: 1 OCR, 1 Translation, 1 PDF worker
- 4 core: 2 OCR, 1 Translation, 1 PDF worker
- 8+ core: 4 OCR, 2 Translation, 2 PDF worker

## Lưu ý triển khai

### Cân nhắc phần cứng
- **CPU bound**: OCR và tiền xử lý hình ảnh
- **Network bound**: Dịch thuật (phụ thuộc API Google)
- **Memory bound**: Lưu cache

### Hướng phát triển tiếp theo
- Thêm Redis làm backend cho cache phân tán
- Tích hợp các dịch vụ dịch thuật thay thế
- Xử lý hình ảnh batch và đa luồng

## Chi tiết kỹ thuật

- **OCR Engine**: Tesseract (qua command-line)
- **Dịch thuật**: Google Translate (API không chính thức)
- **PDF Library**: gofpdf với hỗ trợ UTF-8 cho ký tự tiếng Việt
- **Message Queue**: RabbitMQ
- **Font**: Roboto Regular