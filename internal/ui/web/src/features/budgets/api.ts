import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { API } from '@/app/endpoints'
import { apiGet, apiSend } from '@/lib/api'
import { windowParams, type RangeWindow } from '@/lib/time-range'

// core.Budget. Money is nano-USD here, unlike the metrics endpoints, which send USD.
export interface Budget {
  id: string
  name: string
  budget_limit: number
  budget_unit: string
  budget_duration: string
  last_budget_refresh_at: string
  enforce?: boolean
  total_spend?: number
  request_count?: number
  status?: 'unbound' | 'bound' | 'released'
  created_at?: string
}

export interface BudgetInput {
  name: string
  budget_limit: number
  budget_duration: string
  enforce: boolean
}

// An update sends only what changed. The name is fixed once created.
export type BudgetPatch = Partial<Omit<BudgetInput, 'name'>>

// GET /v1/admin/metrics/budgets/:id. Spend per bucket, plus each bucket split by provider and model
// (sparse: a quiet bucket has no rows) so one response serves the chart and the breakdown.
export interface SpendPoint {
  t: string
  spend_usd: number
  requests: number
}

export interface SpendRow extends SpendPoint {
  provider: string
  model: string
}

export interface BudgetSpend {
  budget_id: string
  from: string
  to: string
  bucket_seconds: number
  points: SpendPoint[]
  breakdown: SpendRow[]
}

const BUDGETS = ['governance', 'budgets'] as const

export function useBudgets() {
  return useQuery({
    queryKey: BUDGETS,
    queryFn: ({ signal }) => apiGet<Budget[]>(API.admin.budgets, undefined, signal),
    refetchInterval: 30_000,
  })
}

export function useBudget(id: string) {
  return useQuery({
    queryKey: [...BUDGETS, id],
    queryFn: ({ signal }) => apiGet<Budget>(API.admin.budget(id), undefined, signal),
    enabled: id !== '',
    retry: false,
    refetchInterval: 30_000,
  })
}

// Metrics can be switched off (503): callers show the failure inside the card. `bucket` (seconds) picks
// the bar size when drilling in; `zone` makes bars start on the viewer's hours and midnights.
export function useBudgetSpend(
  id: string,
  window: RangeWindow,
  view: { bucket?: number; zone: string },
) {
  const params = { ...windowParams(window), bucket: view.bucket, tz: view.zone }
  return useQuery({
    queryKey: [
      'metrics',
      'budget-spend',
      id,
      params.from,
      params.to,
      params.bucket ?? 'auto',
      params.tz,
    ],
    queryFn: ({ signal }) => apiGet<BudgetSpend>(API.admin.metrics.budgetSpend(id), params, signal),
    enabled: id !== '',
    placeholderData: keepPreviousData,
    retry: false,
  })
}

function useInvalidating<V, R>(fn: (v: V) => Promise<R>) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['governance'] })
    },
  })
}

export const useCreateBudget = () =>
  useInvalidating((body: BudgetInput) => apiSend<Budget>('POST', API.admin.budgets, body))

export const useUpdateBudget = () =>
  useInvalidating(({ id, patch }: { id: string; patch: BudgetPatch }) =>
    apiSend<Budget>('PUT', API.admin.budget(id), patch),
  )

export const useDeleteBudget = () =>
  useInvalidating((id: string) => apiSend<undefined>('DELETE', API.admin.budget(id)))

export const nanoToUSD = (nano: number) => nano / 1e9

const DAY_MS = 86_400_000
const UNIT_MS: Record<string, number> = {
  d: DAY_MS,
  w: 7 * DAY_MS,
  m: 30 * DAY_MS,
  y: 365 * DAY_MS,
}

// Mirrors core.ParseDuration: "1d", "2w", "1m" (30 days), "1y", or a plain "720h".
export function budgetPeriodMs(duration: string): number | null {
  const match = /^(\d+)([dwmyh])$/i.exec(duration.trim())
  if (!match) return null
  const count = Number(match[1])
  const unit = (match[2] ?? '').toLowerCase()
  return unit === 'h' ? count * 3_600_000 : count * (UNIT_MS[unit] ?? 0) || null
}

// The next reset: last refresh plus one period. Null when the duration cannot be read.
export function nextReset(budget: Budget): Date | null {
  const period = budgetPeriodMs(budget.budget_duration)
  const last = new Date(budget.last_budget_refresh_at).getTime()
  return period && !Number.isNaN(last) ? new Date(last + period) : null
}
