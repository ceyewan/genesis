#!/bin/bash

# Genesis Telemetry 示例快速启动脚本

set -e

echo "🚀 Genesis Telemetry 示例环境启动器"
echo "=================================="
echo

# 检查 Docker 和 Docker Compose
if ! command -v docker &> /dev/null; then
    echo "❌ Docker 未安装，请先安装 Docker"
    exit 1
fi

if ! command -v docker-compose &> /dev/null; then
    echo "❌ Docker Compose 未安装，请先安装 Docker Compose"
    exit 1
fi

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 函数：打印状态
print_status() {
    echo -e "${GREEN}✓${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

# 检查端口是否被占用
check_ports() {
    local ports=(8080 8081 9090 9093 3000 16686)
    for port in "${ports[@]}"; do
        if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
            print_error "端口 $port 已被占用，请检查其他服务"
            exit 1
        fi
    done
    print_status "端口检查通过"
}

# 构建应用镜像
build_app() {
    echo
    echo "📦 构建应用镜像..."
    docker-compose build order-service
    print_status "应用镜像构建完成"
}

# 启动服务
start_services() {
    echo
    echo "🚀 启动服务..."
    docker-compose up -d prometheus grafana jaeger order-service
    print_status "核心服务已启动"
    
    # 等待服务启动
    echo
    echo "⏳ 等待服务启动..."
    sleep 10
    
    # 启动负载生成器（可选）
    read -p "是否启动负载生成器？(y/n): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        docker-compose up -d load-generator
        print_status "负载生成器已启动"
    fi
}

# 检查服务状态
check_services() {
    echo
    echo "🔍 检查服务状态..."
    
    services=("prometheus" "grafana" "jaeger" "order-service")
    for service in "${services[@]}"; do
        if docker-compose ps | grep -q "$service.*Up"; then
            print_status "$service 服务运行正常"
        else
            print_error "$service 服务未正常运行"
        fi
    done
}

# 显示访问信息
show_access_info() {
    echo
    echo "🌐 服务访问信息："
    echo "=================="
    echo "📊 Prometheus: http://localhost:9090"
    echo "📈 Grafana:    http://localhost:3000 (admin/admin)"
    echo "🔍 Jaeger:     http://localhost:16686"
    echo "🚀 示例应用:   http://localhost:8080"
    echo "📋 应用指标:   http://localhost:9093/metrics"
    echo
    echo "API 端点："
    echo "  POST /api/v1/orders/create - 创建订单"
    echo "  GET  /api/v1/orders/{id}/status - 查询订单状态"
    echo "  PUT  /api/v1/orders/{id}/cancel - 取消订单"
    echo "  GET  /api/v1/health - 健康检查"
    echo "  GET  /api/v1/metrics/info - 指标信息"
    echo
}

# 显示示例命令
show_examples() {
    echo "🔧 示例命令："
    echo "============"
    echo
    echo "# 创建订单"
    echo "curl -X POST http://localhost:8080/api/v1/orders/create \\"
    echo "  -H 'Content-Type: application/json' \\"
    echo "  -d '{\"user_id\": 12345, \"product\": \"iPhone\", \"amount\": 999.99}'"
    echo
    echo "# 查询 Prometheus 指标"
    echo "curl -s http://localhost:9093/metrics | grep order_"
    echo
    echo "# 查看追踪数据（Jaeger）"
    echo "open http://localhost:16686"
    echo
    echo "# 查看 Grafana 仪表板"
    echo "open http://localhost:3000"
    echo
}

# 显示监控查询
show_queries() {
    echo "📊 有用的监控查询："
    echo "=================="
    echo
    echo "# 请求速率（Prometheus）"
    echo "rate(order_requests_total[5m])"
    echo
    echo "# 错误率"
    echo "rate(order_errors_total[5m]) / rate(order_requests_total[5m]) * 100"
    echo
    echo "# 响应时间 P95"
    echo "histogram_quantile(0.95, rate(order_response_duration_seconds_bucket[5m]))"
    echo
    echo "# 活跃用户数"
    echo "active_users_total"
    echo
}

# 清理函数
cleanup() {
    echo
    echo "🧹 正在停止服务..."
    docker-compose down
    print_status "服务已停止"
}

# 主函数
main() {
    # 检查参数
    if [[ "$1" == "stop" ]]; then
        cleanup
        exit 0
    fi
    
    if [[ "$1" == "logs" ]]; then
        docker-compose logs -f
        exit 0
    fi
    
    if [[ "$1" == "status" ]]; then
        docker-compose ps
        exit 0
    fi
    
    # 显示欢迎信息
    echo "🎯 这个脚本将帮助你快速启动 Genesis Telemetry 示例环境"
    echo "   包括：示例应用、Prometheus、Grafana 和 Jaeger"
    echo
    
    # 执行步骤
    check_ports
    build_app
    start_services
    check_services
    show_access_info
    show_examples
    show_queries
    
    echo
    print_status "环境启动完成！"
    echo
    echo "💡 提示："
    echo "  - 使用 './start.sh stop' 停止所有服务"
    echo "  - 使用 './start.sh logs' 查看日志"
    echo "  - 使用 './start.sh status' 查看服务状态"
    echo
    echo "🎉 享受 Genesis Telemetry 的强大功能吧！"
    echo
}

# 错误处理
trap 'print_error "脚本执行失败"; exit 1' ERR

# 运行主函数
main "$@"