# Build stage
FROM golang:1.25.1-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o observability-demo ./examples/observability

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/observability-demo .

# Expose ports
EXPOSE 8080 9090

# Run the application
CMD ["./observability-demo"]
