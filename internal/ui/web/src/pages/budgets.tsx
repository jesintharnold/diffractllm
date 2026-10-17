import { Plus, ShieldCheck, ShieldOff, TriangleAlert, Wallet } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Pager } from '@/components/pager'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useBudgets, type Budget } from '@/features/budgets/api'
import { budgetIssues } from '@/features/budgets/attention'
import { BudgetSheet } from '@/features/budgets/budget-sheet'
import {
  STATE_BG,
  STATE_TEXT,
  isEnforced,
  limitState,
  money,
  percent,
  resetsSoon,
  usedRatio,
  windowName,
} from '@/features/budgets/budget'
import { SquareMeter } from '@/components/charts/square-meter'
import { useKeyList } from '@/features/keys/api'
import { useClock } from '@/lib/clock'
import { cn } from '@/lib/utils'

const PAGE = 10
const head =
  'h-[38px] px-1.5 text-[11px] font-semibold tracking-[0.06em] text-muted-foreground uppercase'

// 06.1 · Budgets: which ones are close to their limit, then every budget with its spend and window.
export default function BudgetsPage() {
  const budgets = useBudgets()
  const keys = useKeyList()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const [page, setPage] = useState(1)
  // `budget` stays set while the drawer closes, so it does not flash back to the create form.
  const [sheet, setSheet] = useState<{ open: boolean; budget: Budget | null }>({
    open: params.get('new') === '1', // the key form's "Create new budget" lands here
    budget: null,
  })
  const now = useClock(60_000) // keeps "in 11 h" and the limit warnings current

  const all = budgets.data ?? []
  const issues = budgetIssues(all, now)
  const sorted = [...all].sort(
    (a, b) => usedRatio(b) - usedRatio(a) || a.name.localeCompare(b.name),
  )
  const pages = Math.max(1, Math.ceil(sorted.length / PAGE))
  const current = Math.min(page, pages)
  const visible = sorted.slice((current - 1) * PAGE, current * PAGE)

  // The key a budget serves, if any (a budget serves one active key).
  const clientOf = (b: Budget) =>
    keys.data?.virtual_keys.find((k) => k.budget_id === b.id && k.is_active)?.client_id

  return (
    <>
      <PageHeader
        title="Budgets"
        actions={
          <Button
            onClick={() => {
              setSheet({ open: true, budget: null })
            }}
          >
            <Plus />
            Create budget
          </Button>
        }
      />
      <PageBody>
        {issues.length > 0 && (
          <section className="overflow-hidden rounded-xl border border-warning/60 bg-card duration-300 animate-in fade-in-0">
            <header className="flex items-center gap-2 border-b px-4 py-3">
              <TriangleAlert className="size-3.5 text-warning" />
              <h2 className="text-[13px] font-semibold">Needs attention</h2>
              <span className="font-mono text-xs font-semibold text-warning">{issues.length}</span>
            </header>
            {issues.map((i) => (
              <Link
                key={i.id}
                to={i.to}
                className="flex items-center gap-3 border-b px-4 py-2.5 transition-colors last:border-b-0 hover:bg-nav-selected"
              >
                <Wallet className="size-4 shrink-0 text-muted-foreground" />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="truncate text-xs font-medium">{i.title}</span>
                  <span className="text-[11px] text-muted-foreground">{i.detail}</span>
                </span>
                <span className="text-[11px] font-medium text-primary">Review →</span>
              </Link>
            ))}
          </section>
        )}

        <div className="flex flex-col gap-3">
          <section className="overflow-hidden rounded-xl border bg-card">
            <Table className="text-[13px] leading-[18px]">
              <TableHeader className="bg-background">
                <TableRow className="hover:bg-transparent">
                  <TableHead className={`${head} w-44 pl-5`}>Budget</TableHead>
                  <TableHead className={`${head} w-40`}>Client</TableHead>
                  <TableHead className={`${head} w-80`}>Spend / limit</TableHead>
                  <TableHead className={`${head} w-48`}>Resets</TableHead>
                  <TableHead className={head}>Enforcement</TableHead>
                  <TableHead className={`${head} w-16 pr-5 text-right`} aria-label="Actions" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {budgets.isPending ? (
                  Array.from({ length: 3 }, (_, i) => (
                    <TableRow key={i} className="h-14 hover:bg-transparent">
                      <TableCell colSpan={6} className="px-5">
                        <Skeleton className="h-4 w-full" />
                      </TableCell>
                    </TableRow>
                  ))
                ) : budgets.isError ? (
                  <TableRow className="hover:bg-transparent">
                    <TableCell colSpan={6} className="h-32 text-center text-sm text-destructive">
                      Could not load budgets: {budgets.error.message}
                    </TableCell>
                  </TableRow>
                ) : visible.length === 0 ? (
                  <TableRow className="hover:bg-transparent">
                    <TableCell
                      colSpan={6}
                      className="h-32 text-center text-sm text-muted-foreground"
                    >
                      No budgets yet. Create one to cap what a key can spend.
                    </TableCell>
                  </TableRow>
                ) : (
                  visible.map((b) => {
                    const client = clientOf(b)
                    const ratio = usedRatio(b)
                    const state = limitState(ratio)
                    const idle = client === undefined
                    const enforced = isEnforced(b)
                    return (
                      <TableRow
                        key={b.id}
                        onClick={() => {
                          void navigate(`/governance/budgets/${encodeURIComponent(b.id)}`)
                        }}
                        className={cn(
                          'h-14 cursor-pointer transition-colors duration-200 hover:bg-nav-selected',
                          idle && 'text-muted-foreground',
                        )}
                      >
                        <TableCell className="pl-5 font-mono">{b.name}</TableCell>
                        <TableCell className="px-1.5 font-mono text-muted-foreground">
                          {client ?? 'Not assigned'}
                        </TableCell>
                        <TableCell className="px-1.5 pr-7">
                          <div className="flex flex-col gap-1.5">
                            <div className="flex items-baseline gap-1.5 font-mono text-xs">
                              <span className={idle ? undefined : 'text-foreground'}>
                                {money(b.total_spend ?? 0)}
                              </span>
                              <span className="text-muted-foreground">
                                / {money(b.budget_limit)}
                              </span>
                              <span className="flex-1" />
                              <span
                                className={cn('text-[11px]', idle ? undefined : STATE_TEXT[state])}
                              >
                                {percent(ratio)}
                              </span>
                            </div>
                            <SquareMeter
                              ratio={ratio}
                              fill={STATE_BG[state]}
                              label={`${b.name} spend against its limit`}
                            />
                          </div>
                        </TableCell>
                        <TableCell className="px-1.5 font-mono">
                          <div className="flex flex-col gap-0.5">
                            <span className={idle ? undefined : 'text-foreground'}>
                              {windowName(b.budget_duration)}
                            </span>
                            <span className="text-[11px] text-muted-foreground">
                              {resetsSoon(b, now)}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell className="px-1.5">
                          <span className="flex items-center gap-1.5 text-xs">
                            {enforced ? (
                              <ShieldCheck className="size-3.5 text-muted-foreground" />
                            ) : (
                              <ShieldOff className="size-3.5 text-muted-foreground" />
                            )}
                            <span className={idle ? undefined : 'text-foreground'}>
                              {enforced ? 'Enforced' : 'Not enforced'}
                            </span>
                          </span>
                        </TableCell>
                        <TableCell className="px-1.5 pr-5 text-right">
                          <Button
                            variant="link"
                            size="sm"
                            className="h-auto p-0 text-xs"
                            onClick={(e) => {
                              e.stopPropagation()
                              setSheet({ open: true, budget: b })
                            }}
                          >
                            Edit
                          </Button>
                        </TableCell>
                      </TableRow>
                    )
                  })
                )}
              </TableBody>
            </Table>
          </section>
          {sorted.length > 0 && (
            <Pager
              summary={`Showing ${String(visible.length)} of ${String(sorted.length)} ${sorted.length === 1 ? 'budget' : 'budgets'}`}
              page={current}
              pages={pages}
              onPage={setPage}
            />
          )}
        </div>
      </PageBody>
      <BudgetSheet
        open={sheet.open}
        onOpenChange={(open) => {
          setSheet((s) => ({ ...s, open }))
          if (!open && params.has('new')) setParams({}, { replace: true })
        }}
        budget={sheet.budget}
        boundClient={sheet.budget ? clientOf(sheet.budget) : undefined}
      />
    </>
  )
}
