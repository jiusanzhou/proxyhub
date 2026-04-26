export function drawStacked(
  ctx: CanvasRenderingContext2D,
  availableSeries: number[],
  bannedSeries: number[],
  width: number,
  height: number,
): void {
  ctx.clearRect(0, 0, width, height)
  const n = availableSeries.length
  if (n === 0) return

  const totals = availableSeries.map((a, i) => a + bannedSeries[i])
  const max = Math.max(...totals, 1)
  const barW = width / n

  for (let i = 0; i < n; i++) {
    const availH = (availableSeries[i] / max) * height * 0.9
    const bannedH = (bannedSeries[i] / max) * height * 0.9
    const x = i * barW + 1
    const bw = barW - 2
    ctx.fillStyle = '#34d399'
    ctx.fillRect(x, height - availH, bw, availH)
    ctx.fillStyle = '#f87171'
    ctx.fillRect(x, height - availH - bannedH, bw, bannedH)
  }

  // Legend
  ctx.font = '11px -apple-system, system-ui'
  ctx.fillStyle = '#34d399'
  ctx.fillRect(width - 120, 8, 8, 8)
  ctx.fillStyle = '#7b8394'
  ctx.fillText('available', width - 108, 16)
  ctx.fillStyle = '#f87171'
  ctx.fillRect(width - 60, 8, 8, 8)
  ctx.fillStyle = '#7b8394'
  ctx.fillText('banned', width - 48, 16)
}

export function drawLine(
  ctx: CanvasRenderingContext2D,
  series: number[],
  width: number,
  height: number,
  color: string,
  labelUnit: string,
): void {
  ctx.clearRect(0, 0, width, height)
  if (series.length < 2) return

  const max = Math.max(...series, 100)
  const min = Math.min(...series, 0)
  const rng = max - min || 1
  const step = width / (series.length - 1)

  // Grid
  ctx.strokeStyle = '#242b3a'
  ctx.lineWidth = 1
  for (let y = 0; y < 4; y++) {
    const yy = (height / 4) * y + 0.5
    ctx.beginPath()
    ctx.moveTo(0, yy)
    ctx.lineTo(width, yy)
    ctx.stroke()
  }

  // Area
  ctx.beginPath()
  for (let i = 0; i < series.length; i++) {
    const x = i * step
    const y = height - ((series[i] - min) / rng) * height * 0.9 - 5
    if (i === 0) ctx.moveTo(x, y)
    else ctx.lineTo(x, y)
  }
  ctx.lineTo((series.length - 1) * step, height)
  ctx.lineTo(0, height)
  ctx.closePath()
  ctx.fillStyle = color + '22'
  ctx.fill()

  // Line
  ctx.beginPath()
  ctx.strokeStyle = color
  ctx.lineWidth = 2
  for (let i = 0; i < series.length; i++) {
    const x = i * step
    const y = height - ((series[i] - min) / rng) * height * 0.9 - 5
    if (i === 0) ctx.moveTo(x, y)
    else ctx.lineTo(x, y)
  }
  ctx.stroke()

  // Labels
  ctx.font = '11px -apple-system, system-ui'
  ctx.fillStyle = '#7b8394'
  ctx.textAlign = 'right'
  ctx.fillText(`${Math.round(max)}${labelUnit}`, width - 4, 12)
  ctx.fillText(`${Math.round(min)}${labelUnit}`, width - 4, height - 4)
}
