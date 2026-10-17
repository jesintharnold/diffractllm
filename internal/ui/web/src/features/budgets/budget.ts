import type { Budget } from '@/features/budgets/api'
import { formatUSD } from '@/lib/format'

// Budget money is nano-USD end to end; only the display divides.
export const toUSD = (nano: number) => nano / 1e9
export const money = (nano: number) => formatUSD(toUSD(nano)).value

// Dollars typed by a person → nano-USD, or null unless it is a positive amount of at most four decimals.
export function parseUSD(text: string): number | null {
  const t = text.trim().replace(/,/g, '')
  if (!/^\d+(\.\d{1,4})?$/.test(t)) return null
  const nano = Math.round(Number(t) * 1e9)
  return nano > 0 && nano <= 1e15 ? nano : null
}

// "500.00" for the edit field; keeps extra decimals only when the amount has them.
export function dollarsText(nano: number): string {
  const usd = toUSD(nano)
  return Number.isInteger(Math.round(usd * 1e4) / 100) ? usd.toFixed(2) : String(usd)
}

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

// "24 h" up to two days, then "7 days".
export function windowLength(seconds: number): string {
  const hours = seconds / 3600
  if (hours <= 48) return `${String(Math.round(hours))} h`
  const days = Math.round(hours / 24)
  return `${String(days)} ${days === 1 ? 'day' : 'days'}`
}

// "Daily", "Weekly", "Monthly", "Yearly"; any other window reads "Every 6 hours" / "Every 3 days".
export function windowName(duration: string): string {
  const named = PERIODS[duration.toLowerCase()]?.[0]
  if (named) return named.charAt(0).toUpperCase() + named.slice(1)
  const seconds = windowSeconds(duration)
  if (seconds === null) return duration
  return `Every ${windowLength(seconds).replace(' h', ' hours').replace('1 days', '1 day')}`
}

// "daily" · "weekly" · "6-hour" · "3-day", for a sentence like "82% of its daily limit".
export function windowAdjective(duration: string): string {
  const named = PERIODS[duration.toLowerCase()]?.[0]
  if (named) return named
  const seconds = windowSeconds(duration)
  if (seconds === null) return duration
  const hours = Math.round(seconds / 3600)
  return hours <= 48 ? `${String(hours)}-hour` : `${String(Math.round(hours / 24))}-day`
}

// ---------- the window picker's draft ----------

export type WindowKind = 'daily' | 'weekly' | 'monthly' | 'custom'
export type CustomUnit = 'h' | 'd'

export interface WindowDraft {
  kind: WindowKind
  count: string // custom only
  unit: CustomUnit
}

export const WINDOW_OPTIONS = [
  ['daily', 'Daily'],
  ['weekly', 'Weekly'],
  ['monthly', 'Monthly'],
  ['custom', 'Custom'],
] as const

const PRESET_VALUE = { daily: '1D', weekly: '1W', monthly: '1M' } as const
const MAX_WINDOW_HOURS = 365 * 24

// What the picker shows for a stored duration. Anything that is not a preset opens as Custom.
export function draftOf(duration: string): WindowDraft {
  const d = duration.toLowerCase()
  if (d === '1d') return { kind: 'daily', count: '', unit: 'h' }
  if (d === '1w') return { kind: 'weekly', count: '', unit: 'h' }
  if (d === '1m') return { kind: 'monthly', count: '', unit: 'h' }
  const seconds = windowSeconds(duration)
  if (seconds === null) return { kind: 'custom', count: '', unit: 'h' }
  return seconds % 86_400 === 0
    ? { kind: 'custom', count: String(seconds / 86_400), unit: 'd' }
    : { kind: 'custom', count: String(Math.round(seconds / 3600)), unit: 'h' }
}

// The budget_duration to send, or null while the custom count is not a valid window.
// Custom windows are sent in hours or days, never "m", which the server reads as months.
export function durationOf(draft: WindowDraft): string | null {
  if (draft.kind !== 'custom') return PRESET_VALUE[draft.kind]
  if (!/^\d+$/.test(draft.count)) return null
  const count = Number(draft.count)
  const hours = draft.unit === 'd' ? count * 24 : count
  return hours >= 1 && hours <= MAX_WINDOW_HOURS ? `${String(count)}${draft.unit}` : null
}

export function windowHint(draft: WindowDraft, started?: string): string {
  if (started) return `Changing the window applies to the current one, which started ${started}.`
  if (draft.kind === 'custom') {
    const duration = durationOf(draft)
    const seconds = duration ? windowSeconds(duration) : null
    return seconds
      ? `Resets every ${windowLength(seconds)}, starting when the budget is created.`
      : 'Between 1 hour and 365 days.'
  }
  const seconds = windowSeconds(PRESET_VALUE[draft.kind]) ?? 0
  return `Starts when the budget is created, then resets every ${windowLength(seconds)}.`
}

// ---------- how a budget is doing ----------

export const NEAR_LIMIT = 0.8

export const isEnforced = (b: Budget) => b.enforce !== false

// Spend over limit, not capped: a budget that is not enforced can pass 100%.
export const usedRatio = (b: Budget) =>
  b.budget_limit > 0 ? (b.total_spend ?? 0) / b.budget_limit : 0

export type LimitState = 'ok' | 'near' | 'reached'

export const limitState = (ratio: number): LimitState =>
  ratio >= 1 ? 'reached' : ratio >= NEAR_LIMIT ? 'near' : 'ok'

export const STATE_BG: Record<LimitState, string> = {
  ok: 'bg-success',
  near: 'bg-warning',
  reached: 'bg-destructive',
}

export const STATE_TEXT: Record<LimitState, string> = {
  ok: 'text-success',
  near: 'text-warning',
  reached: 'text-destructive',
}

// The chip next to a budget's name.
export function statusLabel(b: Budget): string {
  const state = limitState(usedRatio(b))
  if (state === 'ok') return 'On track'
  if (state === 'near') return 'Near limit'
  return isEnforced(b) ? 'Limit reached' : 'Over limit'
}

export const percent = (ratio: number) => `${String(Math.round(ratio * 100))}%`

// ---------- the window clock ----------

function windowBounds(b: Budget): { start: number; length: number } | null {
  const seconds = windowSeconds(b.budget_duration)
  const start = new Date(b.last_budget_refresh_at).getTime()
  return seconds !== null && !Number.isNaN(start) ? { start, length: seconds * 1000 } : null
}

// When the spend counter next resets, in epoch ms.
export function resetAt(b: Budget): number | null {
  const w = windowBounds(b)
  return w ? w.start + w.length : null
}

// How far through the window it is, 0 to 1.
export function elapsedRatio(b: Budget, now = Date.now()): number | null {
  const w = windowBounds(b)
  return w ? Math.min(Math.max((now - w.start) / w.length, 0), 1) : null
}

// "11 h 24 m" · "3 d 4 h" · "<1 m"
export function span(ms: number): string {
  const minutes = Math.floor(ms / 60_000)
  if (minutes < 1) return '<1 m'
  const days = Math.floor(minutes / 1440)
  const hours = Math.floor((minutes % 1440) / 60)
  if (days > 0) return `${String(days)} d ${String(hours)} h`
  return hours > 0 ? `${String(hours)} h ${String(minutes % 60)} m` : `${String(minutes)} m`
}

// "in 11 h 14 m". The window ends when the gateway's 10 s roller next looks, so a little past
// zero reads "now".
export function resetsIn(b: Budget, now = Date.now()): string {
  const end = resetAt(b)
  if (end === null) return '—'
  return end - now <= 0 ? 'now' : `in ${span(end - now)}`
}

// The coarse form for a table cell: "in 4 d" · "in 11 h" · "in 42 m".
export function resetsSoon(b: Budget, now = Date.now()): string {
  const end = resetAt(b)
  if (end === null) return '—'
  const minutes = Math.floor((end - now) / 60_000)
  if (end - now <= 0) return 'now'
  if (minutes >= 2880) return `in ${String(Math.floor(minutes / 1440))} d`
  if (minutes >= 60) return `in ${String(Math.floor(minutes / 60))} h`
  return `in ${String(Math.max(minutes, 1))} m`
}
