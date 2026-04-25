.PHONY: build test run clean install docker dashboard dashboard-dev dashboard-deps smoke

BIN := bin/proxyhub
VERSION := 0.3.0
WEB_DIR := internal/dashboard/web

# 完整构建：先 dashboard，再 Go
build: dashboard
	go build -o $(BIN) ./cmd/proxyhub

# 仅 Go（dashboard 必须已经 build 过，否则 fallback HTML）
build-go:
	go build -o $(BIN) ./cmd/proxyhub

# 构建 React dashboard
dashboard: dashboard-deps
	cd $(WEB_DIR) && pnpm build

# 安装前端依赖（首次或 package.json 改动后）
dashboard-deps:
	@if [ ! -d $(WEB_DIR)/node_modules ]; then \
		echo "Installing dashboard dependencies..."; \
		cd $(WEB_DIR) && pnpm install; \
	fi

# 开发模式：vite hot reload，proxy /api 到 :7001
dashboard-dev: dashboard-deps
	@echo "Vite dev server on :5173, proxying /api to localhost:7001"
	@echo "Make sure proxyhub is running: ./bin/proxyhub serve"
	cd $(WEB_DIR) && pnpm dev

test:
	go test ./...

run: build
	$(BIN) serve

clean:
	rm -rf bin/ *.db *.db-* internal/dashboard/assets/

install: dashboard
	go install ./cmd/proxyhub

docker:
	docker build -t jiusanzhou/proxyhub:$(VERSION) -t jiusanzhou/proxyhub:latest -f deploy/Dockerfile .

# 运行一次性测试：起服务 -> 拉代理 -> 停服
smoke:
	@echo "Smoke test..."
	@rm -f /tmp/proxyhub-smoke.db
	@$(BIN) serve --db /tmp/proxyhub-smoke.db --proxy-port 17100 --api-port 17101 & \
		PID=$$!; \
		sleep 5; \
		curl -s http://localhost:17101/api/v1/stats | head -c 200; \
		echo; \
		kill $$PID
