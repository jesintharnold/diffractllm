import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { API } from '@/app/endpoints'
import { CATALOG_KEY, type CatalogModel } from '@/features/catalog/api'
import { apiGet, apiSend } from '@/lib/api'

// Providers the console can set up today; others appear once their adapter ships.
export const SUPPORTED = ['openai', 'azure'] as const
export type SupportedProvider = (typeof SUPPORTED)[number]
export const isSupported = (p: string): p is SupportedProvider =>
  (SUPPORTED as readonly string[]).includes(p)

export const DESCRIPTIONS: Record<SupportedProvider, string> = {
  openai: 'GPT, o-series, embeddings, images',
  azure: 'Azure OpenAI and AI Foundry deployments',
}

// The adapter appends /v1/… itself, so endpoints are hosts.
export const DEFAULT_ENDPOINT: Record<SupportedProvider, string> = {
  openai: 'https://api.openai.com',
  azure: '',
}

export interface ProviderRow {
  id: string
  name: string
  adapter_enabled: boolean
  model_count: number
}

export type AzureAuthMode = 'azure_api_key' | 'azure_service_principal' | 'azure_default_credential'

export interface Alias {
  deploymentID: string
  model_name?: string
  endpoint_protocol?: 'openai' | 'anthropic'
  route_style?: 'v1' | 'deployment'
  api_version?: string
}

export interface AzureSettings {
  auth_mode: AzureAuthMode
  tenant_id?: string
  client_id?: string
  client_secret?: string
  scopes?: string[]
}

// core.Credential as the admin API returns it: secrets come back masked.
export interface Credential {
  id: string
  provider: string
  name: string
  api_key?: string
  enabled: boolean
  expires_at?: string
  allowed_models: string[]
  blocked_models?: string[] | null
  endpoint: string
  aliases?: Record<string, Alias> | null
  settings?: { azure?: AzureSettings }
}

export type CredentialCreate = Omit<Credential, 'id'>

// dbstore.UpdateCredentialRequest: omitted fields are left as they are.
export interface CredentialUpdate {
  name?: string
  api_key?: string
  enabled?: boolean
  expires_at?: string
  clear_expiry?: boolean
  allowed_models?: string[]
  blocked_models?: string[]
  endpoint?: string
  aliases?: Record<string, Alias>
  settings?: { azure?: AzureSettings }
}

export interface CredentialUsage {
  credential_id: string
  last_used_at: string
  requests: number
}

// Durations are Go time.Duration: nanoseconds.
export interface NetworkConfig {
  headers?: Record<string, string>
  request_timeout?: number
  max_retries?: number
  retry_backoff?: number
  max_conns_per_host?: number
  insecure_skip_verify?: boolean
  allow_private_network?: boolean
  retry_ambiguous_status?: boolean
  max_response_bytes?: number
  stream_idle_timeout?: number
}

export type ProxyType = 'http' | 'socks5' | 'environment'

export interface ProxyConfig {
  type: ProxyType
  url?: string
  username?: string
  password?: string
}

// `network_config` is effective (defaults filled in); `overrides` is what this provider stores.
export interface ProviderSettings {
  network_config: NetworkConfig
  proxy_config?: ProxyConfig | null
  overrides: NetworkConfig
}

const KEY = ['providers'] as const

export function useProviders() {
  return useQuery({
    queryKey: [...KEY, 'list'],
    queryFn: ({ signal }) => apiGet<ProviderRow[]>(API.admin.providers.list, undefined, signal),
  })
}

export function useAllCredentials() {
  return useQuery({
    queryKey: [...KEY, 'credentials'],
    queryFn: ({ signal }) => apiGet<Credential[]>(API.admin.credentials, undefined, signal),
  })
}

// Last use per credential. Metrics can be switched off (503); callers then show a dash.
export function useCredentialUsage(provider: string) {
  return useQuery({
    queryKey: ['metrics', 'credentials', provider],
    queryFn: ({ signal }) =>
      apiGet<{ rows: CredentialUsage[] }>(API.admin.metrics.credentials, { provider }, signal),
    select: (d) => new Map(d.rows.map((r) => [r.credential_id, r])),
    retry: false,
    refetchInterval: 30_000,
  })
}

export function useProviderSettings(provider: string, enabled: boolean) {
  return useQuery({
    queryKey: [...KEY, 'settings', provider],
    queryFn: ({ signal }) =>
      apiGet<ProviderSettings>(API.admin.providers.settings(provider), undefined, signal),
    enabled,
  })
}

// Catalog models of one provider matching q, for the allowed/blocked model pickers.
export function useProviderModels(provider: string, q: string, enabled: boolean) {
  return useQuery({
    queryKey: [...CATALOG_KEY, 'provider-models', provider, q],
    queryFn: ({ signal }) =>
      apiGet<{ models: CatalogModel[] }>(
        API.admin.catalog.models,
        { provider, q, limit: 50 },
        signal,
      ),
    select: (d) => [...new Set(d.models.map((m) => m.model_name))],
    enabled,
  })
}

function useInvalidating<V, R>(fn: (v: V) => Promise<R>) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: KEY })
      void queryClient.invalidateQueries({ queryKey: CATALOG_KEY })
    },
  })
}

export const useCreateCredential = () =>
  useInvalidating((c: CredentialCreate) =>
    apiSend<Credential>('POST', API.admin.providers.credentials(c.provider), c),
  )

export const useUpdateCredential = () =>
  useInvalidating(
    ({ provider, id, patch }: { provider: string; id: string; patch: CredentialUpdate }) =>
      apiSend<Credential>('PUT', API.admin.providers.credential(provider, id), patch),
  )

export const useDeleteCredential = () =>
  useInvalidating(({ provider, id }: { provider: string; id: string }) =>
    apiSend<undefined>('DELETE', API.admin.providers.credential(provider, id)),
  )

export const useSaveProviderSettings = () =>
  useInvalidating(
    ({
      provider,
      body,
    }: {
      provider: string
      body: { network_config: NetworkConfig; proxy_config?: ProxyConfig; confirm_unsafe?: boolean }
    }) => apiSend<ProviderRow>('PUT', API.admin.providers.settings(provider), body),
  )
