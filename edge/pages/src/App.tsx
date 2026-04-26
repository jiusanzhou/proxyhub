import { useState, useEffect, useRef } from 'react'
import { api } from './api'
import { fmtNum, fmtMs, fmtPct, fmtDuration, flag } from './format'
import { usePolling, useHistory } from './hooks'
import { drawStacked, drawLine } from './charts'
import type { Proxy, SessionInfo } from './types'
import './styles.css'

const REFRESH_MS = 5000
const HISTORY_LEN = 12

function App() {
  const [logs, setLogs] = useState<string[]>([])
  const { data: health } = usePolling(api.health, REFRESH_MS)
  const { data: stats } = usePolling(api.stats, REFRESH_MS)

  const availHist = useHistory(health?.available, HISTORY_LEN)
  const bannedHist = useHistory(health ? health.pool_size - health.available : null, HISTORY_LEN)
  const latencyHist = useHistory(stats?.avg_latency_ms, HISTORY_LEN)

  // Chart canvas refs
  const canvasAvail = useRef<HTMLCanvasElement>(null)
  const canvasLat = useRef<HTMLCanvasElement>(null)
  const filterCountryRef = useRef<HTMLInputElement>(null)
  const filterProtoRef = useRef<HTMLSelectElement>(null)
  const filterStatusRef = useRef<HTMLSelectElement>(null)
  const filterLimitRef = useRef<HTMLInputElement>(null)
  const [proxies, setProxies] = useState<Proxy[]>([])
  const [sortKey, setSortKey] = useState<keyof Proxy>('score')
  const [sortDir, setSortDir] = useState(-1)
  const [sessions, setSessions] = useState<SessionInfo[]>([])

  const addLog = (msg: string) => {
    const ts = new Date().toLocaleTimeString()
    setLogs((prev) => [`[${ts}] ${msg}`, ...prev].slice(0, 20))
  }

  // Load proxies on mount and filter change
  useEffect(() => {
    const load = async () => {
      try {
        const c = filterCountryRef.current?.value.trim().toUpperCase()
        const p = filterProtoRef.current?.value || ''
        const s = filterStatusRef.current?.value || ''
        const l = Number(filterLimitRef.current?.value || 100)
        const r = await api.proxies({ country: c, protocol: p, available: s === 'available', limit: l })
        let list = r.proxies
        if (s === 'banned') list = list.filter((x) => x.is_banned)
        list.sort((a, b) => {
          const va = a[sortKey] as string | number | boolean
          const vb = b[sortKey] as string | number | boolean
          if (typeof va === 'string' && typeof vb === 'string') {
            return va.localeCompare(vb) * sortDir
          }
          return ((Number(va) || 0) - (Number(vb) || 0)) * sortDir
        })
        setProxies(list)
      } catch (e) {
        addLog(`proxies load failed: ${(e as Error).message}`)
      }
    }
    load()
  }, [sortKey, sortDir])

  // Sessions polling
  useEffect(() => {
    const load = async () => {
      try {
        const r = await api.sessions()
        setSessions(r.sessions)
      } catch (e) {
        addLog(`sessions load failed: ${(e as Error).message}`)
      }
    }
    load()
    const id = setInterval(load, REFRESH_MS)
    return () => clearInterval(id)
  }, [])

  // Render charts when hist updates
  useEffect(() => {
    if (canvasAvail.current && availHist.length >= 2) {
      const ctx = canvasAvail.current.getContext('2d')
      if (ctx) {
        const dpr = window.devicePixelRatio || 1
        const w = canvasAvail.current.clientWidth * dpr
        const h = canvasAvail.current.clientHeight * dpr
        canvasAvail.current.width = w
        canvasAvail.current.height = h
        ctx.scale(dpr, dpr)
        drawStacked(ctx, availHist, bannedHist, canvasAvail.current.clientWidth, canvasAvail.current.clientHeight)
      }
    }
  }, [availHist, bannedHist])

  useEffect(() => {
    if (canvasLat.current && latencyHist.length >= 2) {
      const ctx = canvasLat.current.getContext('2d')
      if (ctx) {
        const dpr = window.devicePixelRatio || 1
        const w = canvasLat.current.clientWidth * dpr
        const h = canvasLat.current.clientHeight * dpr
        canvasLat.current.width = w
        canvasLat.current.height = h
        ctx.scale(dpr, dpr)
        drawLine(ctx, latencyHist, canvasLat.current.clientWidth, canvasLat.current.clientHeight, '#5b8cff', 'ms')
      }
    }
  }, [latencyHist])

  const onRefreshPool = async () => {
    addLog('refreshing pool...')
    try {
      const r = await api.refreshPool()
      addLog(`refresh ok: added=${r.added}, total=${r.total}`)
    } catch (e) {
      addLog(`refresh failed: ${(e as Error).message}`)
    }
  }
  const onTriggerCheck = async () => {
    addLog('triggering health check...')
    try {
      await api.triggerCheck()
      addLog('check triggered')
    } catch (e) {
      addLog(`check failed: ${(e as Error).message}`)
    }
  }
  const onRotateSession = async (id: string) => {
    try {
      await api.rotateSession(id)
      addLog(`session ${id} rotated`)
      const r = await api.sessions()
      setSessions(r.sessions)
    } catch (e) {
      addLog(`rotate failed: ${(e as Error).message}`)
    }
  }
  const onDeleteSession = async (id: string) => {
    try {
      await api.deleteSession(id)
      addLog(`session ${id} deleted`)
      const r = await api.sessions()
      setSessions(r.sessions)
    } catch (e) {
      addLog(`delete failed: ${(e as Error).message}`)
    }
  }
  const handleSort = (k: keyof Proxy) => {
    if (sortKey === k) setSortDir(-sortDir)
    else { setSortKey(k); setSortDir(-1) }
  }
  const reloadProxies = () => {
    filterCountryRef.current?.dispatchEvent(new Event('input'))
  }

  const isOk = health?.status === 'ok'
  const total = health?.pool_size || 0
  const avail = health?.available || 0
  const banned = total - avail

  return (
    <div className="app">
      <header className="header">
        <div className="brand">
          <span className="logo">🔀</span>
          <span className="title">proxyhub</span>
          <span className="ver">v0.5.0</span>
        </div>
        <nav className="nav">
          <a href="https://github.com/jiusanzhou/proxyhub" target="_blank" rel="noreferrer">
            GitHub
          </a>
          <span className={`status-dot ${isOk ? 'ok' : ''}`}></span>
          <span>{health?.uptime || '—'}</span>
        </nav>
      </header>

      <main className="main">
        <section className="metrics">
          <div className="metric">
            <div className="metric-label">Total</div>
            <div className="metric-value">{fmtNum(total)}</div>
          </div>
          <div className="metric">
            <div className="metric-label">Available</div>
            <div className="metric-value ok">{fmtNum(avail)}</div>
            <div className="metric-sub">{total > 0 ? fmtPct(avail / total) : ''}</div>
          </div>
          <div className="metric">
            <div className="metric-label">Banned</div>
            <div className="metric-value warn">{fmtNum(banned)}</div>
          </div>
          <div className="metric">
            <div className="metric-label">Avg Latency</div>
            <div className="metric-value">{fmtMs(stats?.avg_latency_ms)}</div>
          </div>
          <div className="metric">
            <div className="metric-label">Sessions</div>
            <div className="metric-value">{fmtNum(health?.sessions)}</div>
          </div>
          <div className="metric">
            <div className="metric-label">Proxy Reqs</div>
            <div className="metric-value">{fmtNum(health?.proxy_reqs)}</div>
          </div>
        </section>

        <section className="charts">
          <div className="chart-card">
            <div className="chart-title">Available vs Banned (60s)</div>
            <canvas ref={canvasAvail} />
          </div>
          <div className="chart-card">
            <div className="chart-title">Avg Latency (ms, 60s)</div>
            <canvas ref={canvasLat} />
          </div>
        </section>

        <section className="card block">
          <div className="block-head">
            <h3>Country Distribution</h3>
          </div>
          <div className="country-grid">
            {stats?.by_country ? (
              Object.entries(stats.by_country)
                .sort((a, b) => b[1] - a[1])
                .slice(0, 24)
                .map(([c, n]) => (
                  <div className="country-pill" key={c}>
                    <span className="flag">
                      {flag(c)} {c || 'ZZ'}
                    </span>
                    <span className="count">{n}</span>
                  </div>
                ))
            ) : (
              <div className="empty" style={{ gridColumn: '1/-1', padding: 16, color: 'var(--muted)' }}>
                no data
              </div>
            )}
          </div>
        </section>

        <section className="card block">
          <div className="block-head">
            <h3>Proxies</h3>
            <div className="toolbar">
              <input
                ref={filterCountryRef}
                type="text"
                placeholder="country (CN/US/...)"
                maxLength={4}
                onInput={reloadProxies}
              />
              <select ref={filterProtoRef} onChange={reloadProxies}>
                <option value="">any proto</option>
                <option value="http">http</option>
                <option value="https">https</option>
                <option value="socks4">socks4</option>
                <option value="socks5">socks5</option>
              </select>
              <select ref={filterStatusRef} onChange={reloadProxies}>
                <option value="">all</option>
                <option value="available">available</option>
                <option value="banned">banned</option>
              </select>
              <input ref={filterLimitRef} type="number" defaultValue={100} min={10} max={1000} step={10} onChange={reloadProxies} />
              <button onClick={reloadProxies}>↻</button>
            </div>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th onClick={() => handleSort('country')}>Country</th>
                  <th onClick={() => handleSort('url')}>URL</th>
                  <th onClick={() => handleSort('protocol')}>Proto</th>
                  <th onClick={() => handleSort('score')} className="num">Score</th>
                  <th onClick={() => handleSort('success_rate')} className="num">Success</th>
                  <th onClick={() => handleSort('avg_latency_ms')} className="num">Latency</th>
                  <th onClick={() => handleSort('total_requests')} className="num">Reqs</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {proxies.map((p) => {
                  const status = p.is_banned ? (
                    <span className="badge err">banned</span>
                  ) : (
                    <span className="badge ok">ok</span>
                  )
                  const scorePct = Math.min(100, Math.max(0, p.score * 100))
                  return (
                    <tr key={p.url}>
                      <td>
                        {flag(p.country)} {p.country || 'ZZ'}
                      </td>
                      <td className="url" title={p.url}>
                        {p.url}
                      </td>
                      <td>
                        <span className={`badge proto-${p.protocol}`}>{p.protocol}</span>
                      </td>
                      <td className="num">
                        <span className="score-bar">
                          <span style={{ width: `${scorePct}%` }}></span>
                        </span>
                        {p.score.toFixed(2)}
                      </td>
                      <td className="num">{fmtPct(p.success_rate)}</td>
                      <td className="num">{fmtMs(p.avg_latency_ms)}</td>
                      <td className="num">{fmtNum(p.total_requests)}</td>
                      <td>{status}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </section>

        <section className="card block">
          <div className="block-head">
            <h3>Active Sessions</h3>
            <div className="toolbar">
              <span className="muted">{sessions.length} session{sessions.length === 1 ? '' : 's'}</span>
            </div>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Proxy</th>
                  <th>Country</th>
                  <th className="num">Fails</th>
                  <th className="num">Expires In</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {sessions.length === 0 ? (
                  <tr>
                    <td className="empty" colSpan={6}>
                      no active sessions
                    </td>
                  </tr>
                ) : (
                  sessions.map((s) => (
                    <tr key={s.id}>
                      <td>{s.id}</td>
                      <td className="url" title={s.proxy?.url || '—'}>
                        {s.proxy?.url || '—'}
                      </td>
                      <td>
                        {flag(s.proxy?.country || '')} {s.proxy?.country || '—'}
                      </td>
                      <td className="num">{s.failure_count}</td>
                      <td className="num">{fmtDuration(s.expires_in)}</td>
                      <td>
                        <button className="sm" onClick={() => onRotateSession(s.id)}>
                          rotate
                        </button>
                        <button className="sm danger" onClick={() => onDeleteSession(s.id)}>
                          ×
                        </button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </section>

        <section className="card block">
          <div className="block-head">
            <h3>Ops</h3>
          </div>
          <div className="ops-grid">
            <button className="btn-primary" onClick={onRefreshPool}>
              Refresh Pool
            </button>
            <button onClick={onTriggerCheck}>Force Health Check</button>
            <a className="btn-link" href="/metrics" target="_blank" rel="noreferrer">
              Prometheus metrics ↗
            </a>
            <a className="btn-link" href="/healthz" target="_blank" rel="noreferrer">
              Raw /healthz ↗
            </a>
          </div>
          <pre className={logs.length === 0 ? 'log empty' : 'log'}>{logs.join('\n')}</pre>
        </section>
      </main>

      <footer className="footer">
        <span>proxyhub</span> · <span>auto-refresh every 5s</span> ·{' '}
        <span>updated {new Date().toLocaleTimeString()}</span>
      </footer>
    </div>
  )
}

export default App
