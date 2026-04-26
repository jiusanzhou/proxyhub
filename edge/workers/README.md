# proxyhub Workers

proxyhub 的 Cloudflare Workers 只读代理 API，从 D1 数据库提供代理查询接口。

写入操作（代理检查、存储）仍由 Go 主节点负责，Workers 只提供只读查询。

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/pick` | 按 score 随机返回可用代理 |
| GET | `/api/v1/proxies` | 代理列表（分页） |
| GET | `/api/v1/stats` | 统计摘要 |

### GET /api/v1/pick

```
GET /api/v1/pick?n=5
```

参数：
- `n` - 返回数量（默认 1，最大 50）

响应：
```json
{
  "proxies": [
    {
      "url": "http://1.2.3.4:8080",
      "protocol": "http",
      "country": "US",
      "anonymity": "anonymous",
      "latency_ms": 120
    }
  ],
  "count": 1
}
```

### GET /api/v1/stats

```json
{
  "total": 1000,
  "active": 850,
  "banned": 150,
  "by_protocol": { "http": 600, "socks5": 400 },
  "by_country": { "US": 200, "DE": 150 }
}
```

## 部署步骤

### 前提

- [Node.js 22+](https://nodejs.org/)
- [pnpm](https://pnpm.io/)
- Cloudflare 账号，已创建 D1 数据库

### 1. 安装依赖

```bash
cd edge/workers
pnpm install
```

### 2. 配置 wrangler.toml

编辑 `wrangler.toml`，将 `database_id` 替换为实际的 D1 数据库 ID：

```toml
[[d1_databases]]
binding = "DB"
database_name = "proxyhub"
database_id = "your-actual-database-id"
```

### 3. 本地开发

```bash
pnpm dev
```

Wrangler 会在本地启动 Workers 开发服务器，使用本地 D1 模拟。

### 4. 部署

```bash
pnpm deploy
```

或使用 Wrangler 命令：

```bash
npx wrangler deploy
```

### 5. 绑定主节点 D1

确保 Go 主节点配置了相同的 D1 数据库：

```yaml
store:
  type: d1
  config:
    account_id: "your-account-id"
    database_id: "your-database-id"
    api_token: "your-api-token"
```

## 架构说明

```
Go 主节点 ──写入──> D1 数据库 <──只读── Workers (边缘)
                                              │
                                         用户 /pick 请求
```

- **Go 主节点**：负责代理发现、健康检查、数据写入 D1
- **Workers**：无状态，只从 D1 读取，部署在 Cloudflare 边缘网络
- **低延迟**：Workers 在全球边缘节点运行，代理 pick 延迟极低
