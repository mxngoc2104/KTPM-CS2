FROM golang:1.21-bullseye AS builder

WORKDIR /build

# Copy and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -o imageprocessor

# Final stage
FROM debian:bullseye-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    tesseract-ocr \
    tesseract-ocr-eng \
    libtesseract-dev \
    libleptonica-dev \
    libopencv-dev \
    python3-opencv \
    ca-certificates \
    fonts-liberation \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy application binary
COPY --from=builder /build/imageprocessor .

# Copy font files
COPY font /app/font

# Create necessary directories
RUN mkdir -p /app/data /app/output

# Set environment variables
ENV PATH="/app:${PATH}"

# Expose the application port
EXPOSE 8080

# Run the application
CMD ["/app/imageprocessor"] 