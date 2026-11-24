# ============================================================================
# Keel 企业服务端框架 - 构建与运维入口
# 常用：make build / make run / make test / make docker / make compose
# ============================================================================

APP        := keel
MODULE     := github.com/keel-framework/keel
VERSION    ?= 1.0.0
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS    := -s -w \
	-X $(MODULE)/pkg/version.Version=$(VERSION) \
	-X $(MODULE)/pkg/version.Commit=$(COMMIT) \
	-X $(MODULE)/pkg/version.BuildTime=$(BUILD_TIME)
BIN_DIR    := bin
CONFIG     ?= configs/config.yaml
WEB_DIR    := web
PNPM       ?= pnpm

# 交叉编译目标
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.DEFAULT_GOAL := help

.PHONY: help
help: ## 显示帮助
	@awk 'BEGIN{FS=":.*##"; printf "\n用法: make <目标>\n\n"} /^[a-zA-Z_-]+:.*?##/ {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# GOBIN 优先；未设置时回退 GOPATH/bin（go install 实际写到 GOBIN）
SWAG_DIR  := $(shell go env GOBIN)
ifeq ($(SWAG_DIR),)
SWAG_DIR  := $(shell go env GOPATH)/bin
endif
SWAG      := $(SWAG_DIR)/swag
SWAG_VER  := v1.16.4

.PHONY: tidy
tidy: ## 整理依赖
	GOTOOLCHAIN=local go mod tidy

.PHONY: swag-install
swag-install: ## 安装 swag 接口文档生成工具
	GOTOOLCHAIN=local go install github.com/swaggo/swag/cmd/swag@$(SWAG_VER)

.PHONY: docs
docs: ## 从代码注解重新生成 OpenAPI 文档
	python3 scripts/gen_swagger_annotations.py
	$(SWAG) init -g internal/apidocs/general.go -d . -o internal/apidocs --parseInternal --outputTypes go,json,yaml
	@echo "已生成 internal/apidocs/{docs.go,swagger.json,swagger.yaml}"

.PHONY: fmt
fmt: ## 格式化代码
	gofmt -w .

.PHONY: vet
vet: ## 静态检查
	go vet ./...

.PHONY: test
test: ## 运行全部测试（含 race 检测与集成测试）
	go test -count=1 -race ./...

.PHONY: test-unit
test-unit: ## 仅跑单元测试（不含 test/ 集成，毫秒级）
	go test -count=1 $$(go list ./... | grep -v '/test$$')

.PHONY: test-integration
test-integration: ## 仅跑 test/ 下的集成测试
	go test -count=1 ./test/...

.PHONY: test-mysql
test-mysql: ## 用临时 MySQL 容器跑跨方言契约测试
	bash scripts/dialect_test.sh mysql

.PHONY: test-pg
test-pg: ## 用临时 PostgreSQL 容器跑跨方言契约测试
	bash scripts/dialect_test.sh postgres

.PHONY: test-dialect
test-dialect: ## 用临时 MySQL + PostgreSQL 容器跑跨方言契约测试
	bash scripts/dialect_test.sh all

.PHONY: test-regression
test-regression: ## 一键接口回归（临时起服务 + mock OAuth，跑全部脚本后清理）
	bash test/regression/run_all.sh

.PHONY: check-arch
check-arch: ## 架构约束检查（import 边界 / 方言白名单 / pkg 签名快照）
	python3 scripts/check_architecture.py

.PHONY: cover
cover: ## 生成覆盖率报告
	go test -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

.PHONY: bench
bench: ## 运行基准测试
	go test -run '^$$' -bench . -benchmem ./...

.PHONY: web-deps
web-deps: ## 安装前端依赖
	@command -v $(PNPM) >/dev/null 2>&1 || { echo "缺少 pnpm：npm i -g pnpm"; exit 1; }
	cd $(WEB_DIR) && $(PNPM) install

.PHONY: web
web: ## 构建前端产物到 web/dist
	@command -v $(PNPM) >/dev/null 2>&1 || { echo "缺少 pnpm：npm i -g pnpm"; exit 1; }
	cd $(WEB_DIR) && $(PNPM) install --frozen-lockfile && $(PNPM) build

.PHONY: web-dev
web-dev: ## 启动前端开发服务器（另开终端 make run）
	cd $(WEB_DIR) && $(PNPM) dev

.PHONY: build-go
build-go: ## 仅编译 Go（复用已有 web/dist）
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP) ./cmd/keel
	@echo "已生成 $(BIN_DIR)/$(APP)（$$(du -h $(BIN_DIR)/$(APP) | cut -f1)）"

.PHONY: build
build: web ## 构建前端 + 编译当前平台二进制
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP) ./cmd/keel
	@echo "已生成 $(BIN_DIR)/$(APP)（$$(du -h $(BIN_DIR)/$(APP) | cut -f1)）"

.PHONY: build-all
build-all: ## 交叉编译多平台
	@mkdir -p $(BIN_DIR)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		out=$(BIN_DIR)/$(APP)-$$os-$$arch; \
		[ "$$os" = "windows" ] && out=$$out.exe; \
		echo "编译 $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$out ./cmd/keel || exit 1; \
	done
	@ls -lh $(BIN_DIR)

.PHONY: run
run: ## 本地运行（默认配置）
	go run ./cmd/keel -config $(CONFIG)

.PHONY: dev
dev: ## 以 debug 模式运行
	KEEL_SERVER_MODE=debug KEEL_LOG_LEVEL=debug go run ./cmd/keel -config $(CONFIG)

.PHONY: gateway
gateway: ## 以网关形态运行
	go run ./cmd/keel -config configs/config.gateway.yaml

.PHONY: service
service: ## 以领域服务形态运行
	go run ./cmd/keel -config configs/config.service.yaml

.PHONY: docker
docker: ## 构建镜像
	docker build -t $(APP):$(VERSION) -t $(APP):latest \
		--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) .

.PHONY: compose
compose: ## 启动单机编排（MySQL + Redis + Keel）
	docker compose -f deploy/docker-compose.yml up -d --build

.PHONY: compose-down
compose-down: ## 停止编排
	docker compose -f deploy/docker-compose.yml down

.PHONY: compose-logs
compose-logs: ## 查看编排日志
	docker compose -f deploy/docker-compose.yml logs -f keel

.PHONY: verify
verify: vet check-arch test-unit ## 快速校验：静态检查 + 架构约束 + 单元测试

.PHONY: verify-full
verify-full: vet check-arch test ## 完整校验：静态检查 + 架构约束 + 全量测试（含 race + 集成）

.PHONY: clean
clean: ## 清理构建产物
	rm -rf $(BIN_DIR) coverage.out $(WEB_DIR)/dist
	@mkdir -p $(WEB_DIR)/dist && touch $(WEB_DIR)/dist/.gitkeep
