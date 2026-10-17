import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { API } from '@/app/endpoints'
import { apiGet, apiSend } from '@/lib/api'

export interface ProviderConfig {
  provider: string
  allowed_models: string[]
  blocked_models?: string[] | null
  weight?: number
}

export type KeyMode = 'direct' | 'weighted'

// core.VirtualKeyResponse: the secret never comes back, only its display prefix.
export interface VirtualKey {
  id: string
  display_prefix: string
  client_id: string
  budget_id: string
  mode: KeyMode
  load_balancer: string
  provider_configs: ProviderConfig[]
  is_active: boolean
  expires_at?: string
  created_at: string
  updated_at: string
}

export interface KeyList {
  virtual_keys: VirtualKey[]
  count: number
  skipped: number
  skipped_ids: string[]
}

export interface KeyUsage {
  virtual_key_id: string
  last_used_at: string
  requests: number
}

export interface CreateKeyRequest {
  client_id: string
  budget_id: string
  expires_at?: string
  mode: KeyMode
  load_balancer: string
  provider_configs: ProviderConfig[]
}

// The secret is in `key` once, here, and nowhere else.
export interface CreatedKey {
  key: string
  virtual_key: VirtualKey
}

export interface RoutingUpdate {
  mode: KeyMode
  load_balancer: string
  provider_configs: ProviderConfig[]
}

const KEYS = ['governance', 'virtual-keys'] as const // shared with the pricing override picker
const USAGE = ['metrics', 'virtual-key-usage'] as const

export function useKeyList() {
  return useQuery({
    queryKey: KEYS,
    queryFn: ({ signal }) => apiGet<KeyList>(API.admin.virtualKeys, undefined, signal),
  })
}

export function useKey(id: string) {
  return useQuery({
    queryKey: [...KEYS, id],
    queryFn: ({ signal }) => apiGet<VirtualKey>(API.admin.virtualKey(id), undefined, signal),
    retry: false,
  })
}

// Last use and 24h requests per key. Metrics can be switched off (503): callers show a dash.
export function useKeyUsage() {
  return useQuery({
    queryKey: USAGE,
    queryFn: ({ signal }) =>
      apiGet<{ rows: KeyUsage[] }>(API.admin.metrics.virtualKeyUsage, undefined, signal),
    select: (d) => new Map(d.rows.map((r) => [r.virtual_key_id, r])),
    retry: false,
    refetchInterval: 30_000,
  })
}

function useInvalidating<V, R>(fn: (v: V) => Promise<R>) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['governance'] })
      void queryClient.invalidateQueries({ queryKey: USAGE })
    },
  })
}

export const useCreateKey = () =>
  useInvalidating((req: CreateKeyRequest) =>
    apiSend<CreatedKey>('POST', API.admin.virtualKeys, req),
  )

export const useUpdateRouting = () =>
  useInvalidating(({ id, body }: { id: string; body: RoutingUpdate }) =>
    apiSend<VirtualKey>('PUT', API.admin.virtualKeyRouting(id), body),
  )

export const useRotateKey = () =>
  useInvalidating((id: string) => apiSend<CreatedKey>('POST', API.admin.virtualKeyRotate(id)))

export const useSetKeyStatus = () =>
  useInvalidating(({ id, active }: { id: string; active: boolean }) =>
    apiSend<VirtualKey>('PUT', API.admin.virtualKeyStatus(id), { is_active: active }),
  )

// `expires_at` sets a new expiry; null removes it.
export const useSetKeyExpiry = () =>
  useInvalidating(({ id, expiresAt }: { id: string; expiresAt: string | null }) =>
    apiSend<VirtualKey>(
      'PUT',
      API.admin.virtualKeyExpiry(id),
      expiresAt ? { expires_at: expiresAt } : { clear_expiry: true },
    ),
  )

export const useDeleteKey = () =>
  useInvalidating((id: string) => apiSend<undefined>('DELETE', API.admin.virtualKey(id)))
