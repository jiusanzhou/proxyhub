# proxyhub

[![Go Reference](https://pkg.go.dev/badge/go.zoe.im/proxyhub.svg)](https://pkg.go.dev/go.zoe.im/proxyhub)
[![Go Report Card](https://goreportcard.com/badge/go.zoe.im/proxyhub)](https://goreportcard.com/report/go.zoe.im/proxyhub)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/jiusanzhou/proxyhub/actions/workflows/ci.yml/badge.svg)](https://github.com/jiusanzhou/proxyhub/actions)

> Aggregated proxy pool microservice — multi-source free proxy aggregation + health scoring + smart rotation + session stickiness

Compatible with Bright Data / SmartProxy / Oxylabs standard proxy interface semantics. A single binary, zero external dependencies, self-hosted.

**Built-in Web Dashboard** (Vite + React + TypeScript, build artifacts go:embed into binary, no runtime dependencies) — visit `http://localhost:7001/` after startup.

**[中文文档](README.zh.md)**

## Architecture

```
┌─────────────────┐       ┌──────────────────┐       ┌──────────────────┐
│  Source Fetch   │──>──> │   Pool Management │──>──> │   Consumers      │
│  - Proxifly     │       │ - Health Scoring  │       │ - HTTP forward   │
│  - TextSource   │       │ - Country/Proto  │       │ - REST API       │
│  (extensible)   │       │ - TopN random    │       │ - Go SDK         │
└─────────────────┘       └──────────┬───────┘       └──────────────────┘
                                     │
                                     ▼
                          ┌──────────────────┐
                          │ SQLite Persistence│
                          │ (zero-dep)        │
                          └──────────────────┘
```

## Features

- 🚀 **Zero external dependencies**: single Go binary + SQLite
- 🌐 **Multi-source aggregation**: proxifly (3000+ proxies, refreshed every 5 min) + custom text subscriptions
- 🔬 **Active health probing**: background L4/L7 probing, automatically mark unavailable, record real latency
- 📊 **Health scoring**: `score = success_rate * 0.6 + latency_score * 0.4`
- 🔁 **Smart rotation**: randomly choose from TopN high-score proxies to avoid hot-spot overuse
- 🛡️ **Failure fallback**: per-proxy cooldown + auto-retry + pool-empty fallback direct connect
- 🎯 **Three access methods**: HTTP forward proxy / REST API / Go SDK
- 📈 **Observability**: Prometheus metrics + JSON stats
- 🎨 **Web Dashboard**: real-time metrics, charts, proxy list, session management, 5s auto-refresh
- ⚙️ **Flexible config**: CLI flags > environment variables > YAML/TOML/JSON config file > defaults

## Quick Start

```bash
# Install
go install go.zoe.im/proxyhub/cmd/proxyhub@latest

# Start service
proxyhub serve

# Default listeners:
#   :7000 - HTTP forward proxy
#   :7001 - REST API + Prometheus + Dashboard

# Custom
proxyhub serve --proxy-port 8000 --api-port 8001 --db /var/lib/proxyhub.db
```

## Usage

### 1️⃣ HTTP Forward Proxy (simplest)

```bash
# Any language, any tool, use proxyhub as forward proxy
curl -x http://localhost:7000 https://api.example.com

# Pass preferences via headers
curl -x http://localhost:7000 \
  -H "X-Proxyhub-Country: CN" \
  -H "X-Proxyhub-Prefer-Asian: true" \
  https://push2.eastmoney.com/api/qt/clist/get
```

Supported request headers:

| Header | Description |
|---|---|
| `X-Proxyhub-Country` | ISO country code (CN/US/HK/...) |
| `X-Proxyhub-Protocol` | http/https/socks4/socks5 |
| `X-Proxyhub-Prefer-Asian` | true = prefer Asian proxies |
| `X-Proxyhub-HTTPS-Only` | true = use HTTPS proxies only |
| `X-Proxyhub-Top-N` | random from top N high-score (default 20) |
| `X-Proxyhub-Session` | sticky session ID (same ID = same IP) |
| `X-Proxyhub-TTL` | session TTL (e.g. `10m`, default 10m) |
| `X-Proxyhub-Rotate` | true = force rotate IP for this session |

**Response headers** (attached to every response for audit/logging):

| Header | Description |
|---|---|
| `X-Proxyhub-Proxy` | actual proxy URL used |
| `X-Proxyhub-Country` | proxy exit country |
| `X-Proxyhub-Latency-Ms` | proxy-side latency this request |
| `X-Proxyhub-Session` | echoed session ID |
| `X-Proxyhub-Rotated` | true = just rotated IP |
| `X-Proxyhub-Attempts` | actual attempts (including retries) |

#### Session Sticky IP

```bash
# Same session ID automatically binds to same exit IP until TTL expires or fail threshold reached
curl -x http://localhost:7000 \
  -H "X-Proxyhub-Session: my-task-123" \
  -H "X-Proxyhub-TTL: 30m" \
  http://example.com/api/login

# Subsequent requests reuse same IP
curl -x http://localhost:7000 \
  -H "X-Proxyhub-Session: my-task-123" \
  http://example.com/api/data

# Force rotate IP (same session)
curl -x http://localhost:7000 \
  -H "X-Proxyhub-Session: my-task-123" \
  -H "X-Proxyhub-Rotate: true" \
  http://example.com/api/data

# Bright Data / SmartProxy compatible username format
curl -x http://user-session-myid-country-CN:any@localhost:7000 \
  http://example.com
```

Session auto-rotate on failures: within a session, proxy consecutive failures (default 3) auto-bind new IP. 1-2 failures only rotate within pool without breaking session semantics.

### 2️⃣ REST API

```bash
# Get one proxy
curl 'http://localhost:7001/api/v1/pick?country=CN&prefer_asian=1'
# → {"url":"http://1.2.3.4:8080","country":"CN","score":0.85,...}

# Report usage result (optional, for health stats)
curl -X POST http://localhost:7001/api/v1/report \
  -H 'Content-Type: application/json' \
  -d '{"proxy":"http://1.2.3.4:8080","success":true,"latency_ms":234}'

# Pool stats
curl http://localhost:7001/api/v1/stats
# → {"total":3336,"available":3336,"by_country":{"CN":186,"HK":14,...}}

# List (supports country / available / sort / limit filtering)
curl 'http://localhost:7001/api/v1/proxies?available=true&limit=10&sort=score'

# Create session
curl -X POST http://localhost:7001/api/v1/sessions \
  -H 'Content-Type: application/json' \
  -d '{"id":"my-task","country":"CN","ttl":"30m"}'

# Rotate / delete session
curl -X POST http://localhost:7001/api/v1/sessions/my-task/rotate
curl -X DELETE http://localhost:7001/api/v1/sessions/my-task

# List sessions
curl http://localhost:7001/api/v1/sessions

# Refresh pool
curl -X POST http://localhost:7001/api/v1/refresh

# Trigger health check
curl -X POST http://localhost:7001/api/v1/check

# Prometheus metrics
curl http://localhost:7001/metrics
```

### 3️⃣ Go SDK

```go
import "go.zoe.im/proxyhub/pkg/client"

c, err := client.New("http://localhost:7001")
if err != nil {
    log.Fatal(err)
}

// Get proxy
p, err := c.Pick(&client.PickOpts{
    Country: "CN",
    Protocol: "https",
})
if err != nil {
    log.Fatal(err)
}
http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(p.URL)}}

// Create session
s, err := c.CreateSession("my-task", &client.SessionOpts{
    Country: "CN",
    TTL:     30 * time.Minute,
})
if err != nil {
    log.Fatal(err)
}
http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(s.Proxy.URL)}}

// Rotate / delete
s.Rotate()
s.Delete()
```

See [pkg/client](pkg/client) for full API.

## Configuration

Three config sources with priority: **flag > env > config file > defaults**.

### 1. Config File (recommended for production)

```bash
proxyhub serve --config /etc/proxyhub.yaml
```

Supports YAML / TOML / JSON. Example `proxyhub.yaml`:

```yaml
proxy_port: 7000
api_port: 7001
db: /var/lib/proxyhub.db
log_level: info

refresh_interval: 10m
fail_cooldown: 5m

# Extra text subscriptions: name=url:proto;...
extra_source: ""

# Health check
check_enabled: true
check_interval: 60s
check_dial_timeout: 5s
check_http_timeout: 8s
check_concurrency: 50
check_l7: false
check_target: httpbin.org:80
check_ban_on_fail: 3
```

### 2. Environment Variables

Any field can be set via uppercase underscore format:

```bash
PROXY_PORT=8000 API_PORT=8001 DB=/tmp/p.db proxyhub serve
CHECK_CONCURRENCY=200 CHECK_L7=true proxyhub serve
```

### 3. CLI Flags

Full list `proxyhub serve --help`, main fields:

```
--proxy-port int             HTTP forward proxy port (default 7000)
--api-port int               REST API + Prometheus port (default 7001)
--db string                  SQLite DB path (default ./proxyhub.db)
--refresh-interval dur       pool refresh interval (default 10m)
--fail-cooldown dur          failure cooldown (default 5m)
--log-level string           log level debug/info/warn/error (default info)
--extra-source string        extra text subs name=url:proto, ; separated

# Health check
--check                      enable health check (default true)
--check-interval dur         round interval (default 60s)
--check-dial-timeout dur     L4 TCP dial timeout (default 5s)
--check-http-timeout dur     L7 HTTP CONNECT timeout (default 8s)
--check-concurrency int      probe concurrency (default 50)
--check-l7                   enable L7 HTTP CONNECT (default false)
--check-target string        L7 target host:port (default httpbin.org:80)
--check-ban-on-fail int      consecutive fails to ban (default 3)
```

## Health Check Workflow

```
┌──────────────────┐
│ Background timer │ (every check_interval, default 60s)
└────────┬─────────┘
         │
         ▼
   ┌─────────┐   L4 dial (check_dial_timeout)
   │   All   │──>────────────────────────────┐
   │ proxies │                                │
   └─────────┘                                │
         │                                   ▼
         │                               ┌─────────┐   Success?
         │                               │  Each   │──> Yes → record latency, reset fail count
         │                               │  proxy  │
         │                               └─────────┘
         │                                   │ No
         │                                   ▼
         │                              ┌─────────┐   fail_count++ >= check_ban_on_fail?
         │                              │  Count  │──> Yes → mark banned, ban_time=now + fail_cooldown
         │                              └─────────┘
         │
         ▼
   ┌─────────┐
   │ Update  │   score = success_rate * 0.6 + latency_score * 0.4
   │ scores  │   latency_score = clamp(1 - latency_ms / 5000, 0, 1)
   └─────────┘
```

## Deployment

### Binary

Download from [Releases](https://github.com/jiusanzhou/proxyhub/releases):

```bash
# Linux amd64
curl -L https://github.com/jiusanzhou/proxyhub/releases/download/v0.4.0/proxyhub_0.4.0_linux_amd64.tar.gz | tar -xz
./proxyhub serve --db /var/lib/proxyhub.db

# macOS arm64 (M1/M2/M3/M4)
curl -L https://github.com/jiusanzhou/proxyhub/releases/download/v0.4.0/proxyhub_0.4.0_darwin_arm64.tar.gz | tar -xz
./proxyhub serve
```

Or `go install` (auto-includes embedded dashboard):

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

Multi-arch (amd64 + arm64) images published to GHCR.

### Build from Source

```bash
git clone https://github.com/jiusanzhou/proxyhub
cd proxyhub

make build          # build dashboard (Vite/React) + Go binary
./bin/proxyhub serve

# Dev only: Vite hot reload
make dashboard-dev  # Terminal A: Vite at :5173, auto-proxy /api to :7001
./bin/proxyhub serve   # Terminal B: API at :7001
# Visit http://localhost:5173/
```

### systemd

See `deploy/systemd/proxyhub.service`.

## Architecture

```
proxyhub/
├── cmd/proxyhub/          entry
│   ├── main.go
│   └── commands/
│       └── serve.go       serve subcommand
├── internal/
│   ├── config/            Config struct (flag + yaml + env)
│   ├── dashboard/         React dashboard (Vite + embed)
│   ├── pool/              proxy pool + health checker
│   ├── session/           session sticky IP
│   ├── server/            HTTP forward + REST API handlers
│   ├── source/            proxy sources (Proxifly + Text)
│   ├── store/             SQLite persistence
│   └── metrics/           Prometheus metrics
└── pkg/client/            Go SDK
```

## Notes

- **SQLite default**: single-file DB, suitable for small deployments. For high-throughput, consider PostgreSQL (planned).
- **Health check overhead**: 3336 proxies × 200 concurrency × 3s timeout = ~21s per round. Adjust `check_concurrency` based on your network.
- **Proxy quality**: free proxies are unstable. Use `--check` + `--check-concurrency` + `--check-ban-on-fail` to tune.

### Advanced Config (store + multiple sources)

For PostgreSQL backend and multiple sources, use structured config:

```yaml
# config.example.yaml

store:
  type: postgres
  config:
    dsn: postgres://user:pass@localhost:5432/proxyhub?sslmode=disable

sources:
  - name: proxifly
    type: proxifly

  - name: my-custom-list
    type: text
    config:
      url: https://example.com/proxies.txt
      protocol: http
```

Supported store types: `sqlite` (default), `postgres` (aliases: `pg`, `postgresql`).
Supported source types: `proxifly`, `text` (more planned).

## Roadmap

- [x] PostgreSQL store (via `go.zoe.im/x/factory` for abstraction)
- [ ] More proxy sources (ScrapeCenter, HideMyName, etc.)
- [ ] WebSocket real-time push for dashboard
- [ ] Dark/Light theme toggle

## Related Projects

- [finpipe](https://github.com/jiusanzhou/finpipe) — open-source financial data platform, integrates proxyhub as proxy source for data collection
- [x](https://go.zoe.im/x) — Zoe's Go utility library (cli / version / config / factory)

## License

[MIT](LICENSE)
