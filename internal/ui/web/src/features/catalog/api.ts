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
  last_sync_at?: string
  last_error?: string
}

export const CATALOG_KEY = ['catalog'] as const

export function useCatalogSummary() {
  return useQuery({
    queryKey: [...CATALOG_KEY, 'summary'],
    queryFn: ({ signal }) => apiGet<CatalogSummary>(API.admin.catalog.summary, undefined, signal),
    // Poll fast while a sync runs, so "Last sync" flips as soon as it lands.
    refetchInterval: (q) => (q.state.data?.syncing ? 2_000 : 30_000),
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
  pricing?: TokenPricing
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

export function useSyncCatalog() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => apiSend<{ status: string }>('POST', API.admin.catalog.sync),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: CATALOG_KEY })
    },
  })
}
