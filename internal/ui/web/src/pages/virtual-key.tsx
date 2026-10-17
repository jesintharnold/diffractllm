import { format, formatDistanceToNowStrict } from 'date-fns'
import { ArrowLeft, KeyRound, Loader2, RefreshCw, Trash2 } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { toast } from 'sonner'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useBudget,
  useDeleteKey,
  useKey,
  useKeyUsage,
  useRotateKey,
  useSetKeyExpiry,
  useUpdateRouting,
  type Budget,
  type CreatedKey,
  type VirtualKey,
} from '@/features/keys/api'
import { money, periodLabel, resetsIn, spendRatio } from '@/features/keys/budget'
import { RoutingEditor } from '@/features/keys/routing'
import {
  fromConfigs,
  modeOf,
  routingProblem,
  toConfigs,
  type RouteDraft,
} from '@/features/keys/routing-utils'
import { SecretSheet } from '@/features/keys/secret-sheet'
import { ExpiryPicker } from '@/features/providers/form'
import { formatCount } from '@/lib/format'
import { cn } from '@/lib/utils'

const MIN_SAVING_MS = 600 // a local save answers in milliseconds; keep the spinner visible

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono">{children}</span>
    </div>
  )
}

function RoutingCard({ k }: { k: VirtualKey }) {
  const update = useUpdateRouting()
  const initial = fromConfigs(k.provider_configs)
  const [drafts, setDrafts] = useState<RouteDraft[]>(initial)
  const [saving, setSaving] = useState(false)
  const dirty = JSON.stringify(drafts) !== JSON.stringify(initial)
  const problem = routingProblem(drafts)

  const save = async () => {
    if (problem) return
    setSaving(true)
    try {
      await Promise.all([
        update.mutateAsync({
          id: k.id,
          body: {
            mode: modeOf(drafts),
            load_balancer: k.load_balancer,
            provider_configs: toConfigs(drafts),
          },
        }),
        new Promise((r) => setTimeout(r, MIN_SAVING_MS)),
      ])
      toast.success('Routing saved')
    } catch (err) {
      toast.error(`Could not save routing: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="overflow-hidden rounded-xl border bg-card">
      <div className="px-5 pt-4 pb-5">
        <RoutingEditor
          value={drafts}
          onChange={setDrafts}
          lead={<h2 className="text-sm font-semibold">Providers and routing</h2>}
        />
      </div>
      {dirty && (
        <footer className="flex items-center gap-2.5 border-t bg-muted px-5 py-3 duration-200 animate-in fade-in-0 slide-in-from-bottom-1">
          <span className="size-[7px] shrink-0 rounded-full bg-warning" />
          <span
            className={cn(
              'flex-1 truncate text-xs',
              problem ? 'text-destructive' : 'text-muted-foreground',
            )}
          >
            {problem ?? 'Unsaved changes'}
          </span>
          <Button
            variant="outline"
            disabled={saving}
            onClick={() => {
              setDrafts(initial)
            }}
          >
            Discard
          </Button>
          <Button
            onClick={() => void save()}
            disabled={problem !== null || saving}
            className="min-w-28"
          >
            {saving ? (
              <>
                <Loader2 className="animate-spin" />
                Saving…
              </>
            ) : (
              'Save routing'
            )}
          </Button>
        </footer>
      )}
    </section>
  )
}

function BudgetCard({ budgetId }: { budgetId: string }) {
  const budget = useBudget(budgetId)
  const b: Budget | undefined = budget.data
  const ratio = b ? spendRatio(b) : 0
  return (
    <section className="flex flex-col gap-3.5 rounded-xl border bg-card p-5">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold">Budget</h2>
        {b && (
          <Link
            to="/governance/budgets"
            className="text-[11px] font-medium text-primary transition-opacity hover:opacity-80"
          >
            {b.name} →
          </Link>
        )}
      </div>
      {budget.isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : !b ? (
        <p className="text-xs text-muted-foreground">This key's budget no longer exists.</p>
      ) : (
        <>
          <div className="flex flex-col gap-1">
            <span
              className={cn(
                'font-mono text-[28px] leading-8 font-medium',
                ratio >= 1 ? 'text-destructive' : ratio >= 0.8 ? 'text-warning' : 'text-foreground',
              )}
            >
              {money(b.total_spend ?? 0)}
            </span>
            <span className="text-xs text-muted-foreground">
              of {money(b.budget_limit)} · {periodLabel(b.budget_duration)}
            </span>
          </div>
          <div
            className="h-1.5 overflow-hidden rounded-full bg-muted"
            role="progressbar"
            aria-label="Budget used"
            aria-valuenow={Math.round(ratio * 100)}
            aria-valuemin={0}
            aria-valuemax={100}
          >
            <div
              className={cn(
                'h-full rounded-full transition-[width] duration-500',
                ratio >= 1 ? 'bg-destructive' : ratio >= 0.8 ? 'bg-warning' : 'bg-primary',
              )}
              style={{ width: `${String(Math.round(ratio * 100))}%` }}
            />
          </div>
          <div className="flex flex-col gap-2.5">
            <Row label="window resets">{resetsIn(b)}</Row>
            <Row label="requests">{formatCount(b.request_count ?? 0).value}</Row>
            <Row label="on exhaustion">
              {b.enforce === false ? 'tracking only' : 'reject with 429'}
            </Row>
          </div>
        </>
      )}
    </section>
  )
}

function IdentityCard({ k }: { k: VirtualKey }) {
  const usage = useKeyUsage()
  const expiry = useSetKeyExpiry()
  const u = usage.data?.get(k.id)
  return (
    <section className="flex flex-col gap-3.5 rounded-xl border bg-card p-5">
      <h2 className="text-sm font-semibold">Identity</h2>
      <div className="flex flex-col gap-2.5">
        <Row label="key">{k.display_prefix}…</Row>
        <Row label="client">{k.client_id}</Row>
        <Row label="status">{k.is_active ? 'enabled' : 'disabled'}</Row>
        <Row label="created">{format(new Date(k.created_at), 'MMM d, yyyy')}</Row>
        <div className="flex items-center justify-between gap-3 text-xs">
          <span className="text-muted-foreground">expires</span>
          <ExpiryPicker
            compact
            disabled={expiry.isPending}
            value={k.expires_at ? new Date(k.expires_at) : null}
            onChange={(d) => {
              expiry.mutate(
                { id: k.id, expiresAt: d ? d.toISOString() : null },
                {
                  onSuccess: () => toast.success(d ? 'Expiry updated' : 'Expiry removed'),
                  onError: (err) => toast.error(`Could not update the expiry: ${err.message}`),
                },
              )
            }}
          />
        </div>
        <Row label="last used">
          {u ? formatDistanceToNowStrict(new Date(u.last_used_at), { addSuffix: true }) : '—'}
        </Row>
        <Row label="requests · 24h">
          {usage.isError ? '—' : formatCount(u?.requests ?? 0).value}
        </Row>
        <Row label="mode">{k.mode}</Row>
        <Row label="load balancer">{k.load_balancer.replaceAll('_', ' ')}</Row>
      </div>
    </section>
  )
}

// 05.2 · one virtual key: its providers and routing, its budget, and who it is.
export default function VirtualKeyPage() {
  const { id = '' } = useParams()
  const key = useKey(id)
  const navigate = useNavigate()
  const rotate = useRotateKey()
  const remove = useDeleteKey()
  const [confirm, setConfirm] = useState<'rotate' | 'delete' | null>(null)
  const [secret, setSecret] = useState<CreatedKey | null>(null)
  const k = key.data

  const doRotate = () => {
    if (!k) return
    rotate.mutate(k.id, {
      onSuccess: (res) => {
        setSecret(res)
        toast.success('Secret rotated')
      },
      onError: (err) => toast.error(`Could not rotate the secret: ${err.message}`),
    })
  }
  const doDelete = () => {
    if (!k) return
    remove.mutate(k.id, {
      onSuccess: () => {
        toast.success(`${k.client_id} deleted`)
        void navigate('/governance/virtual-keys')
      },
      onError: (err) => toast.error(`Could not delete the key: ${err.message}`),
    })
  }
  // The first click arms the action; the second runs it. Leaving the button disarms it.
  const arm = (what: 'rotate' | 'delete', run: () => void) => () => {
    if (confirm === what) {
      setConfirm(null)
      run()
    } else {
      setConfirm(what)
    }
  }
  const disarm = () => {
    setConfirm(null)
  }

  return (
    <>
      <PageHeader
        title={k?.client_id ?? 'Virtual key'}
        icon={
          <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border bg-muted">
            <KeyRound className="size-[18px] text-primary" />
          </span>
        }
        actions={
          k && (
            <>
              <Button
                variant={confirm === 'rotate' ? 'destructive' : 'outline'}
                disabled={!k.is_active || rotate.isPending}
                title={k.is_active ? undefined : 'Enable the key before rotating its secret'}
                onClick={arm('rotate', doRotate)}
                onBlur={disarm}
              >
                {rotate.isPending ? (
                  <Loader2 className="animate-spin" />
                ) : (
                  <RefreshCw className={confirm === 'rotate' ? '' : 'text-muted-foreground'} />
                )}
                {confirm === 'rotate' ? 'Confirm rotate' : 'Rotate secret'}
              </Button>
              <Button
                variant={confirm === 'delete' ? 'destructive' : 'outline'}
                disabled={remove.isPending}
                className={
                  confirm === 'delete'
                    ? ''
                    : 'border-destructive/60 text-destructive hover:text-destructive'
                }
                onClick={arm('delete', doDelete)}
                onBlur={disarm}
              >
                {remove.isPending ? <Loader2 className="animate-spin" /> : <Trash2 />}
                {confirm === 'delete' ? 'Confirm delete' : 'Delete'}
              </Button>
            </>
          )
        }
      />
      <PageBody>
        <Link
          to="/governance/virtual-keys"
          className="flex w-fit items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="size-3.5" />
          Virtual keys
        </Link>

        {key.isPending ? (
          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_340px]">
            <Skeleton className="h-96 rounded-xl" />
            <Skeleton className="h-96 rounded-xl" />
          </div>
        ) : !k ? (
          <p className="rounded-xl border bg-card p-6 text-sm text-muted-foreground">
            This key was not found. It may have been deleted.
          </p>
        ) : (
          <div className="grid items-start gap-4 duration-300 animate-in fade-in-0 lg:grid-cols-[minmax(0,1fr)_340px]">
            {/* Remount when the saved routing changes, so the form restarts from what is stored. */}
            <RoutingCard key={k.updated_at} k={k} />
            <div className="flex flex-col gap-4">
              <BudgetCard budgetId={k.budget_id} />
              <IdentityCard k={k} />
            </div>
          </div>
        )}
      </PageBody>
      <SecretSheet
        secret={secret?.key ?? null}
        title="Secret rotated"
        subtitle={`${k?.client_id ?? ''} · the old secret no longer works`}
        onClose={() => {
          setSecret(null)
        }}
      />
    </>
  )
}
