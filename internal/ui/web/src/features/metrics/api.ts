import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { API } from '@/app/endpoints'
import { apiGet } from '@/lib/api'
import { MAX_RANGE_MS, MIN_RANGE_MS, windowParams, type RangeWindow } from '@/lib/time-range'

// GET /v1/admin/metrics/stats. Money arrives in USD, latency in ms (log-engine.md §3).
export interface OverviewTiles {
  requests: number
  errors: number
  cancelled: number
  error_rate: number
  p95_latency_ms: number
  p99_latency_ms: number
  total_tokens: number
  spend_usd: number
}

// Every metrics query is keyed on the page's shared window, so tiles and charts agree.
// keepPreviousData holds the old numbers on screen while a new range loads.
export function useOverviewStats(window: RangeWindow) {
  const params = windowParams(window)
  return useQuery({
    queryKey: ['metrics', 'stats', params.from, params.to],
    queryFn: ({ signal }) => apiGet<OverviewTiles>(API.admin.metrics.stats, params, signal),
    placeholderData: keepPreviousData,
  })
}

// Has the gateway served anything in the whole retained window (90 days)? False only on a
// fresh install, where empty charts are noise. Cached for 5 minutes; independent of the picker.
export const HAS_TRAFFIC_KEY = ['metrics', 'has-traffic'] as const

export function useHasTraffic() {
  return useQuery({
    queryKey: HAS_TRAFFIC_KEY,
    queryFn: async ({ signal }) => {
      const to = new Date()
      const params = windowParams({ from: new Date(to.getTime() - MAX_RANGE_MS), to })
      const tiles = await apiGet<OverviewTiles>(API.admin.metrics.stats, params, signal)
      return tiles.requests > 0
    },
    staleTime: 5 * 60_000,
  })
}

// GET /v1/admin/metrics/requests: one row per request, newest first. `provider` is empty for
// requests rejected before routing; `result` tells a cancelled request (client_abort) from a 200.
export interface RequestLogRow {
  id: string
  request_id?: string
  started_at: string
  request_kind: string
  provider: string
  model: string
  latency_ms: number
  input_tokens: number
  output_tokens: number
  cost_usd: number
  http_status: number
  result: string
  client_id: string
}

// Recent requests are a live view, not part of the page window: always the last RECENT_MINUTES,
// refreshed every RECENT_POLL_MS while the tab is visible (Bifrost's logs page does the same).
export const RECENT_KEY = ['metrics', 'recent'] as const
const RECENT_MINUTES = 10
const RECENT_POLL_MS = 10_000

export function useRecentRequests(limit = 10) {
  return useQuery({
    queryKey: [...RECENT_KEY, limit],
    queryFn: async ({ signal }) => {
      // The backend takes windows of an hour or more: ask for the last hour, keep the last 10 minutes.
      const to = new Date()
      const params = windowParams({ from: new Date(to.getTime() - MIN_RANGE_MS), to })
      const page = await apiGet<{ total: number; rows: RequestLogRow[] }>(
        API.admin.metrics.requests,
        { ...params, limit },
        signal,
      )
      const cutoff = to.getTime() - RECENT_MINUTES * 60_000
      return page.rows.filter((r) => new Date(r.started_at).getTime() >= cutoff)
    },
    refetchInterval: RECENT_POLL_MS,
  })
}

// GET /v1/admin/metrics/requests/summary. The server picks the bucket from the range
// (Bifrost's table) and aligns bars to clock boundaries; empty buckets come back as zeros.
export interface RequestSummary {
  from: string
  to: string
  bucket_seconds: number
  points: {
    t: string
    errors: number
    cancelled: number
    providers: { provider: string; success: number }[]
  }[]
}

export function useRequestSummary(window: RangeWindow) {
  const params = windowParams(window)
  return useQuery({
    queryKey: ['metrics', 'summary', params.from, params.to],
    queryFn: ({ signal }) =>
      apiGet<RequestSummary>(API.admin.metrics.requestSummary, params, signal),
    placeholderData: keepPreviousData,
  })
}
