/**
 * D1 查询封装
 *
 * schema 与 internal/store/sqlite.go 保持一致：
 *
 *   CREATE TABLE proxies (
 *     proxy_url TEXT PRIMARY KEY,
 *     protocol TEXT NOT NULL,
 *     country TEXT NOT NULL DEFAULT 'XX',
 *     anonymity TEXT NOT NULL DEFAULT 'unknown',
 *     total_requests INTEGER NOT NULL DEFAULT 0,
 *     success_count INTEGER NOT NULL DEFAULT 0,
 *     fail_count INTEGER NOT NULL DEFAULT 0,
 *     last_latency_ms INTEGER NOT NULL DEFAULT 0,
 *     avg_latency_ms_x1000 INTEGER NOT NULL DEFAULT 0,
 *     is_banned INTEGER NOT NULL DEFAULT 0,
 *     ban_until_ns INTEGER NOT NULL DEFAULT 0,
 *     last_used_at_ns INTEGER NOT NULL DEFAULT 0,
 *     last_check_at_ns INTEGER NOT NULL DEFAULT 0,
 *     source TEXT NOT NULL DEFAULT ''
 *   );
 */

export interface Proxy {
  proxy_url: string;
  protocol: string;
  country: string;
  anonymity: string;
  total_requests: number;
  success_count: number;
  fail_count: number;
  last_latency_ms: number;
  avg_latency_ms_x1000: number;
  is_banned: number;
  ban_until_ns: number;
  last_used_at_ns: number;
  last_check_at_ns: number;
  source: string;
}

export interface StatsRow {
  total: number;
  active: number;
  banned: number;
  by_protocol: string;
  by_country: string;
}

/** 按 score 降序取 TopN 可用代理（未封禁） */
export async function pickProxies(db: D1Database, n = 10): Promise<Proxy[]> {
  const nowNs = BigInt(Date.now()) * 1_000_000n;
  // score = success_count * 1.0 / (total_requests + 1) - avg_latency_ms_x1000 / 10000000.0
  const { results } = await db
    .prepare(
      `SELECT * FROM proxies
       WHERE is_banned = 0
         AND (ban_until_ns = 0 OR ban_until_ns < ?)
       ORDER BY
         CAST(success_count AS REAL) / (total_requests + 1) * 0.6
         - CAST(avg_latency_ms_x1000 AS REAL) / 10000000.0 * 0.4 DESC
       LIMIT ?`
    )
    .bind(nowNs.toString(), n * 3) // 取多一些再随机
    .all<Proxy>();

  // 随机打乱后取前 n 条
  const shuffled = (results ?? []).sort(() => Math.random() - 0.5);
  return shuffled.slice(0, n);
}

/** 全部代理列表（分页） */
export async function listProxies(
  db: D1Database,
  limit = 100,
  offset = 0
): Promise<{ proxies: Proxy[]; total: number }> {
  const [listRes, countRes] = await Promise.all([
    db
      .prepare(`SELECT * FROM proxies ORDER BY last_check_at_ns DESC LIMIT ? OFFSET ?`)
      .bind(limit, offset)
      .all<Proxy>(),
    db.prepare(`SELECT COUNT(*) as total FROM proxies`).first<{ total: number }>(),
  ]);
  return {
    proxies: listRes.results ?? [],
    total: countRes?.total ?? 0,
  };
}

/** 统计摘要 */
export async function getStats(db: D1Database): Promise<{
  total: number;
  active: number;
  banned: number;
  by_protocol: Record<string, number>;
  by_country: Record<string, number>;
}> {
  const nowNs = BigInt(Date.now()) * 1_000_000n;

  const [totals, byProto, byCountry] = await Promise.all([
    db
      .prepare(
        `SELECT
           COUNT(*) as total,
           SUM(CASE WHEN is_banned = 0 AND (ban_until_ns = 0 OR ban_until_ns < ?) THEN 1 ELSE 0 END) as active,
           SUM(CASE WHEN is_banned = 1 OR (ban_until_ns > 0 AND ban_until_ns >= ?) THEN 1 ELSE 0 END) as banned
         FROM proxies`
      )
      .bind(nowNs.toString(), nowNs.toString())
      .first<{ total: number; active: number; banned: number }>(),
    db
      .prepare(`SELECT protocol, COUNT(*) as cnt FROM proxies GROUP BY protocol`)
      .all<{ protocol: string; cnt: number }>(),
    db
      .prepare(`SELECT country, COUNT(*) as cnt FROM proxies GROUP BY country ORDER BY cnt DESC LIMIT 20`)
      .all<{ country: string; cnt: number }>(),
  ]);

  const by_protocol: Record<string, number> = {};
  for (const row of byProto.results ?? []) {
    by_protocol[row.protocol] = row.cnt;
  }
  const by_country: Record<string, number> = {};
  for (const row of byCountry.results ?? []) {
    by_country[row.country] = row.cnt;
  }

  return {
    total: totals?.total ?? 0,
    active: totals?.active ?? 0,
    banned: totals?.banned ?? 0,
    by_protocol,
    by_country,
  };
}
