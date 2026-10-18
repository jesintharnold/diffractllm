import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { API } from '@/app/endpoints'
import type { RequestLogRow } from '@/features/metrics/api'
import { ApiError, apiGet } from '@/lib/api'
import { windowParams, type RangeWindow } from '@/lib/time-range'

export const requestPath = (id: string) => `/observability/requests/${encodeURIComponent(id)}`

// What narrows the log. Empty means "any"; `q` is a piece of an id, client or model name.
export type StatusFilter = '' | 'success' | 'error' | 'cancelled'

export interface RequestFilter {
  q: string
  status: StatusFilter
  provider: string
  model: string
  client: string
}

export const NO_FILTER: RequestFilter = { q: '', status: '', provider: '', model: '', client: '' }

export const isFiltered = (f: RequestFilter) => Object.values(f).some(Boolean)

// GET /v1/admin/metrics/requests: `total` counts every match, not just this page.
export interface RequestPage {
  total: number
  rows: RequestLogRow[]
}

export const REQUEST_PAGE_SIZE = 25

// A live table asks for a new window every few seconds; the old ones are not worth keeping.
const LIVE_GC_MS = 30_000

export function useRequestLog(window: RangeWindow, filter: RequestFilter, page: number) {
  const params = {
    ...windowParams(window),
    q: filter.q,
    status: filter.status,
    provider: filter.provider,
    model: filter.model,
    client: filter.client,
    limit: REQUEST_PAGE_SIZE,
    offset: (page - 1) * REQUEST_PAGE_SIZE,
  }
  return useQuery({
    queryKey: ['metrics', 'request-log', params],
    queryFn: ({ signal }) => apiGet<RequestPage>(API.admin.metrics.requests, params, signal),
    placeholderData: keepPreviousData,
    gcTime: LIVE_GC_MS,
  })
}

// GET /v1/admin/metrics/requests/filters: the values that exist in the window, for the dropdowns.
export interface FilterOptions {
  providers: string[]
  models: string[]
  clients: string[]
}

export function useRequestFilterOptions(window: RangeWindow) {
  const params = windowParams(window)
  return useQuery({
    queryKey: ['metrics', 'request-filters', params.from, params.to],
    queryFn: ({ signal }) =>
      apiGet<FilterOptions>(API.admin.metrics.requestFilters, params, signal),
    placeholderData: keepPreviousData,
  })
}

// GET /v1/admin/metrics/requests/{id}: one request, by event id or by the client's request id.
// Money arrives in USD, times in ms; absent values are omitted by the server.
export interface CostLine {
  item: string
  quantity: number
  unit: string
  unit_cost_usd: number
  cost_usd: number
}

export interface Stage {
  name: string
  start_us: number
  end_us: number
  duration_us: number
}

export interface RequestDetail {
  id: string
  request_id?: string
  started_at: string
  request_kind?: string
  endpoint: string
  result: string
  http_status: number
  latency_ms: number
  ttfb_ms?: number
  ttft_ms?: number
  error?: { code?: string; category?: string; message?: string }
  model: {
    requested?: string
    provider?: string
    model?: string
    upstream?: string
    stream: boolean
    finish_reason?: string
  }
  tokens: {
    input?: number
    output?: number
    cached_input?: number
    cache_creation?: number
    reasoning?: number
    billed?: number
  }
  context: {
    virtual_key_id?: string
    client_id?: string
    budget_id?: string
    credential?: string
    routing_mode?: string
  }
  upstream: {
    http_status?: number
    attempts?: number
    request_id?: string
    headers?: Record<string, string>
  }
  pipeline?: Stage[]
  cost: { total_usd: number; lines?: CostLine[] }
  payload_state: 'none' | 'ready' | 'truncated'
}

// A request that is not stored (yet, or any more) is a 404 the page explains; retrying will not help.
export function useRequestDetail(id: string) {
  return useQuery({
    queryKey: ['metrics', 'request', id],
    queryFn: ({ signal }) =>
      apiGet<RequestDetail>(API.admin.metrics.request(id), undefined, signal),
    enabled: id !== '',
  })
}

// GET /v1/admin/metrics/requests/{id}/payload: the bodies, as the client and the provider saw them.
export interface RequestPayload {
  client_request?: unknown
  normalized_request?: unknown
  provider_request?: unknown
  provider_response?: unknown
  normalized_response?: unknown
  client_response?: unknown
}

export const isNotFound = (error: unknown) => error instanceof ApiError && error.status === 404

// Fetched only when the admin asks to see it: bodies can be large and hold customer content.
export function useRequestPayload(eventId: string, enabled: boolean) {
  return useQuery({
    queryKey: ['metrics', 'request-payload', eventId],
    queryFn: ({ signal }) =>
      apiGet<RequestPayload>(API.admin.metrics.payload(eventId), undefined, signal),
    enabled: enabled && eventId !== '',
  })
}
