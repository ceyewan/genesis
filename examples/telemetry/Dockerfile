# 构建阶段
FROM golang:1.21-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制 go mod 文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main main.go

# 运行阶段
FROM alpine:latest

# 安装必要的工具
RUN apk --no-cache add ca-certificates curl

# 创建非root用户
RUN addgroup -g 1001 -S appuser && \
    adduser -S -u 1001 -G appuser appuser

# 设置工作目录
WORKDIR /root/

# 复制二进制文件
COPY --from=builder /app/main .

# 更改所有权
RUN chown appuser:appuser main

# 切换到非root用户
USER appuser

# 暴露端口
EXPOSE 8080 8081 9093

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/api/v1/health || exit 1

# 运行应用
CMD ["./main"]