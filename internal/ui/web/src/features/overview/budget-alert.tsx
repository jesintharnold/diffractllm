import { TriangleAlert } from 'lucide-react'
import { Link } from 'react-router'
import { nanoToUSD, nextReset, useBudgets, type Budget } from '@/features/budgets/api'
import { useClock } from '@/lib/clock'
import { formatUSD } from '@/lib/format'
import { cn } from '@/lib/utils'

// Shown only when a budget reaches this share of its cap (same warn line as the budget chart).
const WARN_RATIO = 0.8

function ratioOf(budget: Budget): number {
  return budget.budget_limit > 0 ? (budget.total_spend ?? 0) / budget.budget_limit : 0
}

// "in 11 h", "in 4 d", "in 35 min"
function untilText(when: Date, now: number): string {
  const ms = when.getTime() - now
  if (ms <= 60_000) return 'shortly'
  const minutes = Math.round(ms / 60_000)
  if (minutes < 60) return `in ${String(minutes)} min`
  const hours = Math.round(ms / 3_600_000)
  return hours < 48 ? `in ${String(hours)} h` : `in ${String(Math.round(hours / 24))} d`
}

// Overview's one budget signal: the budget closest to (or past) its cap, if any crossed 80%.
// It is an alert, not a section, so it stays silent while loading or if budgets fail to load.
export function BudgetAlert() {
  const { data } = useBudgets()
  const now = useClock(60_000)
  const hot = (data ?? [])
    .filter((b) => ratioOf(b) >= WARN_RATIO)
    .sort((a, b) => ratioOf(b) - ratioOf(a))
  const top = hot[0]
  if (!top) return null

  const ratio = ratioOf(top)
  const over = ratio >= 1
  const enforced = top.enforce !== false
  const reset = nextReset(top)
  const cap = formatUSD(nanoToUSD(top.budget_limit)).value

  const details = [
    over
      ? `has reached its ${cap} cap`
      : `is at ${String(Math.floor(ratio * 100))}% of its ${cap} cap`,
    enforced ? 'enforced, requests are rejected past 100%' : 'monitor only',
    reset ? `resets ${untilText(reset, now)}` : null,
    hot.length > 1 ? `+${String(hot.length - 1)} more` : null,
  ].filter(Boolean)

  return (
    <div
      role="status"
      className={cn(
        'flex flex-wrap items-center gap-x-2.5 gap-y-1 rounded-lg border bg-card px-3.5 py-2.5 text-xs',
        over ? 'border-destructive' : 'border-warning',
      )}
    >
      <TriangleAlert
        className={cn('size-3.5 shrink-0', over ? 'text-destructive' : 'text-warning')}
        aria-hidden
      />
      <span className="font-semibold">{top.name}</span>
      <span className="min-w-0 flex-1 text-muted-foreground">{details.join(' · ')}</span>
      <Link
        to={`/governance/budgets/${encodeURIComponent(top.id)}`}
        viewTransition
        className="text-[11px] font-medium text-primary underline-offset-4 hover:underline"
      >
        View budget
      </Link>
    </div>
  )
}
