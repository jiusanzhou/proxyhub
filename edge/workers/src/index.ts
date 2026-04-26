/**
 * proxyhub Cloudflare Workers
 *
 * 只读代理 API，从 D1 数据库查询代理数据。
 * 写入（checkin/report）仍由 Go 主节点负责。
 */
import { Hono } from 'hono';
import { cors } from 'hono/cors';
import { getStats, listProxies, pickProxies } from './db';

export interface Env {
  DB: D1Database;
}

const app = new Hono<{ Bindings: Env }>();

app.use('*', cors());

/**
 * GET /api/v1/pick
 *
 * 从 D1 查可用代理，按 score 取 TopN 后随机返回。
 *
 * Query params:
 *   n       - 返回数量，默认 1，最大 50
 *   proto   - 协议过滤（http/socks5/...）
 *   country - 国家过滤（US/CN/...）
 */
app.get('/api/v1/pick', async (c) => {
  const n = Math.min(50, Math.max(1, parseInt(c.req.query('n') ?? '1', 10)));
  const proxies = await pickProxies(c.env.DB, n);

  if (proxies.length === 0) {
    return c.json({ error: 'no proxies available' }, 503);
  }

  const result = proxies.map((p) => ({
    url: p.proxy_url,
    protocol: p.protocol,
    country: p.country,
    anonymity: p.anonymity,
    latency_ms: p.last_latency_ms,
  }));

  return c.json({ proxies: result, count: result.length });
});

/**
 * GET /api/v1/proxies
 *
 * 代理列表（分页）。
 *
 * Query params:
 *   limit  - 每页数量，默认 100，最大 500
 *   offset - 偏移量，默认 0
 */
app.get('/api/v1/proxies', async (c) => {
  const limit = Math.min(500, Math.max(1, parseInt(c.req.query('limit') ?? '100', 10)));
  const offset = Math.max(0, parseInt(c.req.query('offset') ?? '0', 10));

  const { proxies, total } = await listProxies(c.env.DB, limit, offset);

  return c.json({
    proxies: proxies.map((p) => ({
      url: p.proxy_url,
      protocol: p.protocol,
      country: p.country,
      anonymity: p.anonymity,
      total_requests: p.total_requests,
      success_count: p.success_count,
      fail_count: p.fail_count,
      latency_ms: p.last_latency_ms,
      is_banned: p.is_banned === 1,
      source: p.source,
    })),
    total,
    limit,
    offset,
  });
});

/**
 * GET /api/v1/stats
 *
 * 统计摘要：总数、活跃数、封禁数、按协议/国家分组。
 */
app.get('/api/v1/stats', async (c) => {
  const stats = await getStats(c.env.DB);
  return c.json(stats);
});

// 404 fallback
app.notFound((c) => c.json({ error: 'not found' }, 404));

export default app;
