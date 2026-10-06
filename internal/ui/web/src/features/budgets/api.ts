import { useQuery } from '@tanstack/react-query'
import { API } from '@/app/endpoints'
import { apiGet } from '@/lib/api'

// GET /v1/admin/budgets (core.Budget). Money is nano-USD here, unlike the metrics endpoints.
export interface Budget {
  id: string
  name: string
  budget_limit: number
  budget_duration: string
  last_budget_refresh_at: string
  enforce?: boolean
  total_spend?: number
}

export function useBudgets() {
  return useQuery({
    queryKey: ['budgets'],
    queryFn: ({ signal }) => apiGet<Budget[]>(API.admin.budgets, undefined, signal),
    refetchInterval: 60_000,
  })
}

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
