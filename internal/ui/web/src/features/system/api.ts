import { useQuery } from '@tanstack/react-query'
import { API } from '@/app/endpoints'
import { ApiError, apiGet } from '@/lib/api'

// Gateway health for the sidebar footer (GET /ready).
export interface Readiness {
  status: 'ready' | 'not ready'
  checks: Record<string, boolean>
}

// /ready answers 503 with the same body when a check fails, so both are a valid answer.
async function fetchReadiness(signal: AbortSignal): Promise<Readiness> {
  try {
    return await apiGet<Readiness>(API.ready, undefined, signal)
  } catch (err) {
    if (err instanceof ApiError && err.status === 503) return { status: 'not ready', checks: {} }
    throw err
  }
}

// Build facts (GET /v1/info). They never change while the binary runs, so fetch once.
export interface GatewayInfo {
  version: string
  license: 'free' | 'commercial'
}

export function useGatewayInfo() {
  return useQuery({
    queryKey: ['system', 'info'],
    queryFn: ({ signal }) => apiGet<GatewayInfo>(API.info, undefined, signal),
    staleTime: Infinity,
  })
}

export function useReadiness() {
  return useQuery({
    queryKey: ['system', 'ready'],
    queryFn: ({ signal }) => fetchReadiness(signal),
    refetchInterval: 15_000,
  })
}
