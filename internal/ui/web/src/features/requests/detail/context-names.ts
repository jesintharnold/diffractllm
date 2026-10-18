import { useBudgets } from '@/features/budgets/api'
import { useKeyList } from '@/features/keys/api'
import type { RequestDetail } from '@/features/requests/api'
import { shortId } from '@/features/requests/detail/format'

export interface Named {
  label: string
  to?: string // where the key or budget is managed, when it still exists
}

// A request stores ids. The key list and the budget list turn them into what an admin recognises:
// dk-8fa2c41e…, acme-prod. An id that no longer resolves (a deleted key) stays as its short form.
export interface ContextNames {
  key?: Named
  budget?: Named
}

export function useContextNames(d: RequestDetail | undefined): ContextNames {
  const keys = useKeyList().data?.virtual_keys
  const budgets = useBudgets().data
  const { virtual_key_id: keyId, budget_id: budgetId } = d?.context ?? {}

  const key = keyId ? keys?.find((k) => k.id === keyId) : undefined
  const budget = budgetId ? budgets?.find((b) => b.id === budgetId) : undefined

  return {
    key: keyId
      ? {
          label: key ? `${key.display_prefix}…` : shortId(keyId),
          to: key && `/governance/virtual-keys/${encodeURIComponent(key.id)}`,
        }
      : undefined,
    budget: budgetId
      ? {
          label: budget ? budget.name : shortId(budgetId),
          to: budget && `/governance/budgets/${encodeURIComponent(budget.id)}`,
        }
      : undefined,
  }
}
