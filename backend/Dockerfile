FROM golang:1.26-alpine AS builder
WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/finerag-backend ./cmd/finerag-backend

FROM alpine:3.22
WORKDIR /app
COPY --from=builder /out/finerag-backend /usr/local/bin/finerag-backend
COPY migrations /app/migrations

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/finerag-backend"]
