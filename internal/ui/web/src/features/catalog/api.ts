import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { API } from '@/app/endpoints'
import type { OverviewTiles } from '@/features/metrics/api'
import { apiGet, apiSend } from '@/lib/api'

// Catalog page data. Usage numbers (requests, tokens, spend) always cover the last 24 hours up
// to now; keys sit under 'metrics' so the Overview's Sync refreshes them too.
const DAY_MS = 86_400_000

function last24h() {
  const to = new Date()
  return { from: new Date(to.getTime() - DAY_MS).toISOString(), to: to.toISOString() }
}

export interface CatalogSummary {
  providers: string[] // configured: adapter in this build + at least one credential
  provider_models: Record<string, number>
  models: number
  priced: number
  unpriced: number
  syncing: boolean
  last_run_at?: string // start of the latest run, successful or not
  last_sync_at?: string
  last_error?: string
}

export const CATALOG_KEY = ['catalog'] as const

export function useCatalogSummary() {
  return useQuery({
    queryKey: [...CATALOG_KEY, 'summary'],
    queryFn: ({ signal }) => apiGet<CatalogSummary>(API.admin.catalog.summary, undefined, signal),
    refetchInterval: 30_000, // picks up scheduled syncs; Sync now watches its own run

  })
}

export interface ProviderUsage {
  provider: string
  requests: number
  tokens: number
  spend_usd: number
}

export function useProviderUsage24h() {
  return useQuery({
    queryKey: ['metrics', 'providers-24h'],
    queryFn: ({ signal }) =>
      apiGet<{ rows: ProviderUsage[] }>(API.admin.metrics.providers, last24h(), signal),
    refetchInterval: 60_000,
  })
}

export function useSpend24h() {
  return useQuery({
    queryKey: ['metrics', 'stats-24h'],
    queryFn: ({ signal }) => apiGet<OverviewTiles>(API.admin.metrics.stats, last24h(), signal),
    refetchInterval: 60_000,
  })
}

// Per-token prices as the catalog stores them; the table shows them per 1M tokens.
export interface TokenPricing {
  input_cost_per_token?: number
  output_cost_per_token?: number
  cache_read_input_token_cost?: number
  cache_creation_input_token_cost?: number
}

export interface CatalogModel {
  id?: string
  provider: string
  model_name: string
  model_type: string
  limits: { context_window?: number; max_output_tokens?: number }
  pricing?: TokenPricing & Record<string, number | undefined> // every core.Pricing field it sets
}

export interface ModelFilters {
  q: string
  provider: string
  type: string
  pricing: '' | 'priced' | 'unpriced'
}

export const MODELS_PAGE = 10

export function useCatalogModels(filters: ModelFilters, page: number) {
  const params = { ...filters, limit: MODELS_PAGE, offset: (page - 1) * MODELS_PAGE }
  return useQuery({
    queryKey: [...CATALOG_KEY, 'models', params],
    queryFn: ({ signal }) =>
      apiGet<{ models: CatalogModel[]; total: number }>(API.admin.catalog.models, params, signal),
    placeholderData: keepPreviousData,
  })
}

export interface CatalogSettings {
  auto_sync: boolean
  interval_seconds: number
  timeout_seconds: number
  missing_price: 'reject' | 'charge_zero'
  ledger_unit: string
  display_currency: string
}

const SETTINGS_KEY = [...CATALOG_KEY, 'settings'] as const

export function useCatalogSettings() {
  return useQuery({
    queryKey: SETTINGS_KEY,
    queryFn: ({ signal }) => apiGet<CatalogSettings>(API.admin.catalog.settings, undefined, signal),
  })
}

// Save sends the whole form as one JSON object; the server applies it live.
export function useSaveCatalogSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (next: CatalogSettings) =>
      apiSend<CatalogSettings>('PUT', API.admin.catalog.settings, next),
    onSuccess: (saved) => {
      queryClient.setQueryData(SETTINGS_KEY, saved)
      void queryClient.invalidateQueries({ queryKey: [...CATALOG_KEY, 'summary'] })
    },
  })
}

const SUMMARY_KEY = [...CATALOG_KEY, 'summary'] as const
const WATCH_EVERY_MS = 5_000
const GIVE_UP_MS = 5 * 60_000 // the longest fetch timeout the settings allow

export type SyncOutcome = { ok: true; models: number } | { ok: false; error: string }

// Sync now: queue the sync, then watch the summary until that run has finished, and report
// how it went. The run is "ours" once last_run_at moves past its value at the click.
export function useSyncCatalog(onDone: (outcome: SyncOutcome) => void) {
  const queryClient = useQueryClient()

  const start = useMutation({
    mutationFn: async (): Promise<SyncOutcome> => {
      const before = queryClient.getQueryData<CatalogSummary>(SUMMARY_KEY)?.last_run_at
      await apiSend<{ status: string }>('POST', API.admin.catalog.sync)
      const deadline = Date.now() + GIVE_UP_MS
      while (Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, WATCH_EVERY_MS))
        const s = await queryClient.query({
          queryKey: SUMMARY_KEY,
          queryFn: ({ signal }) => apiGet<CatalogSummary>(API.admin.catalog.summary, undefined, signal),
          staleTime: 0,
        })
        if (!s.syncing && s.last_run_at && s.last_run_at !== before) {
          return s.last_error ? { ok: false, error: s.last_error } : { ok: true, models: s.models }
        }
      }
      return { ok: false, error: 'no result after 5 minutes' }
    },
    onSuccess: (outcome) => {
      void queryClient.invalidateQueries({ queryKey: CATALOG_KEY })
      onDone(outcome)
    },
    onError: (err) => {
      onDone({ ok: false, error: err.message })
    },
  })

  return {
    sync: () => {
      start.mutate()
    },
    syncing: start.isPending,
  }
}
