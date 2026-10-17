import {
  ArrowLeft,
  ArrowRight,
  KeyRound,
  Loader2,
  Pencil,
  ShieldCheck,
  ShieldOff,
  Trash2,
  Wallet,
} from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { toast } from 'sonner'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useBudget, useDeleteBudget, type Budget } from '@/features/budgets/api'
import { BudgetSheet } from '@/features/budgets/budget-sheet'
import {
  STATE_TEXT,
  elapsedRatio,
  isEnforced,
  limitState,
  money,
  percent,
  resetAt,
  span,
  statusLabel,
  usedRatio,
  windowLength,
  windowName,
  windowSeconds,
  type LimitState,
} from '@/features/budgets/budget'
import { SpendSection } from '@/features/budgets/spend-section'
import { useKeyList } from '@/features/keys/api'
import { useClock } from '@/lib/clock'
import { formatCount, formatUSD } from '@/lib/format'
import { formatInZone, useTimezone } from '@/lib/timezone'
import { cn } from '@/lib/utils'

const CHIP_TONE: Record<LimitState, string> = {
  ok: 'border-success/30 bg-success/10 text-success',
  near: 'border-warning/30 bg-warning/10 text-warning',
  reached: 'border-destructive/30 bg-destructive/10 text-destructive',
}

function Tile({
  label,
  value,
  sub,
  valueClass,
}: {
  label: string
  value: string
  sub: string
  valueClass?: string
}) {
  return (
    <div className="flex flex-col gap-1.5 rounded-xl border bg-card px-[18px] py-4">
      <span className="text-[10px] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
        {label}
      </span>
      <span className={cn('font-mono text-2xl leading-8 font-medium tabular-nums', valueClass)}>
        {value}
      </span>
      <span className="text-[11px] text-muted-foreground">{sub}</span>
    </div>
  )
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono">{children}</span>
    </div>
  )
}

function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3.5 rounded-xl border bg-card p-5">
      <h2 className="text-sm font-semibold">{title}</h2>
      {children}
    </section>
  )
}

function Details({ b }: { b: Budget }) {
  const { zone } = useTimezone()
  const seconds = windowSeconds(b.budget_duration)
  const end = resetAt(b)
  const created = b.created_at ? new Date(b.created_at) : null
  return (
    <div className="flex flex-col gap-4 duration-300 animate-in fade-in-0">
      <Row label="limit">{money(b.budget_limit)}</Row>
      <Row label="window">
        {windowName(b.budget_duration)}
        {seconds !== null && ` · ${windowLength(seconds)}`}
      </Row>
      <Row label="window started">
        {formatInZone(new Date(b.last_budget_refresh_at), zone, 'MMM d, HH:mm')}
      </Row>
      <Row label="next reset">
        {end === null ? '—' : formatInZone(new Date(end), zone, 'MMM d, HH:mm')}
      </Row>
      <Row label="enforcement">{isEnforced(b) ? 'Enforced' : 'Not enforced'}</Row>
      {created && created.getFullYear() > 2000 && (
        <Row label="created">{formatInZone(created, zone, 'MMM d, yyyy')}</Row>
      )}
    </div>
  )
}

// 06.2 · one budget: how it stands this window, what it spent over a range, and what that went on.
export default function BudgetPage() {
  const { id = '' } = useParams()
  const budget = useBudget(id)
  const keys = useKeyList()
  const remove = useDeleteBudget()
  const navigate = useNavigate()
  const { zone } = useTimezone()
  const now = useClock(30_000)
  const [editing, setEditing] = useState(false)
  const [armed, setArmed] = useState(false)
  const b = budget.data

  const key = b
    ? keys.data?.virtual_keys.find((k) => k.budget_id === b.id && k.is_active)
    : undefined
  const bound = b?.status === 'bound'

  const doDelete = () => {
    if (!b) return
    if (!armed) {
      setArmed(true)
      return
    }
    setArmed(false)
    remove.mutate(b.id, {
      onSuccess: () => {
        toast.success(`${b.name} deleted`)
        void navigate('/governance/budgets')
      },
      onError: (err) => toast.error(`Could not delete the budget: ${err.message}`),
    })
  }

  const ratio = b ? usedRatio(b) : 0
  const state = limitState(ratio)
  const end = b ? resetAt(b) : null
  const elapsed = b ? elapsedRatio(b, now) : null
  const spent = b?.total_spend ?? 0
  const requests = b?.request_count ?? 0

  return (
    <>
      <PageHeader
        title={b?.name ?? 'Budget'}
        icon={
          <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border bg-muted">
            <Wallet className="size-[18px] text-primary" />
          </span>
        }
        badges={
          b && (
            <>
              <span className="flex items-center gap-1.5 rounded-full border bg-foreground/5 px-2.5 py-1 text-[11px] font-medium">
                {isEnforced(b) ? (
                  <ShieldCheck className="size-3 text-muted-foreground" />
                ) : (
                  <ShieldOff className="size-3 text-muted-foreground" />
                )}
                {isEnforced(b) ? 'Enforced' : 'Not enforced'}
              </span>
              <span
                className={cn(
                  'flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] font-medium',
                  CHIP_TONE[state],
                )}
              >
                <span className="size-1.5 rounded-full bg-current" />
                {statusLabel(b)}
              </span>
            </>
          )
        }
        actions={
          b && (
            <>
              <Button
                variant="outline"
                onClick={() => {
                  setEditing(true)
                }}
              >
                <Pencil className="text-muted-foreground" />
                Edit budget
              </Button>
              <Button
                variant={armed ? 'destructive' : 'outline'}
                disabled={bound || remove.isPending}
                title={bound ? 'Disable or delete the key that uses this budget first' : undefined}
                className={
                  armed ? '' : 'border-destructive/60 text-destructive hover:text-destructive'
                }
                onClick={doDelete}
                onBlur={() => {
                  setArmed(false)
                }}
              >
                {remove.isPending ? <Loader2 className="animate-spin" /> : <Trash2 />}
                {armed ? 'Confirm delete' : 'Delete'}
              </Button>
            </>
          )
        }
      />
      <PageBody>
        <Link
          to="/governance/budgets"
          className="flex w-fit items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="size-3.5" />
          Budgets
        </Link>

        {budget.isPending ? (
          <div className="grid gap-4 lg:grid-cols-4">
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} className="h-28 rounded-xl" />
            ))}
          </div>
        ) : !b ? (
          <p className="rounded-xl border bg-card p-6 text-sm text-muted-foreground">
            This budget was not found. It may have been deleted.
          </p>
        ) : (
          <>
            <div className="grid gap-4 duration-300 animate-in fade-in-0 sm:grid-cols-2 xl:grid-cols-4">
              <Tile
                label="Spent this window"
                value={money(spent)}
                valueClass={state === 'ok' ? undefined : STATE_TEXT[state]}
                sub={`${percent(ratio)} of the ${money(b.budget_limit)} limit`}
              />
              <Tile
                label="Limit"
                value={money(b.budget_limit)}
                sub={`${windowName(b.budget_duration)} · resets every ${windowLength(windowSeconds(b.budget_duration) ?? 0)}`}
              />
              <Tile
                label="Resets in"
                value={end === null || end - now <= 0 ? 'now' : span(end - now)}
                sub={
                  end === null
                    ? '—'
                    : `${formatInZone(new Date(end), zone, 'MMM d, HH:mm')}${elapsed === null ? '' : ` · ${percent(elapsed)} of the window elapsed`}`
                }
              />
              <Tile
                label="Requests this window"
                value={formatCount(requests).value}
                sub={
                  requests > 0
                    ? `${formatUSD(spent / 1e9 / requests).value} average per request`
                    : 'No requests yet'
                }
              />
            </div>

            <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_340px]">
              <div className="flex min-w-0 flex-col gap-4">
                <SpendSection budgetId={b.id} />
              </div>
              <div className="flex flex-col gap-4">
                <Card title="Configuration">
                  <Details b={b} />
                </Card>
                <Card title="Key">
                  {key ? (
                    <Link
                      to={`/governance/virtual-keys/${key.id}`}
                      className="group flex items-center gap-2.5"
                    >
                      <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border bg-muted">
                        <KeyRound className="size-4 text-primary" />
                      </span>
                      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                        <span className="truncate font-mono text-xs">{key.client_id}</span>
                        <span className="truncate font-mono text-[11px] text-muted-foreground">
                          {key.display_prefix}… · enabled
                        </span>
                      </span>
                      <ArrowRight className="size-3.5 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
                    </Link>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      Not assigned to a key. Choose this budget when you create one.
                    </p>
                  )}
                </Card>
              </div>
            </div>
          </>
        )}
      </PageBody>
      {b && (
        <BudgetSheet
          open={editing}
          onOpenChange={setEditing}
          budget={b}
          boundClient={key?.client_id}
          onDeleted={() => {
            void navigate('/governance/budgets')
          }}
        />
      )}
    </>
  )
}
