#!/bin/bash

# Cấu hình
NUM_REQUESTS=10
CONCURRENCY=5
QUEUE_TIMEOUT=180  # Thời gian chờ tối đa cho queue version (seconds)
DIRECT_TIMEOUT=60  # Thời gian chờ tối đa cho direct version (seconds)

# Màu sắc cho đầu ra
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Kiểm tra trạng thái của các container
echo -e "${YELLOW}Kiểm tra trạng thái dịch vụ...${NC}"
docker compose ps

# Đảm bảo worker đang chạy
if [ "$(docker compose ps -q worker)" == "" ]; then
  echo -e "${YELLOW}Worker không chạy, khởi động worker...${NC}"
  docker compose up -d worker
  # Đợi worker khởi động
  echo -e "${YELLOW}Đợi worker khởi động...${NC}"
  sleep 5
else
  echo -e "${GREEN}Worker đã sẵn sàng.${NC}"
fi

# Xóa dữ liệu Redis để đảm bảo benchmark mới
echo -e "${YELLOW}Xóa dữ liệu Redis...${NC}"
docker compose exec -T redis redis-cli FLUSHALL

# Xóa các hàng đợi RabbitMQ
echo -e "${YELLOW}Xóa hàng đợi RabbitMQ...${NC}"
docker compose exec -T rabbitmq rabbitmqctl purge_queue ocr_queue || true
docker compose exec -T rabbitmq rabbitmqctl purge_queue translation_queue || true
docker compose exec -T rabbitmq rabbitmqctl purge_queue pdf_queue || true

# Chạy benchmark không dùng hàng đợi
echo -e "${GREEN}=== Chạy benchmark KHÔNG sử dụng hàng đợi ===${NC}"
timeout $DIRECT_TIMEOUT docker compose exec -T app /app/imageprocessor -benchmark -use-queue=false -num-requests=$NUM_REQUESTS -concurrency=$CONCURRENCY -redis=redis://redis:6379

# Kiểm tra mã exit của timeout
if [ $? -eq 124 ]; then
  echo -e "${RED}Benchmark không sử dụng hàng đợi bị timeout sau $DIRECT_TIMEOUT giây${NC}"
fi

# Đợi một chút
sleep 2

# Xóa dữ liệu Redis
echo -e "${YELLOW}Xóa dữ liệu Redis...${NC}"
docker compose exec -T redis redis-cli FLUSHALL

# Chạy benchmark với hàng đợi
echo ""
echo -e "${GREEN}=== Chạy benchmark SỬ DỤNG hàng đợi ===${NC}"
echo -e "${YELLOW}Kiểm tra logs của worker container để theo dõi xử lý:${NC}"
echo -e "${YELLOW}docker compose logs -f worker${NC}"
echo ""
echo -e "${YELLOW}Đang chạy benchmark với hàng đợi (có thể mất nhiều thời gian hơn)...${NC}"

# Sử dụng timeout để đảm bảo script không chạy vô thời hạn
timeout $QUEUE_TIMEOUT docker compose exec -T app /app/imageprocessor -benchmark -use-queue=true -num-requests=$NUM_REQUESTS -concurrency=$CONCURRENCY -redis=redis://redis:6379 -rabbitmq=amqp://guest:guest@rabbitmq:5672/

# Kiểm tra mã exit của timeout
if [ $? -eq 124 ]; then
  echo -e "${RED}Benchmark sử dụng hàng đợi bị timeout sau $QUEUE_TIMEOUT giây${NC}"
  echo -e "${YELLOW}Đang kiểm tra logs worker để xem trạng thái...${NC}"
  docker compose logs --tail=20 worker
fi

echo ""
echo -e "${GREEN}Benchmark hoàn tất!${NC}"

# Hiển thị thông tin từ Redis để xác nhận kết quả
echo -e "${YELLOW}Kiểm tra số lượng khóa trong Redis:${NC}"
docker compose exec -T redis redis-cli INFO | grep keys

# Phần tùy chọn: Thống kê hàng đợi RabbitMQ
echo -e "${YELLOW}Thống kê hàng đợi RabbitMQ:${NC}"
docker compose exec -T rabbitmq rabbitmqctl list_queues 