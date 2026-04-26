# proxyhub Edge

proxyhub 的 Cloudflare Edge 部分，包含 Workers（只读 API）和 Pages（Dashboard）。

## 目录结构

```
edge/
├── pages/          # Dashboard 前端 (React + Vite)
│                   # 同时用于 Go embed 和 Cloudflare Pages 部署
└── workers/        # Cloudflare Workers 只读代理 API (TypeScript + Hono)
```

## 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                       proxyhub 系统                          │
│                                                             │
│  ┌──────────────┐   写入/检查    ┌──────────────────────┐   │
│  │  Go 主节点   │ ─────────────> │  共享数据库           │   │
│  │              │                │  D1（Cloudflare）     │   │
│  │ • 代理发现   │                │  或 PostgreSQL        │   │
│  │ • 健康检查   │ <──────────── │  或 SQLite（本地）    │   │
│  │ • /api/v1/*  │   LoadAll      └──────────────────────┘   │
│  │ • Dashboard  │                          │                 │
│  └──────────────┘                          │ 只读             │
│         │                                  ▼                 │
│         │ embed                 ┌──────────────────────┐    │
│         ▼                       │  Workers (边缘节点)   │    │
│  ┌──────────────┐               │                      │    │
│  │   Dashboard  │               │ GET /api/v1/pick     │    │
│  │   (内嵌)     │               │ GET /api/v1/stats    │    │
│  └──────────────┘               │ GET /api/v1/proxies  │    │
│                                  └──────────────────────┘    │
│                                                              │
│  ┌──────────────────────────────────────────┐               │
│  │  Pages (Cloudflare Pages / Go embed)     │               │
│  │  Dashboard UI — React + Vite             │               │
│  └──────────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────┘
```

## 组件说明

### Go 主节点

负责核心业务逻辑：
- 代理源抓取与发现
- 代理健康检查（定期 check）
- 代理数据持久化（支持 D1 / PostgreSQL / SQLite）
- 完整 REST API：`/api/v1/pick`、`/api/v1/stats`、`/api/v1/proxies`
- 内嵌 Dashboard（`go:embed`）

### D1 / 共享数据库

支持三种 store：
| Store | 场景 |
|-------|------|
| SQLite | 本地开发、单机部署 |
| PostgreSQL | 多节点生产部署 |
| D1 | Cloudflare 生态，配合 Workers 使用 |

当使用 D1 时，Go 主节点通过 [D1 REST API](https://developers.cloudflare.com/d1/platform/client-api/) 写入，Workers 直接绑定 D1 读取。

### Workers（只读边缘 API）

部署在 Cloudflare 边缘网络，直接绑定 D1 数据库，提供低延迟的代理查询接口。不处理写入，仅做只读查询。

详见 [workers/README.md](./workers/README.md)。

### Pages（Dashboard）

`edge/pages/` 是 React + Vite 前端项目，有两种部署方式：

1. **Go embed**：`pnpm build` 输出到 `../../internal/dashboard/assets/`，随 Go 二进制一起发布
2. **Cloudflare Pages**：直接部署到 Cloudflare Pages，指向同一 Workers API

## 部署方案

### 方案一：Go 主节点 + SQLite（单机，最简单）

```bash
# 构建并运行
make build
./bin/proxyhub serve
```

### 方案二：Go 主节点 + PostgreSQL（多节点）

```yaml
# config.yaml
store:
  type: postgres
  config:
    dsn: postgres://user:pass@host:5432/proxyhub
```

```bash
make build
./bin/proxyhub serve --config config.yaml
```

### 方案三：Go 主节点 + D1 + Workers（Cloudflare 全栈）

1. 在 Cloudflare 控制台创建 D1 数据库

2. 配置 Go 主节点使用 D1：
   ```yaml
   store:
     type: d1
     config:
       account_id: "your-account-id"
       database_id: "your-database-id"
       api_token: "your-api-token"
   ```

3. 部署 Workers（边缘只读 API）：
   ```bash
   cd edge/workers
   pnpm install
   # 编辑 wrangler.toml，填入 database_id
   pnpm deploy
   ```

4. （可选）部署 Dashboard 到 Cloudflare Pages：
   ```bash
   cd edge/pages
   pnpm install
   pnpm build
   # 通过 Cloudflare Pages 部署 ../../internal/dashboard/assets/
   ```

## 本地开发

```bash
# 启动 Go 主节点（带热重载 API）
make run

# 启动 Dashboard 开发服务器（HMR，代理 /api 到 :7001）
make dashboard-dev

# 启动 Workers 本地开发
cd edge/workers && pnpm dev
```
