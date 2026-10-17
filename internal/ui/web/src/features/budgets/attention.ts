import type { Budget } from '@/features/budgets/api'
import {
  isEnforced,
  limitState,
  money,
  percent,
  resetsSoon,
  usedRatio,
  windowAdjective,
} from '@/features/budgets/budget'

export interface BudgetIssue {
  id: string
  title: string
  detail: string
  to: string
}

// Budgets at 80% of their limit or past it, the fullest first. An unused budget is not flagged.
export function budgetIssues(budgets: Budget[], now = Date.now()): BudgetIssue[] {
  return budgets
    .map((b) => ({ b, ratio: usedRatio(b) }))
    .filter(({ ratio }) => limitState(ratio) !== 'ok')
    .sort((x, y) => y.ratio - x.ratio || x.b.name.localeCompare(y.b.name))
    .map(({ b, ratio }) => {
      const enforced = isEnforced(b)
      const window = windowAdjective(b.budget_duration)
      const title =
        ratio < 1
          ? `${b.name} is at ${percent(ratio)} of its ${window} limit`
          : enforced
            ? `${b.name} has reached its ${window} limit`
            : `${b.name} is over its ${window} limit`
      const effect = enforced
        ? 'This budget is enforced, so requests are blocked at the limit.'
        : 'This budget is not enforced, so requests are not blocked.'
      return {
        id: b.id,
        title,
        detail: `${money(b.total_spend ?? 0)} of ${money(b.budget_limit)} · resets ${resetsSoon(b, now)}. ${effect}`,
        to: `/governance/budgets/${encodeURIComponent(b.id)}`,
      }
    })
}
