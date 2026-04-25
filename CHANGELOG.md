# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.1] - 2026-04-25

### Changed
- **Dashboard 重构为 Vite + React 19 + TypeScript**
  - 组件化、类型安全、HMR 开发体验
  - 沿用之前的视觉设计（卡片 / 柱状+折线图 / 表格 / Session 管理 / Ops）
  - Dev: `make dashboard-dev` Vite proxy 到 :7001
  - Build 产物 go:embed 进二进制（生产无需 Node.js）
  - CI/Release workflow 增加 pnpm build 步骤
  - 资源体积: app.js 64KB gzipped, 入口 html 0.5KB

### Internal
- internal/dashboard/web/ 是 Vite/React 工程
- internal/dashboard/assets/ 是 build 产物（go:embed 目标，纳入 git 方便 go install 用户）
- Makefile: `make build` 自动跑 dashboard build

## [0.3.0] - 2026-04-25

### Added
- **Web Dashboard**（嵌入式 UI）
  - 实时 metrics 卡片（total/available/banned/latency/sessions/reqs）
  - Canvas 绘制的可用率柱状图 + 延迟折线图（60s 窗口）
  - 代理列表（过滤 + 排序）、国家分布、活跃 session 管理
  - 5s 自动刷新，支持强制刷新池 / 触发健康探测 / 轮转 session
- `/api/v1/proxies` 新增 `protocol` 过滤参数

## [0.2.0] - 2026-04-25

### Added
- **Session 粘性会话** — 同一 session ID 绑定同一出口 IP，支持 TTL/轮转/自动失败切换
- **响应元数据 header** — `X-Proxyhub-Proxy` / `Country` / `Latency-Ms` / `Session` / `Rotated` / `Attempts`
- **Bright Data 兼容用户名编码** — `user-session-xxx-country-CN:any@host:7000`
- **主动健康探测** — L4 TCP dial（可选 L7 CONNECT），每轮自动淘汰失效代理
- **Session REST API** — `GET/POST/DELETE /api/v1/sessions`、`POST /api/v1/sessions/rotate`
- `/healthz` 新增 `sessions` 字段
- Go SDK `pkg/client` 新增 `ParseMeta` / `CreateSession` / `RotateSession` / `DeleteSession`

## [0.1.0] - 2026-04-25

### Added
- 初始版本
- 多源代理聚合（Proxifly + 可扩展 TextSource）
- SQLite 持久化（modernc.org/sqlite，无 CGO）
- 三种消费方式：HTTP 前向代理 / REST API / Go SDK
- 健康度评分（成功率 × 0.6 + 延迟分 × 0.4）
- Prometheus metrics（手写纯文本）
- Top-N 随机挑选避免热门代理被打爆
- 支持 http/https/socks4/socks5 协议
