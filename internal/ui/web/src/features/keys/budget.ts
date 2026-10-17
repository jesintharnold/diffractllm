import { formatUSD } from '@/lib/format'
import type { Budget } from '@/features/keys/api'

// Budget money is nano-USD end to end; only the display divides.
export const toUSD = (nano: number) => nano / 1e9
export const money = (nano: number) => formatUSD(toUSD(nano)).value

// core.ParseDuration: d, w, m (a month is 30 days) and y, otherwise a Go duration.
const SECONDS: Record<string, number> = {
  s: 1,
  h: 3600,
  d: 86_400,
  w: 604_800,
  m: 2_592_000,
  y: 31_536_000,
}

export function windowSeconds(duration: string): number | null {
  const m = /^(\d+(?:\.\d+)?)([smhdwy])$/i.exec(duration.trim())
  if (!m?.[1] || !m[2]) return null
  const unit = SECONDS[m[2].toLowerCase()]
  return unit === undefined ? null : Number(m[1]) * unit
}

const PERIODS: Record<string, [label: string, unit: string]> = {
  '1d': ['daily', 'day'],
  '1w': ['weekly', 'week'],
  '1m': ['monthly', 'month'],
  '1y': ['yearly', 'year'],
}

// "1D" → daily · anything unusual is shown as written.
export const periodLabel = (duration: string) => PERIODS[duration.toLowerCase()]?.[0] ?? duration
export const periodUnit = (duration: string) => PERIODS[duration.toLowerCase()]?.[1] ?? duration

// "11 h 14 m" · "3 d 4 h" · "<1 m"
function span(ms: number): string {
  const minutes = Math.floor(ms / 60_000)
  if (minutes < 1) return '<1 m'
  const days = Math.floor(minutes / 1440)
  const hours = Math.floor((minutes % 1440) / 60)
  if (days > 0) return `${String(days)} d ${String(hours)} h`
  return hours > 0 ? `${String(hours)} h ${String(minutes % 60)} m` : `${String(minutes)} m`
}

// When the spend counter next resets, e.g. "in 11 h 14 m".
export function resetsIn(b: Budget, now = Date.now()): string {
  const seconds = windowSeconds(b.budget_duration)
  if (seconds === null) return '—'
  const left = new Date(b.last_budget_refresh_at).getTime() + seconds * 1000 - now
  return left <= 0 ? 'now' : `in ${span(left)}`
}

// Share of the limit used this window, 0 to 1 (capped).
export const spendRatio = (b: Budget) =>
  b.budget_limit > 0 ? Math.min((b.total_spend ?? 0) / b.budget_limit, 1) : 0
