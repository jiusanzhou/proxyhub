# proxyhub

[![Go Reference](https://pkg.go.dev/badge/github.com/jiusanzhou/proxyhub.svg)](https://pkg.go.dev/github.com/jiusanzhou/proxyhub)
[![Go Report Card](https://goreportcard.com/badge/github.com/jiusanzhou/proxyhub)](https://goreportcard.com/report/github.com/jiusanzhou/proxyhub)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/jiusanzhou/proxyhub/actions/workflows/ci.yml/badge.svg)](https://github.com/jiusanzhou/proxyhub/actions)

> 聚合代理池微服务 - 多源免费代理聚合 + 健康度评估 + 智能轮转 + Session 粘性

兼容 Bright Data / SmartProxy / Oxylabs 的标准代理接口语义。一个二进制，零外部依赖，自部署。

**自带 Web Dashboard**（Vite + React + TypeScript，build 产物 go:embed 进二进制，运行时无外部依赖）— 启动后访问 `http://localhost:7001/` 即可。

## 设计

```
┌─────────────────┐       ┌──────────────────┐       ┌──────────────────┐
│  Source 拉取    │──>──> │   Pool 管理       │──>──> │   消费方         │
│  - Proxifly     │       │ - 健康度评分      │       │ - HTTP forward   │
│  - TextSource   │       │ - 国家/协议筛选   │       │ - REST API       │
│  (可扩展)        │       │ - TopN 随机挑选   │       │ - Go SDK         │
└─────────────────┘       └──────────┬───────┘       └──────────────────┘
                                     │
                                     ▼
                          ┌──────────────────┐
                          │ SQLite 持久化     │
                          │ (零依赖)          │
                          └──────────────────┘
```

## 特性

- 🚀 **零外部依赖**：单 Go 二进制 + SQLite
- 🌐 **多源聚合**：proxifly（3000+ 代理，每 5 分钟更新）+ 自定义文本订阅
- 🔬 **主动健康探测**：后台定期 L4/L7 探测，自动标记不可用，记录真实延迟
- 📊 **健康度评分**：`score = 成功率 * 0.6 + 延迟分 * 0.4`
- 🔁 **智能轮转**：TopN 高分代理中随机选，避免热门代理被打爆
- 🛡️ **失败兜底**：单代理冷却 + 自动重试 + 池空降级直连
- 🎯 **三种接入**：HTTP 前向代理 / REST API / Go SDK
- 📈 **可观测**：Prometheus metrics + JSON stats

## 快速开始

```bash
# 启动服务
proxyhub serve

# 默认监听：
#   :7000 - HTTP 前向代理
#   :7001 - REST API + Prometheus

# 自定义
proxyhub serve --proxy-port 8000 --api-port 8001 --db /var/lib/proxyhub.db
```

## 三种使用方式

### 1️⃣ HTTP 前向代理（最简）

```bash
# 任意语言、任意工具，把 proxyhub 当前向代理
curl -x http://localhost:7000 https://api.example.com

# 通过 header 传递偏好
curl -x http://localhost:7000 \
  -H "X-Proxyhub-Country: CN" \
  -H "X-Proxyhub-Prefer-Asian: true" \
  https://push2.eastmoney.com/api/qt/clist/get
```

支持的请求头：

| Header | 说明 |
|---|---|
| `X-Proxyhub-Country` | ISO 国家代码（CN/US/HK/...） |
| `X-Proxyhub-Protocol` | http/https/socks4/socks5 |
| `X-Proxyhub-Prefer-Asian` | true 时优先亚洲代理 |
| `X-Proxyhub-HTTPS-Only` | true 时只用 HTTPS 代理 |
| `X-Proxyhub-Top-N` | 在前 N 个高分中随机（默认 20） |
| `X-Proxyhub-Session` | 粘性会话 ID（同 ID 复用同一 IP） |
| `X-Proxyhub-TTL` | 会话存活时间（如 `10m`，默认 10m） |
| `X-Proxyhub-Rotate` | true 时强制轮转该 session 的 IP |

**响应头**（每次请求都会附带，业务可读取审计/日志）：

| Header | 说明 |
|---|---|
| `X-Proxyhub-Proxy` | 实际使用的代理 URL |
| `X-Proxyhub-Country` | 代理出口国家 |
| `X-Proxyhub-Latency-Ms` | 代理侧本次请求延迟 |
| `X-Proxyhub-Session` | 回显 session ID |
| `X-Proxyhub-Rotated` | true=本次刚轮转过 IP |
| `X-Proxyhub-Attempts` | 实际尝试次数（含重试） |

#### Session 粘性 IP

```bash
# 同一 session ID 自动绑定同一出口 IP，直到 TTL 过期或失败超阈值
curl -x http://localhost:7000 \
  -H "X-Proxyhub-Session: my-task-123" \
  -H "X-Proxyhub-TTL: 30m" \
  http://example.com/api/login

# 后续请求自动复用同一 IP
curl -x http://localhost:7000 \
  -H "X-Proxyhub-Session: my-task-123" \
  http://example.com/api/data

# 强制换 IP（同 session）
curl -x http://localhost:7000 \
  -H "X-Proxyhub-Session: my-task-123" \
  -H "X-Proxyhub-Rotate: true" \
  http://example.com/api/data

# Bright Data / SmartProxy 兼容用户名格式
curl -x http://user-session-myid-country-CN:any@localhost:7000 \
  http://example.com
```

session 自动失败轮转：单 session 内代理连续失败 N 次（默认 3）会自动绑定新 IP，
失败 1-2 次只是在 pool 内换代理，不破坏 session 语义。

### 2️⃣ REST API

```bash
# 获取一个代理
curl 'http://localhost:7001/api/v1/pick?country=CN&prefer_asian=1'
# → {"url":"http://1.2.3.4:8080","country":"CN","score":0.85,...}

# 上报使用结果（可选，用于健康度统计）
curl -X POST http://localhost:7001/api/v1/report \
  -H 'Content-Type: application/json' \
  -d '{"proxy":"http://1.2.3.4:8080","success":true,"latency_ms":234}'

# 池统计
curl http://localhost:7001/api/v1/stats
# → {"total":3336,"available":3336,"by_country":{"CN":186,"HK":14,...}}

# 列表（支持 country / available / sort / limit 过滤）
curl 'http://localhost:7001/api/v1/proxies?country=CN&sort=score&limit=10'

# 立即触发刷新
curl -X POST http://localhost:7001/api/v1/refresh

# Session 管理
curl http://localhost:7001/api/v1/sessions                            # 列表
curl -X POST http://localhost:7001/api/v1/sessions \
  -d '{"id":"task-1","country":"CN","ttl":"30m"}'                     # 创建/查看
curl -X POST 'http://localhost:7001/api/v1/sessions/rotate?id=task-1' # 强制轮转
curl -X DELETE 'http://localhost:7001/api/v1/sessions?id=task-1'      # 删除

# Prometheus 指标
curl http://localhost:7001/metrics

# Healthz
curl http://localhost:7001/healthz
```

### 3️⃣ Go SDK

```go
import "go.zoe.im/proxyhub/pkg/client"

// 方式 A: HTTP 前向代理
httpClient := client.NewHTTPClient("http://localhost:7000", &client.PickOpts{
    Country: "CN", PreferAsian: true,
})
resp, _ := httpClient.Get("https://api.example.com")

// 方式 B: REST API
api := client.NewAPI("http://localhost:7001")
proxy, _ := api.Pick(ctx, &client.PickOpts{Country: "CN"})
// ... 自己用 proxy.URL 构造客户端
api.Report(ctx, proxy.URL, true, 234*time.Millisecond)

stats, _ := api.Stats(ctx)
fmt.Println(stats.Total, stats.Available)
```

## 配置

支持三种方式，优先级：**flag > env > config file > 默认值**。

### 1. 配置文件（推荐生产使用）

```bash
proxyhub serve --config /etc/proxyhub.yaml
```

支持 YAML / TOML / JSON，示例 `proxyhub.yaml`：

```yaml
proxy_port: 7000
api_port: 7001
db: /var/lib/proxyhub.db
log_level: info

refresh_interval: 10m
fail_cooldown: 5m

# 额外文本订阅源: name=url:proto;...
extra_source: ""

# 健康探测
check_enabled: true
check_interval: 60s
check_dial_timeout: 5s
check_http_timeout: 8s
check_concurrency: 50
check_l7: false
check_target: httpbin.org:80
check_ban_on_fail: 3
```

### 2. 环境变量

任何字段都可用全大写下划线形式设置：

```bash
PROXY_PORT=8000 API_PORT=8001 DB=/tmp/p.db proxyhub serve
CHECK_CONCURRENCY=200 CHECK_L7=true proxyhub serve
```

### 3. 命令行 flag

完整列表 `proxyhub serve --help`，主要字段：

```
--proxy-port int             HTTP 前向代理端口 (默认 7000)
--api-port int               REST API + Prometheus 端口 (默认 7001)
--db string                  SQLite 数据库路径 (默认 ./proxyhub.db)
--refresh-interval dur       代理池刷新间隔 (默认 10m)
--fail-cooldown dur          失败代理冷却时间 (默认 5m)
--log-level string           日志级别 debug/info/warn/error (默认 info)
--extra-source string        额外文本订阅源 name=url:proto，多个用 ; 分隔

# 健康探测
--check                      启用后台健康探测 (默认 true)
--check-interval dur         整轮探测间隔 (默认 60s)
--check-dial-timeout dur     L4 TCP dial 超时 (默认 5s)
--check-http-timeout dur     L7 HTTP CONNECT 探测超时 (默认 8s)
--check-concurrency int      探测并发度 (默认 50)
--check-l7                   启用 L7 HTTP CONNECT 探测 (默认 false)
--check-target string        L7 探测目标 (默认 httpbin.org:80)
--check-ban-on-fail int      连续失败多少次标记 banned (默认 3)
```

实测：3336 代理 × 200 并发 × 3s 超时 = 21 秒/轮。

## Web Dashboard

启动后访问 `http://localhost:7001/`，零构建步骤，纯嵌入静态资源（go:embed）。

![Dashboard](docs/dashboard.png)

**能看到**：

- 实时 metrics：总量 / 可用 / 已 ban / 平均延迟 / 活跃 session 数 / 累计请求
- 可用率 vs 失败率柱状图（60 秒窗口）
- 平均延迟折线图
- 代理列表（按国家/协议/状态过滤 + 点击表头排序）
- 国家分布（Top 24 国家 + 旗帜 + 数量）
- 活跃 session 列表（剩余 TTL + 一键轮转/删除）

**能操作**：

- 强制刷新代理池（拉新源）
- 触发健康探测
- 轮转指定 session 的 IP
- 直接跳转 Prometheus `/metrics` 和 `/healthz` 查看原始数据

依赖：零。不需要 Node.js / npm / 构建步骤，`go build` 即带。

## 健康探测工作流

```
新代理进池 → 满分假象 (success_rate=1, latency=0)
       ↓
后台探测 (默认 60s/轮，并发 50)
       ↓
   ┌──────┐
   │ TCP  │  L4 dial 到 host:port
   │ dial │  失败 → fail++
   └──┬───┘  成功 → 记录真实延迟
      │
      ↓ (可选 --check-l7)
   ┌──────────┐
   │ CONNECT  │  HTTP CONNECT example.com:443
   │ probe    │  失败 → fail++
   └──┬───────┘  成功 → 记录端到端延迟
      │
      ↓
   连续失败 ≥ BanOnFail (默认 3) → 进入冷却 (--fail-cooldown)
   单次成功 → 解封 + 重置 streak
```

实战数据：3336 代理探完一轮，约 65% L4 可达，35% 死链被标记。

示例：

```bash
proxyhub serve \
  --proxy-port 7000 \
  --api-port 7001 \
  --db /var/lib/proxyhub.db \
  --refresh-interval 5m \
  --fail-cooldown 3m \
  --extra-source "my-cn=https://example.com/cn-proxies.txt:http;my-jp=https://example.com/jp.txt:socks5" \
  --log-level info
```

## 部署

### 二进制

从 [Releases](https://github.com/jiusanzhou/proxyhub/releases) 下载：

```bash
# Linux amd64
curl -L https://github.com/jiusanzhou/proxyhub/releases/latest/download/proxyhub_0.2.0_linux_amd64.tar.gz | tar -xz
./proxyhub serve --db /var/lib/proxyhub.db

# macOS arm64 (M1/M2/M3/M4)
curl -L https://github.com/jiusanzhou/proxyhub/releases/latest/download/proxyhub_0.2.0_darwin_arm64.tar.gz | tar -xz
./proxyhub serve
```

或 `go install`：

```bash
go install go.zoe.im/proxyhub/cmd/proxyhub@latest
```

### Docker

```bash
docker run -d --name proxyhub \
  -p 7000:7000 -p 7001:7001 \
  -v proxyhub-data:/data \
  ghcr.io/jiusanzhou/proxyhub:latest \
  serve --db /data/proxyhub.db
```

多架构（amd64 + arm64）镜像已发布到 GHCR。

### 从源码编译

需要 Go 1.25+ 和 [pnpm](https://pnpm.io/)。

```bash
git clone https://github.com/jiusanzhou/proxyhub
cd proxyhub

make build          # 构建 dashboard (Vite/React) + Go 二进制
./bin/proxyhub serve

# 仅开发 dashboard（Vite hot reload）
make dashboard-dev  # 终端 A：启动 Vite 在 :5173
./bin/proxyhub serve   # 终端 B：起 API 在 :7001
# 访问 http://localhost:5173/，Vite 自动 proxy /api 到 :7001
```

发布产物（GitHub Release）已经预先 build 好 dashboard，**用户拿来直接用，不需要 Node.js 环境**。

### systemd

参考 `deploy/systemd/proxyhub.service`。

## 架构

```
proxyhub/
├── cmd/proxyhub/         # 主入口
├── internal/
│   ├── pool/            # 代理池核心（Proxy + Pool + RoundTripper）
│   ├── source/          # 代理来源（Proxifly + TextSource）
│   ├── store/           # SQLite 持久化
│   ├── server/          # HTTP 前向代理 + REST API
│   └── metrics/         # Prometheus 指标
├── pkg/client/          # Go SDK（公开 API）
└── deploy/              # 部署配置
```

## 注意事项

⚠️ **免费代理质量差**：实测可用率 5-15%，靠多次重试 + 健康度淘汰。
靠谱场景：A 股数据采集（接口稳定且容忍失败重试）；不靠谱场景：登录态、长连接、严格 SSL 校验。

⚠️ **HTTPS CONNECT**：免费代理大多不支持 CONNECT 隧道；HTTPS 走前向代理时通过率显著低于 HTTP。
建议：用 REST API 拿代理 URL，自己控制重试逻辑。

⚠️ **首次启用建议 warmup**：跑 30 分钟以上让健康度数据沉淀，再用作业务采集。

## 相关项目

- [finpipe](https://github.com/jiusanzhou/finpipe) - 开源金融数据平台（首个 proxyhub 消费方）

## License

MIT
