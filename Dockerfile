# Build stage
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o socks5-pool .

# Run stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/socks5-pool .
EXPOSE 1080 8080
CMD ["./socks5-pool", "-listen", "0.0.0.0:1080", "-status", "0.0.0.0:8080"]
