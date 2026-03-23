FROM golang:1.26-bookworm AS builder

# Install build dependencies for CGO and Tesseract
RUN apt-get update && apt-get install -y \
    pkg-config \
    libssl-dev \
    tesseract-ocr \
    libtesseract-dev \
    clang \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod ./
# Note: we need to run go mod download after adding extractous-go, 
# but for now we assume it will be handled during build
RUN go mod download || true

COPY . .
# Resolve missing go.sum entries for extractous-go
RUN go mod tidy

# Download and install native extractous libraries
RUN go run github.com/rahulpoonia29/extractous-go/cmd/install@latest

# Enable CGO for extractous-go bindings
# We point CGO to the downloaded native libraries
ENV CGO_CFLAGS="-I/app/native/linux_amd64"
ENV CGO_LDFLAGS="-L/app/native/linux_amd64 -lextractous_ffi -lstdc++ -Wl,-rpath,/app/native/linux_amd64"
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /out/finerag-backend ./cmd/finerag-backend

FROM debian:bookworm-slim
WORKDIR /app

# Install runtime dependencies for Tesseract
RUN apt-get update && apt-get install -y \
    ca-certificates \
    tesseract-ocr \
    libtesseract-dev \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/finerag-backend /usr/local/bin/finerag-backend
COPY --from=builder /app/native /app/native
COPY migrations /app/migrations

# Ensure the library is findable at runtime
ENV LD_LIBRARY_PATH="/app/native/linux_amd64:${LD_LIBRARY_PATH}"

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/finerag-backend"]
