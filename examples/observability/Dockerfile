# Build stage
FROM golang:1.25.1-alpine AS builder

WORKDIR /app

# SQLite (go-sqlite3) 需要 cgo 编译
RUN apk --no-cache add build-base

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . ./

# Build binaries
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/gateway ./examples/observability/cmd/gateway
RUN CGO_ENABLED=1 GOOS=linux go build -o /out/logic ./examples/observability/cmd/logic
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/task ./examples/observability/cmd/task

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binaries from builder
COPY --from=builder /out/gateway ./gateway
COPY --from=builder /out/logic ./logic
COPY --from=builder /out/task ./task

# Expose ports (gateway http + gateway callback grpc + logic grpc)
EXPOSE 8080 9091 9090

# Default to gateway; docker-compose overrides per service
CMD ["./gateway"]
