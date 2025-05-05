# Ứng dụng Xử lý Văn bản Hình ảnh (Image Text Processor - Web Version)

## 1. Giới thiệu

Dự án này là một ứng dụng web hoàn chỉnh cho phép người dùng tải lên hình ảnh, ứng dụng sẽ tự động trích xuất văn bản tiếng Anh từ hình ảnh đó, dịch sang tiếng Việt và tạo ra một file PDF chứa nội dung đã dịch. Hệ thống được thiết kế với kiến trúc microservice/phân tán sử dụng hàng đợi tin nhắn (message queue) và bộ đệm (cache) để đảm bảo khả năng mở rộng và hiệu suất.

Đây là phiên bản nâng cấp từ một công cụ dòng lệnh ban đầu, bổ sung giao diện người dùng web, cơ chế xử lý bất đồng bộ và các kỹ thuật tối ưu hóa.

## 2. Mục tiêu và Yêu cầu Thiết kế

Ứng dụng được xây dựng nhằm đáp ứng các yêu cầu chính sau:

*   **Triển khai Web:** Xây dựng giao diện web thân thiện cho người dùng cuối.
*   **Xử lý Bất đồng bộ:** Tách biệt quá trình xử lý tốn thời gian (OCR, dịch thuật, tạo PDF) khỏi luồng yêu cầu chính để cải thiện trải nghiệm người dùng và khả năng chịu lỗi.
*   **Tối ưu Hiệu năng:**
    *   Sử dụng **Persistent Cache** để lưu trữ kết quả xử lý cho các hình ảnh đã được xử lý trước đó, tránh việc xử lý lại tốn kém.
    *   Lựa chọn phương pháp **tiền xử lý ảnh (Filter)** phù hợp với đặc thù dữ liệu và cân bằng giữa độ chính xác OCR và hiệu năng trên hạ tầng phần cứng cụ thể.
*   **Kiến trúc Hướng dịch vụ:** Sử dụng các thành phần độc lập (API, Worker) giao tiếp qua Message Queue.
*   **Giữ nguyên Thư viện Cốt lõi:** Tận dụng các module OCR, Translator, PDF đã có từ phiên bản gốc.
*   **Đánh giá Hiệu năng:** Có kế hoạch và phương pháp để đo lường, đánh giá hiệu năng của hệ thống.

## 3. Kiến trúc Hệ thống

Hệ thống bao gồm các thành phần chính sau:

```
+-----------------+      +-----------------+      +-----------------+      +-----------------+
| Frontend (React)| ---> |   API (Go/Gin)  | ---> |   Kafka Queue   | ---> |  Worker (Go)    |
| - Upload UI     |      | - Handle Upload |      | - Job Messages  |      | - Consume Jobs  |
| - Status Poll   |      | - Check Status  |      +-----------------+      | - Image Filter  |
| - Download Link | <--- | - Provide PDF   |             ^      |        | - OCR           |
+-----------------+      | - Interact Redis|             |      |        | - Translator    |
                         +--------+--------+             |      |        | - PDF Generation|
                                  |                      |      |        | - Interact Redis|
                                  v                      |      v        +--------+--------+
                         +--------+--------+             |               |
                         |   Redis Cache   | <-----------+---------------+
                         | - Job Status    |
                         | - Job Details   |
                         | - Image Hash Cache|
                         +-----------------+
```

**Luồng xử lý chính:**

1.  **Upload:** Người dùng chọn ảnh trên giao diện **Frontend** và nhấn Upload.
2.  **API tiếp nhận:** **API Service** (xây dựng bằng Go và Gin) nhận request, lưu tạm file ảnh, tạo `job_id` duy nhất.
3.  **Cache Check (API):** API tính toán SHA256 hash của ảnh và kiểm tra **Redis Cache** xem hash này đã có kết quả PDF chưa.
4.  **Enqueue Job:**
    *   Nếu cache miss, API lưu trạng thái ban đầu (`queued`) vào Redis và gửi một message chứa `job_id` và đường dẫn ảnh vào **Kafka Queue**.
    *   Nếu cache hit, API cập nhật trạng thái job là `completed` (cached) trong Redis và không gửi message vào Kafka.
5.  **API phản hồi:** API trả về `job_id` cho Frontend.
6.  **Worker xử lý:** **Worker Service** (viết bằng Go) lắng nghe Kafka Queue. Khi nhận được message:
    *   Cập nhật trạng thái job thành `processing` trong Redis.
    *   Thực hiện các bước:
        *   **Image Filtering:** Áp dụng bộ lọc ảnh (hiện tại là Grayscale).
        *   **OCR:** Gọi Tesseract để trích xuất văn bản.
        *   **Translation:** Dịch văn bản sang tiếng Việt.
        *   **PDF Generation:** Tạo file PDF kết quả.
    *   Đo thời gian thực thi từng bước.
    *   Lưu kết quả (đường dẫn PDF), trạng thái (`completed`/`failed`), thông tin chi tiết (thời gian xử lý, trạng thái cache) vào Redis.
    *   Lưu mapping `imagehash:{hash} -> pdfPath` vào Redis cache.
7.  **Frontend theo dõi:** Frontend sử dụng `job_id` để định kỳ gọi API `/api/status/:job_id` (polling).
8.  **API trả trạng thái:** API đọc trạng thái và thông tin chi tiết từ Redis và trả về cho Frontend.
9.  **Download:** Khi trạng thái là `completed`, Frontend hiển thị thông tin chi tiết và nút Download. Người dùng nhấn nút để tải file PDF qua API `/api/download/:job_id`.

## 4. Công nghệ Sử dụng

*   **Backend:** Go (Golang), Gin Web Framework
*   **Frontend:** React (với Vite), TypeScript, Axios
*   **Message Queue:** Apache Kafka
*   **Cache & State Store:** Redis
*   **OCR Engine:** Tesseract OCR
*   **Image Processing:** Go `image` package, `disintegration/imaging` (cho Grayscale)
*   **PDF Generation:** `jung-kurt/gofpdf`
*   **Infrastructure:** Docker, Docker Compose
*   **Build & Dependencies (Go):** Go Modules, Go Workspaces (`go.work`)

## 5. Chuẩn bị Môi trường (Prerequisites)

*   **Git:** Để clone repository.
*   **Go:** Phiên bản 1.21 trở lên (kiểm tra `go version`).
*   **Node.js và npm:** Để xây dựng và chạy frontend (kiểm tra `node -v` và `npm -v`).
*   **Docker và Docker Compose:** Để chạy Kafka và Redis (kiểm tra `docker --version` và `docker-compose --version`).
*   **Tesseract OCR:** **Phải được cài đặt trên hệ thống** (phiên bản 4+ được khuyến nghị).
    *   **macOS (dùng Homebrew):** `brew install tesseract tesseract-lang`
    *   **Ubuntu/Debian:** `sudo apt-get update && sudo apt-get install tesseract-ocr tesseract-ocr-eng`
    *   **Windows:** Tải từ [Tesseract at UB Mannheim](https://github.com/UB-Mannheim/tesseract/wiki). Đảm bảo thư mục cài đặt Tesseract được thêm vào biến môi trường `PATH`.
    *   Kiểm tra cài đặt: `tesseract --version`

## 6. Cài đặt và Khởi chạy

1.  **Clone Repository:**
    ```bash
    git clone https://github.com/mxngoc2104/KTPM-CS2.git
    cd KTPM-CS2
    ```
2.  **Khởi chạy Infrastructure (Kafka, Redis):**
    ```bash
    docker-compose up -d
    ```
    *(Đợi khoảng 1 phút để các container khởi động hoàn toàn)*

3.  **Đồng bộ Go Workspace:**
    ```bash
    go work sync
    ```

4.  **Cài đặt Dependencies cho Frontend:**
    ```bash
    cd frontend
    npm install
    cd ..
    ```

5.  **Khởi chạy các Services (mở các terminal riêng biệt tại thư mục gốc `KTPM-CS2`):**
    *   **Terminal 1 (Worker):**
        ```bash
        go run github.com/mxngoc2104/KTPM-CS2/worker
        ```
    *   **Terminal 2 (API):**
        ```bash
        go run github.com/mxngoc2104/KTPM-CS2/api
        ```
    *   **Terminal 3 (Frontend):**
        ```bash
        cd frontend
        npm run dev
        ```

6.  **Truy cập Ứng dụng:** Mở trình duyệt và truy cập vào địa chỉ được cung cấp bởi `npm run dev` (thường là `http://localhost:5173`).

## 7. Hướng dẫn Sử dụng

1.  Truy cập giao diện web qua trình duyệt.
2.  Nhấn nút "Choose File" (hoặc tương tự) để chọn một file ảnh từ máy tính của bạn.
3.  Nhấn nút "Upload Image".
4.  Quan sát mục "Processing Status":
    *   Job ID sẽ xuất hiện.
    *   Trạng thái sẽ cập nhật từ `Uploading...` -> `queued` -> `processing`.
    *   Khi hoàn thành, trạng thái sẽ là `completed` hoặc `failed`.
5.  Nếu trạng thái là `completed`:
    *   Thông tin chi tiết về thời gian xử lý (OCR, Translate, PDF) và trạng thái cache sẽ hiển thị.
    *   Nút "Download PDF" sẽ xuất hiện. Nhấn nút này để tải file kết quả.
6.  Nếu trạng thái là `failed`:
    *   Thông báo lỗi sẽ hiển thị.

## 8. Chi tiết Kỹ thuật

*   **Xử lý Bất đồng bộ (Kafka):** Việc sử dụng Kafka giúp API phản hồi ngay lập tức sau khi nhận upload, không cần chờ xử lý xong. Worker có thể được scale độc lập để xử lý nhiều job hơn. Message `JobMessage` chứa `jobID` và `imagePath` được gửi đi.
*   **Quản lý Trạng thái và Cache (Redis):**
    *   Redis được dùng để lưu trạng thái tức thời của mỗi job (`{jobID}:status`).
    *   Thông tin chi tiết khi job hoàn thành (thời gian, cache status, pdf path) được lưu vào một Redis Hash (`{jobID}:details`).
    *   Cache kết quả dựa trên nội dung ảnh: SHA256 hash của ảnh được tính và lưu vào key `imagehash:{hash}` với giá trị là đường dẫn PDF đã xử lý. `cacheTTL` được áp dụng.
    *   Các key Redis có `jobTTL` để tự động dọn dẹp.
*   **Tiền xử lý Ảnh (Filter):**
    *   Hiện tại, hệ thống áp dụng bộ lọc **Grayscale** (chuyển ảnh xám) sử dụng thư viện `bild` trước khi đưa vào OCR.
    *   **Lựa chọn tối ưu:** Qua thử nghiệm, các bộ lọc phức tạp hơn (Median, Otsu Binarization, Adaptive Thresholding) hoặc các chế độ PSM khác nhau của Tesseract không cho thấy sự cải thiện đáng kể hoặc thậm chí làm giảm chất lượng nhận dạng chữ số trên bộ dữ liệu thử nghiệm, đồng thời tăng chi phí xử lý. Do đó, cấu hình đơn giản (Grayscale + PSM mặc định) được chọn là phương án cân bằng tốt nhất giữa hiệu năng và độ chính xác cho yêu cầu hiện tại và hạ tầng phần cứng giả định. Việc này cần được đánh giá lại nếu có bộ dữ liệu hoặc yêu cầu phần cứng khác.
*   **Xử lý Lỗi:** Các lỗi trong quá trình xử lý (OCR, Translate, PDF, Redis, Kafka) được ghi log và cập nhật vào trạng thái job trong Redis (`failed` cùng với `error_message`). Frontend sẽ hiển thị các lỗi này cho người dùng.

## 9. Đánh giá Hiệu năng (Kế hoạch)

*   **Mục tiêu:** Đo lường thông lượng (requests/giây), độ trễ (thời gian phản hồi API, thời gian xử lý đầu cuối), tỷ lệ cache hit, và mức sử dụng tài nguyên (CPU, RAM) của các thành phần hệ thống dưới các mức tải khác nhau.
*   **Phương pháp:**
    *   Sử dụng công cụ kiểm thử tải (ví dụ: `k6`) để mô phỏng nhiều người dùng gửi yêu cầu upload đồng thời.
    *   Sử dụng công cụ profiling của Go (`pprof`) để phân tích hiệu năng chi tiết của API và Worker.
    *   Sử dụng `docker stats` để theo dõi tài nguyên của container.
*   **Kịch bản:** Thực hiện các bài test với số lượng người dùng ảo tăng dần, sử dụng các ảnh mẫu đa dạng, và kiểm tra cả trường hợp cache hit và cache miss.
*   **Kết quả dự kiến:** Xác định được giới hạn hiệu năng của hệ thống, các điểm nghẽn cổ chai tiềm ẩn, và hiệu quả thực tế của cơ chế cache.

## 10. Hướng Phát triển Tương lai (Tùy chọn)

*   Sử dụng WebSocket thay vì polling để cập nhật trạng thái job theo thời gian thực.
*   Thêm cơ chế retry cho các bước xử lý có thể thất bại tạm thời (ví dụ: lỗi mạng khi dịch).
*   Triển khai các bộ lọc ảnh nâng cao hơn và cho phép người dùng chọn lựa.
*   Cải thiện giao diện người dùng.
*   Đóng gói ứng dụng bằng Docker cho từng service (API, Worker, Frontend) để dễ dàng triển khai trên các môi trường khác nhau (ví dụ: Kubernetes).
*   Thêm cơ chế dọn dẹp file ảnh gốc và ảnh đã lọc trong thư mục `uploads`.