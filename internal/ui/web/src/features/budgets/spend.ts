import type { SpendRow } from '@/features/budgets/api'

export interface ModelSpend {
  provider: string
  model: string
  spend_usd: number
  requests: number
}

// The per-bucket rows added up by provider and model, biggest spend first.
export function aggregate(rows: SpendRow[]): ModelSpend[] {
  const sums = new Map<string, ModelSpend>()
  for (const r of rows) {
    const key = `${r.provider}\u0000${r.model}`
    const sum = sums.get(key) ?? { provider: r.provider, model: r.model, spend_usd: 0, requests: 0 }
    sum.spend_usd += r.spend_usd
    sum.requests += r.requests
    sums.set(key, sum)
  }
  return [...sums.values()].sort(
    (a, b) =>
      b.spend_usd - a.spend_usd || b.requests - a.requests || a.model.localeCompare(b.model),
  )
}
