import { useEffect, useRef, useState } from 'react'

/**
 * usePolling 反复执行异步任务，间隔 intervalMs 毫秒。
 * 返回最新值、loading、error 和手动 refetch。
 */
export function usePolling<T>(fn: () => Promise<T>, intervalMs: number) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [loading, setLoading] = useState(true)
  const fnRef = useRef(fn)
  fnRef.current = fn

  const tick = async () => {
    try {
      const v = await fnRef.current()
      setData(v)
      setError(null)
    } catch (e) {
      setError(e as Error)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    tick()
    const id = setInterval(tick, intervalMs)
    return () => clearInterval(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intervalMs])

  return { data, error, loading, refetch: tick }
}

/**
 * useHistory 维护一个 push-only 滑动窗口数组。
 */
export function useHistory<T>(value: T | null | undefined, max: number): T[] {
  const [history, setHistory] = useState<T[]>([])
  const lastRef = useRef<T | null | undefined>(null)
  useEffect(() => {
    if (value === null || value === undefined) return
    if (lastRef.current === value) return
    lastRef.current = value
    setHistory((h) => {
      const next = [...h, value]
      return next.length > max ? next.slice(next.length - max) : next
    })
  }, [value, max])
  return history
}
