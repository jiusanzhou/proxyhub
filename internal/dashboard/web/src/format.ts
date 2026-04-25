export function fmtNum(n: number | undefined | null): string {
  if (n === null || n === undefined) return '—'
  if (n < 1000) return String(n)
  if (n < 10000) return (n / 1000).toFixed(1) + 'k'
  return Math.round(n / 1000) + 'k'
}

export function fmtMs(n: number | undefined | null): string {
  if (n === undefined || n === null) return '—'
  return Math.round(n) + 'ms'
}

export function fmtPct(n: number): string {
  return (n * 100).toFixed(1) + '%'
}

export function fmtDuration(sec: number): string {
  if (sec < 0) sec = 0
  if (sec < 60) return sec + 's'
  if (sec < 3600) return `${Math.floor(sec / 60)}m ${sec % 60}s`
  return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`
}

export function flag(iso: string): string {
  if (!iso || iso.length !== 2) return '🌐'
  const A = 0x1f1e6
  return (
    String.fromCodePoint(A + iso.charCodeAt(0) - 65) +
    String.fromCodePoint(A + iso.charCodeAt(1) - 65)
  )
}
