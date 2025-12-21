.PHONY: help up down test lint clean logs status examples

help:
	@echo "Genesis 开发环境"
	@echo ""
	@echo "使用方法:"
	@echo "  make up        - 启动所有开发服务"
	@echo "  make down      - 停止所有开发服务"
	@echo "  make test      - 运行测试"
	@echo "  make lint      - 运行代码检查"
	@echo "  make clean     - 清理卷和网络"
	@echo "  make logs      - 显示所有服务日志"
	@echo "  make status    - 查看服务状态"
	@echo "  make examples  - 运行示例代码"

up:
	@echo "创建 genesis-net 网络（如果不存在）..."
	@docker network create genesis-net 2>/dev/null || true
	@echo "启动开发服务..."
	@docker compose -f docker-compose.dev.yml up -d

down:
	@echo "停止开发服务..."
	@docker compose -f docker-compose.dev.yml down

test:
	@echo "运行测试..."
	@go test ./...

lint:
	@echo "运行代码检查..."
	@golangci-lint run

clean:
	@echo "清理卷和网络..."
	@docker compose -f docker-compose.dev.yml down -v
	@docker network rm genesis-net 2>/dev/null || true

logs:
	@echo "显示服务日志..."
	@docker compose -f docker-compose.dev.yml logs -f

status:
	@echo "查看服务状态..."
	@docker compose -f docker-compose.dev.yml ps

# 显示所有示例
examples:
	@echo "列出所有示例:"
	@for d in examples/*; do if [ -f "$d/main.go" ]; then echo "  - $(basename $d)"; fi; done

example-%:
	@echo "运行 $* 示例..."
	@cd examples/$* && go run main.go

# 一键运行所有示例
example-all:
	@echo "运行所有示例..."
	for d in examples/*; do \
		if [ -f "$d/main.go" ]; then \
			echo "运行 $(basename $d) 示例..."; \
			(cd "$d" && go run main.go) || true; \
		fi; \
	done