# ProxyHub Dashboard (Cloudflare Pages)

Vite + React + TypeScript dashboard，支持两种构建模式。

## 构建模式

### 1. Go embed (默认)

输出到 `../../internal/dashboard/assets/`，供 Go 二进制嵌入：

```bash
pnpm build
```

### 2. Cloudflare Pages

输出到 `dist/`，供 CF Pages 部署：

```bash
VITE_API_BASE=https://proxyhub.example.com pnpm build:pages
```

`VITE_API_BASE` 指向 Go 主节点或 Workers 的 API 地址（必须 CORS 允许来源）。

## 本地开发

```bash
pnpm dev    # localhost:5173, API 代理到 localhost:7001
```

## Cloudflare Pages 部署

### 方式 A: `wrangler pages deploy`

```bash
# 构建
VITE_API_BASE=https://proxyhub-workers.wuma.workers.dev pnpm build:pages

# 首次创建 project
npx wrangler pages project create proxyhub-dashboard --production-branch main

# 部署
npx wrangler pages deploy dist --project-name proxyhub-dashboard
```

### 方式 B: CF Dashboard 连仓库

1. Cloudflare Dashboard → Workers & Pages → Create → Pages → Connect to Git
2. 选仓库 `jiusanzhou/proxyhub`
3. Build settings:
   - Build command: `cd edge/pages && pnpm install && pnpm build:pages`
   - Build output directory: `edge/pages/dist`
   - Root directory: `/`
4. Environment variables:
   - `VITE_API_BASE`: API 地址（如 `https://proxyhub.example.com`）

## Env vars

| 变量 | 作用 | 示例 |
|------|------|------|
| `VITE_API_BASE` | API base URL（构建时注入） | `https://proxyhub.example.com` |
| `PAGES_BUILD` | `1` 时输出到 `dist/`，否则输出到 Go embed 目录 | `1` |
