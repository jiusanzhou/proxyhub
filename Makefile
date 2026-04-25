.PHONY: build test run clean install docker

BIN := bin/proxyhub
VERSION := 0.1.0

build:
	go build -o $(BIN) ./cmd/proxyhub

test:
	go test ./...

run: build
	$(BIN) serve

clean:
	rm -rf bin/ *.db *.db-*

install:
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
