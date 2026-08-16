.PHONY: help up down test test-race lint lint-tool-check markdown-check modernize modernize-check clean logs status examples examples-check example-all buf-lint api-inventory api-inventory-check api-baseline-check api-compat-check exported-comments godoc-artifacts

DEV_COMPOSE_FILE := deploy/dev/docker-compose.yml
BUF_DIR := examples
RC1_BASELINE_COMMIT := ec5ad2c31fb4adce2bd42529e3d7fbfe92b23aa7
GOLANGCI_LINT_VERSION ?= 2.12.2
GOLANGCI_LINT_VERSION_NORMALIZED := $(patsubst v%,%,$(GOLANGCI_LINT_VERSION))
MARKDOWNLINT_CLI2_VERSION ?= 0.18.1

# example-all is used by CI and must contain only self-terminating examples.
# Scenario examples that require a development stack or intentionally run as a
# service are invoked explicitly with make example-<name>.
CI_EXAMPLES := auth breaker cache clog config connector db dlock grpc-registry idem idgen mq ratelimit registry xerrors

help:
	@echo "Genesis 开发环境"
	@echo ""
	@echo "使用方法:"
	@echo "  make up        - 启动所有开发服务"
	@echo "  make down      - 停止所有开发服务"
	@echo "  make test      - 运行测试"
	@echo "  make lint      - 运行代码检查"
	@echo "  make markdown-check - 检查 Markdown 格式和仓库内链接"
	@echo "  make buf-lint  - 检查 internal example proto 定义"
	@echo "  make modernize - 运行 go fix 现代化代码"
	@echo "  make modernize-check - 检查是否存在 go fix 建议"
	@echo "  make api-inventory - 更新 v1 导出 API 清单"
	@echo "  make api-inventory-check - 检查 v1 导出 API 清单漂移"
	@echo "  make api-baseline-check - 从固定 RC1 提交重建并核验基线"
	@echo "  make api-compat-check - 检查相对 RC1 基线的已审阅兼容性"
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
	@actual_version="$$(golangci-lint version 2>/dev/null | awk '{ for (i = 1; i < NF; i++) if ($$i == "version") { print $$(i + 1); exit } }')"; \
	if [ "$$actual_version" != "$(GOLANGCI_LINT_VERSION_NORMALIZED)" ]; then \
		echo "golangci-lint 版本不匹配，需要 $(GOLANGCI_LINT_VERSION_NORMALIZED)"; \
		echo "当前版本: $${actual_version:-unknown}"; \
		echo "安装命令: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION_NORMALIZED)"; \
		exit 1; \
	fi

lint: lint-tool-check
	@echo "运行代码检查..."
	@golangci-lint run

markdown-check:
	@echo "检查 Markdown 格式..."
	@echo "Node.js $$(node --version), npm $$(npm --version)"
	@npm exec --yes --ignore-scripts --package=markdownlint-cli2@$(MARKDOWNLINT_CLI2_VERSION) -- markdownlint-cli2
	@echo "测试并检查仓库内 Markdown 链接..."
	@go test ./internal/cmd/mdlinkcheck
	@go run ./internal/cmd/mdlinkcheck .

buf-lint:
	@echo "检查 internal example proto 定义..."
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

api-baseline-check:
	@baseline_dir=$$(mktemp -d); \
	trap 'rm -rf "$$baseline_dir"' EXIT; \
	git archive $(RC1_BASELINE_COMMIT) | tar -x -C "$$baseline_dir"; \
	go run ./internal/cmd/apiinventory \
		-dir "$$baseline_dir" \
		-legacy-public-examples \
		-check docs/api-baselines/v1.0.0-rc.1.md

api-compat-check:
	@go run ./internal/cmd/apiinventory \
		-compat-baseline docs/api-baselines/v1.0.0-rc.1.md \
		-allow-breaking docs/v1-api-compat-allowlist.md \
		-expect-breaking docs/v1-api-compat-expected.md \
		-allow-removals docs/v1-api-compat-removals.md

exported-comments: lint-tool-check
	@golangci-lint run --config .golangci-doc.yml ./...

godoc-artifacts:
	@rm -rf artifacts/godoc
	@mkdir -p artifacts/godoc
	@for pkg in $$(go list ./... | grep -v '/examples/' | grep -v '/internal/'); do \
		name=$$(echo "$$pkg" | tr '/.' '__'); \
		go doc -all "$$pkg" > "artifacts/godoc/$$name.txt" || exit 1; \
	done
	@count=$$(find artifacts/godoc -type f -name '*.txt' | wc -l | tr -d ' '); \
	test "$$count" = "17" || { echo "expected 17 GoDoc artifacts, got $$count"; exit 1; }

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

# 运行可自行结束的 CI 示例
example-all:
	@echo "运行可在 CI 中完成的独立示例..."
	@for name in $(CI_EXAMPLES); do \
		echo "运行 $$name 示例..."; \
		(cd "examples/$$name" && go run main.go) || exit 1; \
	done
