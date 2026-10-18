import type { RequestDetail } from '@/features/requests/api'

// Durations on this page are set tight, with no space before the unit: 0.4ms, 167.5ms, 2.24s.
export function duration(ms: number): string {
  return ms < 1000 ? `${ms.toFixed(1)}ms` : `${(ms / 1000).toFixed(2)}s`
}

// Axis labels: 0, 600ms, 1.2s.
export function tick(ms: number): string {
  if (ms <= 0) return '0'
  return ms < 1000 ? `${String(Math.round(ms))}ms` : `${(ms / 1000).toFixed(1)}s`
}

// Money on a detail page is shown to the digit the lines add up to: $0.01219.
export const dollars = (usd: number) => `$${usd.toFixed(5)}`

export const nanoUSD = (usd: number) => Math.round(usd * 1e9).toLocaleString('en-US')

export const words = (name: string) => name.replaceAll('_', ' ')

// A long id keeps its start: dk-8fa2c41e…
export const shortId = (id: string) => (id.length > 12 ? `${id.slice(0, 12)}…` : id)

const RESULT_LABEL: Record<string, string> = {
  ok: 'ok',
  rejected: 'rejected',
  upstream_error: 'upstream error',
  gateway_error: 'gateway error',
  client_abort: 'cancelled',
}

// "200 · ok", "400 · rejected". A cancelled request answered 200 before the client left.
export function statusPill(d: RequestDetail): { text: string; tone: 'good' | 'bad' | 'muted' } {
  const label = RESULT_LABEL[d.result] ?? words(d.result)
  const code = d.http_status === 0 ? '' : `${String(d.http_status)} · `
  if (d.result === 'client_abort') return { text: `${code}${label}`, tone: 'muted' }
  return { text: `${code}${label}`, tone: d.result === 'ok' ? 'good' : 'bad' }
}

export const PILL_TONE = {
  good: 'border-success/30 bg-success/10 text-success',
  warn: 'border-warning/30 bg-warning/10 text-warning',
  bad: 'border-destructive/30 bg-destructive/10 text-destructive',
  muted: 'border-border bg-foreground/5 text-muted-foreground',
} as const

// The request never left the gateway: no provider was picked, so there is no upstream side.
export const reachedProvider = (d: RequestDetail) =>
  Boolean(d.model.provider) &&
  ((d.upstream.attempts ?? 0) > 0 || (d.upstream.http_status ?? 0) > 0 || (d.ttfb_ms ?? 0) > 0)
