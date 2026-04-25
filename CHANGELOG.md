# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
