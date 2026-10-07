import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { API } from '@/app/endpoints'
import { CATALOG_KEY, type CatalogModel } from '@/features/catalog/api'
import { apiGet, apiSend } from '@/lib/api'

// Prices are per token, keyed by core.Pricing's json names; absent = not set.
export type PricingFields = Record<string, number | undefined>

export type Scope = 'global' | 'provider' | 'virtualkey'

export interface Override {
  id: string
  name: string
  model_name: string
  model_type: string
  pricing: PricingFields
  scope_type: Scope
  scope_virtual_key_id?: string
  scope_provider?: string
  created_at: string
  updated_at: string
}

export interface OverrideRequest {
  name: string
  model_name: string
  model_type: string
  pricing: PricingFields
  scope_type: Scope
  scope_virtual_key_id?: string
  scope_provider?: string
}

const OVERRIDES_KEY = ['pricing', 'overrides'] as const

export function useOverrides() {
  return useQuery({
    queryKey: OVERRIDES_KEY,
    queryFn: ({ signal }) => apiGet<Override[]>(API.admin.pricing.overrides, undefined, signal),
  })
}

export interface VirtualKey {
  id: string
  display_prefix: string
  client_id: string
}

export function useVirtualKeys() {
  return useQuery({
    queryKey: ['governance', 'virtual-keys'],
    queryFn: ({ signal }) =>
      apiGet<{ virtual_keys: VirtualKey[] }>(API.admin.virtualKeys, undefined, signal),
    select: (d) => d.virtual_keys,
  })
}

export interface ModelOption {
  model_name: string
  model_type: string
}

// Priced models matching q (substring), limited to one provider when given. The catalog lists a
// model once per provider; each name and type is offered once.
export function useModelSearch(q: string, provider: string, enabled: boolean) {
  return useQuery({
    enabled,
    queryKey: [...CATALOG_KEY, 'search', q, provider],
    queryFn: ({ signal }) =>
      apiGet<{ models: CatalogModel[] }>(
        API.admin.catalog.models,
        { q, provider, pricing: 'priced', limit: 200 },
        signal,
      ),
    select: (d): ModelOption[] => {
      const seen = new Map<string, ModelOption>()
      for (const m of d.models) seen.set(`${m.model_name}\u0000${m.model_type}`, m)
      return [...seen.values()]
    },
    placeholderData: keepPreviousData,
  })
}

// A model's base price on each configured provider, from the catalog listing (which already
// carries `pricing`); q matches by substring, so keep the exact name only.
export function useBasePrices(modelName: string, modelType: string) {
  return useQuery({
    queryKey: [...CATALOG_KEY, 'base', modelName, modelType],
    queryFn: ({ signal }) =>
      apiGet<{ models: CatalogModel[] }>(
        API.admin.catalog.models,
        { q: modelName, type: modelType, limit: 100 },
        signal,
      ),
    select: (d) => d.models.filter((m) => m.model_name === modelName && m.pricing),
    enabled: modelName !== '' && modelType !== '',
  })
}

function useOverrideMutation<V>(fn: (v: V) => Promise<unknown>) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: OVERRIDES_KEY })
    },
  })
}

export const useCreateOverride = () =>
  useOverrideMutation((req: OverrideRequest) => apiSend<Override>('POST', API.admin.pricing.overrides, req))

// The server's update takes the prices only; name, model and scope are fixed once created.
export const useUpdateOverride = () =>
  useOverrideMutation(({ id, pricing }: { id: string; pricing: PricingFields }) =>
    apiSend<Override>('PUT', API.admin.pricing.override(id), pricing),
  )

export const useDeleteOverride = () =>
  useOverrideMutation((id: string) => apiSend<undefined>('DELETE', API.admin.pricing.override(id)))
