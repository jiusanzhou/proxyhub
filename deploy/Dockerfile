# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o proxyhub ./cmd/proxyhub

# Runtime stage
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /build/proxyhub /usr/local/bin/proxyhub

# 默认数据目录
RUN mkdir -p /data
VOLUME ["/data"]

EXPOSE 7000 7001

ENTRYPOINT ["proxyhub"]
CMD ["serve", "--db", "/data/proxyhub.db"]
