# proxyhub

> 聚合代理池微服务 - 多源免费代理聚合 + 健康度评估 + 智能轮转

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

支持的 header：
- `X-Proxyhub-Country`: ISO 国家代码（CN/US/HK/...）
- `X-Proxyhub-Protocol`: http/https/socks4/socks5
- `X-Proxyhub-Prefer-Asian`: true 时优先亚洲代理
- `X-Proxyhub-HTTPS-Only`: true 时只用 HTTPS 代理
- `X-Proxyhub-Top-N`: 在前 N 个高分中随机（默认 20）

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

# Prometheus 指标
curl http://localhost:7001/metrics

# Healthz
curl http://localhost:7001/healthz
```

### 3️⃣ Go SDK

```go
import "github.com/jiusanzhou/proxyhub/pkg/client"

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

CLI flags：

```
--proxy-port int          HTTP 前向代理端口 (默认 7000)
--api-port int            REST API + Prometheus 端口 (默认 7001)
--db string               SQLite 数据库路径 (默认 ./proxyhub.db)
--refresh-interval dur    代理池刷新间隔 (默认 10m)
--fail-cooldown dur       失败代理冷却时间 (默认 5m)
--log-level string        日志级别 debug/info/warn/error (默认 info)
--extra-source string     额外文本订阅源 name=url:proto，多个用 ; 分隔
```

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

```bash
go install github.com/jiusanzhou/proxyhub/cmd/proxyhub@latest
proxyhub serve --db /var/lib/proxyhub.db
```

### Docker

```bash
docker run -d --name proxyhub \
  -p 7000:7000 -p 7001:7001 \
  -v proxyhub-data:/data \
  zoe/proxyhub:latest \
  serve --db /data/proxyhub.db
```

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
