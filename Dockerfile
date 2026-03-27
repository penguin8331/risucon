# Build stage
FROM golang:1.22-bookworm AS builder

WORKDIR /build
COPY go/go.mod go/go.sum ./
RUN go mod download

COPY go/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server .

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    default-mysql-client \
    openssl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app/go
COPY --from=builder /app/server ./server
COPY sql/ ../sql/
COPY public/ ../public/

EXPOSE 8080
CMD ["./server"]
