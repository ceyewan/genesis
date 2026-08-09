.PHONY: help up down test test-race lint lint-tool-check modernize modernize-check clean logs status examples examples-check buf-lint api-inventory api-inventory-check exported-comments godoc-artifacts

DEV_COMPOSE_FILE := deploy/dev/docker-compose.yml
BUF_DIR := examples/proto
GOLANGCI_LINT_VERSION ?= 2.12.2
GOLANGCI_LINT_VERSION_NORMALIZED := $(patsubst v%,%,$(GOLANGCI_LINT_VERSION))

help:
	@echo "Genesis 开发环境"
	@echo ""
	@echo "使用方法:"
	@echo "  make up        - 启动所有开发服务"
	@echo "  make down      - 停止所有开发服务"
	@echo "  make test      - 运行测试"
	@echo "  make lint      - 运行代码检查"
	@echo "  make buf-lint  - 检查共享示例 proto 定义"
	@echo "  make modernize - 运行 go fix 现代化代码"
	@echo "  make modernize-check - 检查是否存在 go fix 建议"
	@echo "  make api-inventory - 更新 v1 导出 API 清单"
	@echo "  make api-inventory-check - 检查 v1 导出 API 清单漂移"
	@echo "  make clean     - 清理卷和网络"
	@echo "  make logs      - 显示所有服务日志"
	@echo "  make status    - 查看服务状态"
	@echo "  make examples  - 运行示例代码"
up:
	@echo "创建 genesis-net 网络（如果不存在）..."
	@docker network create genesis-net 2>/dev/null || true
	@echo "启动开发服务..."
	@docker compose -f $(DEV_COMPOSE_FILE) up -d
down:
	@echo "停止开发服务..."
	@docker compose -f $(DEV_COMPOSE_FILE) down
test:
	@echo "运行测试..."
	@go test ./...

test-race:
	@echo "运行 race tests..."
	@go test -race -count=1 ./...

lint-tool-check:
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "未安装 golangci-lint，请执行: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION_NORMALIZED)"; \
		exit 1; \
	fi
	@if ! golangci-lint version 2>/dev/null | grep -Fq "version $(GOLANGCI_LINT_VERSION_NORMALIZED)"; then \
		echo "golangci-lint 版本不匹配，需要 $(GOLANGCI_LINT_VERSION_NORMALIZED)"; \
		echo "安装命令: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION_NORMALIZED)"; \
		exit 1; \
	fi

lint: lint-tool-check
	@echo "运行代码检查..."
	@golangci-lint run

buf-lint:
	@echo "检查共享示例 proto 定义..."
	@cd $(BUF_DIR) && buf lint

modernize:
	@echo "运行 go fix..."
	@go fix ./...

modernize-check:
	@echo "检查 go fix 建议..."
	@out_file="$$(mktemp)"; \
	if ! go fix -diff ./... >"$$out_file"; then \
		cat "$$out_file"; \
		rm -f "$$out_file"; \
		exit 1; \
	fi; \
	if [ -s "$$out_file" ]; then \
		cat "$$out_file"; \
		echo ""; \
		echo "检测到可应用的 go fix 变更，请运行: go fix ./..."; \
		rm -f "$$out_file"; \
		exit 1; \
	fi; \
	rm -f "$$out_file"

api-inventory:
	@go run ./internal/cmd/apiinventory -write docs/v1-api-inventory.md

api-inventory-check:
	@go run ./internal/cmd/apiinventory -check docs/v1-api-inventory.md

exported-comments: lint-tool-check
	@golangci-lint run --config .golangci-doc.yml ./...

godoc-artifacts:
	@mkdir -p artifacts/godoc
	@for pkg in $$(go list ./... | grep -v '/examples/' | grep -v '/internal/'); do \
		name=$$(echo "$$pkg" | tr '/.' '__'); \
		go doc -all "$$pkg" > "artifacts/godoc/$$name.txt" || exit 1; \
	done
	@count=$$(find artifacts/godoc -type f -name '*.txt' | wc -l | tr -d ' '); \
	test "$$count" = "18" || { echo "expected 18 GoDoc artifacts, got $$count"; exit 1; }

examples-check:
	@go test ./examples/...

clean:
	@echo "清理卷和网络..."
	@docker compose -f $(DEV_COMPOSE_FILE) down -v
	@docker network rm genesis-net 2>/dev/null || true
logs:
	@echo "显示服务日志..."
	@docker compose -f $(DEV_COMPOSE_FILE) logs -f
status:
	@echo "查看服务状态..."
	@docker compose -f $(DEV_COMPOSE_FILE) ps
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
		if [ -f "$$d/main.go" ]; then \
			echo "运行 $$(basename $$d) 示例..."; \
			(cd "$$d" && go run main.go) || exit 1; \
		fi; \
	done
