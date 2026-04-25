# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Root directory of the project (absolute path).
ROOTDIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

BIN := bin/proxyhub
WEB_DIR := internal/dashboard/web

# go.zoe.im/x/version ldflags 自动生成（GitVersion / GitCommit / BuildDate / GitTreeState）
LDFLAGS := $(shell go run -mod=readonly go.zoe.im/x/version/gen 2>/dev/null)
TAG := $(shell go run -mod=readonly go.zoe.im/x/version/gen -v 2>/dev/null)

ifndef GODEBUG
EXTRA_LDFLAGS += -s -w
else
DEBUG_GO_GCFLAGS := -gcflags=all="-N -l"
endif

GO_BUILD_FLAGS := --ldflags '${LDFLAGS} ${EXTRA_LDFLAGS}'

.PHONY: all build build-go dashboard dashboard-dev dashboard-deps test run clean install docker smoke help fmt vet

.DEFAULT_GOAL := build

all: build

# 完整构建：先 dashboard，再 Go 二进制（带 version ldflags）
build: dashboard build-go ## 完整构建 (dashboard + 带 version 的 Go binary)

# 仅 Go（dashboard 必须已经 build 过，否则 fallback HTML）
build-go: ## 仅 Go binary (假定 dashboard 产物已存在)
	@echo "🔨 Building proxyhub $(TAG)..."
	@CGO_ENABLED=0 go build $(DEBUG_GO_GCFLAGS) $(GO_BUILD_FLAGS) -o $(BIN) ./cmd/proxyhub

# 构建 React dashboard (Vite)
dashboard: dashboard-deps ## 构建 dashboard (Vite build)
	@echo "🎨 Building dashboard..."
	@cd $(WEB_DIR) && pnpm build

# 安装前端依赖
dashboard-deps:
	@if [ ! -d $(WEB_DIR)/node_modules ]; then \
		echo "📦 Installing dashboard dependencies..."; \
		cd $(WEB_DIR) && pnpm install; \
	fi

# 开发模式：Vite hot reload，自动 proxy /api 到 :7001
dashboard-dev: dashboard-deps ## Dashboard 开发模式 (Vite HMR)
	@echo "🚀 Vite dev server on :5173 (proxying /api to :7001)"
	@echo "💡 Make sure proxyhub is running: ./bin/proxyhub serve"
	@cd $(WEB_DIR) && pnpm dev

fmt: ## go fmt
	@go fmt ./...

vet: ## go vet
	@go vet ./...

test: ## go test
	@go test ./...

run: build ## build + run
	@$(BIN) serve

clean: ## 清理 build 产物
	@rm -rf bin/ *.db *.db-* internal/dashboard/assets/

install: dashboard ## go install (带 ldflags)
	@go install $(GO_BUILD_FLAGS) ./cmd/proxyhub

docker: ## 构建 Docker 镜像
	@docker build -t jiusanzhou/proxyhub:$(TAG) -t jiusanzhou/proxyhub:latest -f deploy/Dockerfile .

# Smoke test: 起服务 -> 查 stats -> 停
smoke: build ## Smoke test
	@echo "🧪 Smoke test..."
	@rm -f /tmp/proxyhub-smoke.db
	@$(BIN) serve --db /tmp/proxyhub-smoke.db --proxy-port 17100 --api-port 17101 & \
		PID=$$!; \
		sleep 5; \
		curl -s http://localhost:17101/api/v1/stats | head -c 200; \
		echo; \
		kill $$PID

help: ## 帮助
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort
