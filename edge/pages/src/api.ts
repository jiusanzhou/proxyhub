import type { HealthResp, Proxy, SessionInfo, StatsResp } from './types'

// API base URL, injected at build time via vite define
// - 默认空字符串（embed 模式，与 Go 主节点同源）
// - Pages 部署可通过 VITE_API_BASE 指向远端 Workers/主节点
declare const __API_BASE__: string
const API_BASE = typeof __API_BASE__ !== 'undefined' ? __API_BASE__ : ''

async function http<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(API_BASE + path, init)
  if (!r.ok) {
    const body = await r.text().catch(() => '')
    throw new Error(`${r.status} ${r.statusText}: ${body}`)
  }
  const ct = r.headers.get('content-type') || ''
  return ct.includes('json') ? r.json() : (r.text() as unknown as T)
}

export const api = {
  health: () => http<HealthResp>('/healthz'),
  stats: () => http<StatsResp>('/api/v1/stats'),

  proxies: (params: {
    country?: string
    protocol?: string
    available?: boolean
    limit?: number
  }) => {
    const q = new URLSearchParams()
    if (params.country) q.set('country', params.country)
    if (params.protocol) q.set('protocol', params.protocol)
    if (params.available) q.set('available', 'true')
    q.set('limit', String(params.limit ?? 100))
    return http<{ proxies: Proxy[]; count: number }>(`/api/v1/proxies?${q}`)
  },

  refreshPool: () =>
    http<{ added: number; total: number }>('/api/v1/refresh', { method: 'POST' }),

  triggerCheck: () =>
    http<{ enabled: boolean; stats?: unknown }>('/api/v1/check', { method: 'POST' }),

  sessions: () => http<{ sessions: SessionInfo[]; count: number }>('/api/v1/sessions'),
  rotateSession: (id: string) =>
    http<{ rotated: string }>(`/api/v1/sessions/rotate?id=${encodeURIComponent(id)}`, {
      method: 'POST',
    }),
  deleteSession: (id: string) =>
    http<{ deleted: string }>(`/api/v1/sessions?id=${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
}
