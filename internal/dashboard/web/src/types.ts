// proxyhub API types - mirror internal/pool/stats and internal/server handlers

export interface Proxy {
  url: string
  protocol: 'http' | 'https' | 'socks4' | 'socks5'
  country: string
  anonymity: string
  source: string
  score: number
  success_rate: number
  avg_latency_ms: number
  total_requests: number
  success_count: number
  fail_count: number
  is_banned: boolean
}

export interface HealthResp {
  status: 'ok' | string
  uptime: string
  pool_size: number
  available: number
  sessions: number
  proxy_reqs: number
  api_reqs: number
  checker?: {
    total_probes: number
    success: number
    failed: number
  }
}

export interface StatsResp {
  total: number
  available: number
  banned: number
  by_country: Record<string, number>
  by_protocol: Record<string, number>
  avg_score: number
  avg_latency_ms: number
}

export interface SessionInfo {
  id: string
  proxy?: Proxy
  created_at: string
  last_used: string
  ttl_seconds: number
  expires_in: number
  failure_count: number
}
