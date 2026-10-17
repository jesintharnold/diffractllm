import { formatDistanceToNowStrict } from 'date-fns'
import { KeyRound, Plus, TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { toast } from 'sonner'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Pager } from '@/components/pager'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  useKeyList,
  useKeyUsage,
  useSetKeyStatus,
  type CreatedKey,
  type VirtualKey,
} from '@/features/keys/api'
import { keyIssues } from '@/features/keys/attention'
import { useBudgets } from '@/features/budgets/api'
import { money } from '@/features/budgets/budget'
import { CreateKeySheet } from '@/features/keys/create-sheet'
import { SecretSheet } from '@/features/keys/secret-sheet'
import { expiryDue, expiryText } from '@/features/providers/attention'
import { useClock } from '@/lib/clock'
import { cn } from '@/lib/utils'

const PAGE = 10
const head =
  'h-[38px] px-1.5 text-[11px] font-semibold tracking-[0.06em] text-muted-foreground uppercase'

function EnabledSwitch({ k }: { k: VirtualKey }) {
  const status = useSetKeyStatus()
  return (
    <Switch
      checked={k.is_active}
      disabled={status.isPending}
      aria-label={`${k.is_active ? 'Disable' : 'Enable'} ${k.client_id}`}
      onCheckedChange={(active) => {
        status.mutate(
          { id: k.id, active },
          {
            onSuccess: () => toast.success(`${k.client_id} ${active ? 'enabled' : 'disabled'}`),
            onError: (err) => toast.error(`Could not update ${k.client_id}: ${err.message}`),
          },
        )
      }}
    />
  )
}

// 05.1 · Virtual keys: what needs attention, then every key with its spend and an Enabled switch.
export default function VirtualKeysPage() {
  const list = useKeyList()
  const budgets = useBudgets()
  const usage = useKeyUsage()
  const navigate = useNavigate()
  const [page, setPage] = useState(1)
  const [creating, setCreating] = useState(false)
  const [created, setCreated] = useState<CreatedKey | null>(null)
  useClock(60_000) // keeps "in 3 days" and "2 min ago" current

  const keys = list.data?.virtual_keys ?? []
  const budgetById = new Map((budgets.data ?? []).map((b) => [b.id, b]))
  const issues = list.data ? keyIssues(list.data) : []
  const enabled = keys.filter((k) => k.is_active).length

  const pages = Math.max(1, Math.ceil(keys.length / PAGE))
  const current = Math.min(page, pages)
  const visible = keys.slice((current - 1) * PAGE, current * PAGE)

  const spend = (k: VirtualKey) => {
    const b = budgetById.get(k.budget_id)
    if (!b) return { text: '—', warn: false }
    return {
      text: `${money(b.total_spend ?? 0)} / ${money(b.budget_limit)}`,
      warn: b.budget_limit > 0 && (b.total_spend ?? 0) / b.budget_limit >= 0.8,
    }
  }
  const lastUsed = (k: VirtualKey) => {
    const u = usage.data?.get(k.id)
    return u ? formatDistanceToNowStrict(new Date(u.last_used_at), { addSuffix: true }) : '—'
  }

  return (
    <>
      <PageHeader
        title="Virtual keys"
        actions={
          <Button
            onClick={() => {
              setCreating(true)
            }}
          >
            <Plus />
            Create key
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
            {issues.map((i) => {
              const row = (
                <>
                  <KeyRound className="size-4 shrink-0 text-muted-foreground" />
                  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="truncate text-xs font-medium">{i.title}</span>
                    <span className="text-[11px] text-muted-foreground">{i.detail}</span>
                  </span>
                  {i.to && <span className="text-[11px] font-medium text-primary">Review →</span>}
                </>
              )
              const cls = 'flex items-center gap-3 border-b px-4 py-2.5 last:border-b-0'
              return i.to ? (
                <Link
                  key={i.id}
                  to={i.to}
                  className={cn(cls, 'transition-colors hover:bg-nav-selected')}
                >
                  {row}
                </Link>
              ) : (
                <div key={i.id} className={cls}>
                  {row}
                </div>
              )
            })}
          </section>
        )}

        <div className="flex flex-col gap-3">
          <section className="overflow-hidden rounded-xl border bg-card">
            <header className="flex flex-col gap-1 px-5 pt-4 pb-3">
              <h2 className="text-sm font-semibold">Keys</h2>
              <span className="text-[11px] text-muted-foreground">
                Clients call the gateway with these keys. The secret is shown once, at creation.
              </span>
            </header>
            <Table className="text-[13px] leading-[18px]">
              <TableHeader className="bg-background">
                <TableRow className="hover:bg-transparent">
                  <TableHead className={`${head} w-40 pl-5`}>Key</TableHead>
                  <TableHead className={`${head} w-40`}>Client</TableHead>
                  <TableHead className={head}>Budget</TableHead>
                  <TableHead className={`${head} w-44`}>Spend</TableHead>
                  <TableHead className={`${head} w-44`}>Expires</TableHead>
                  <TableHead className={`${head} w-32`}>Last used</TableHead>
                  <TableHead className={`${head} w-24`}>Enabled</TableHead>
                  <TableHead className={`${head} w-16 pr-5 text-right`} aria-label="Actions" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.isPending ? (
                  Array.from({ length: 3 }, (_, i) => (
                    <TableRow key={i} className="h-12 hover:bg-transparent">
                      <TableCell colSpan={8} className="px-5">
                        <Skeleton className="h-4 w-full" />
                      </TableCell>
                    </TableRow>
                  ))
                ) : list.isError ? (
                  <TableRow className="hover:bg-transparent">
                    <TableCell colSpan={8} className="h-32 text-center text-sm text-destructive">
                      Could not load keys: {list.error.message}
                    </TableCell>
                  </TableRow>
                ) : visible.length === 0 ? (
                  <TableRow className="hover:bg-transparent">
                    <TableCell
                      colSpan={8}
                      className="h-32 text-center text-sm text-muted-foreground"
                    >
                      No virtual keys yet. Create one to let a client call the gateway.
                    </TableCell>
                  </TableRow>
                ) : (
                  visible.map((k) => {
                    const s = spend(k)
                    return (
                      <TableRow
                        key={k.id}
                        onClick={() => {
                          void navigate(`/governance/virtual-keys/${k.id}`)
                        }}
                        className={cn(
                          'h-12 cursor-pointer transition-colors duration-200 hover:bg-nav-selected',
                          !k.is_active && 'text-muted-foreground',
                        )}
                      >
                        <TableCell className="pl-5 font-mono text-muted-foreground">
                          {k.display_prefix}…
                        </TableCell>
                        <TableCell className="px-1.5 font-mono">{k.client_id}</TableCell>
                        <TableCell className="px-1.5 font-mono text-muted-foreground">
                          {budgetById.get(k.budget_id)?.name ?? '—'}
                        </TableCell>
                        <TableCell
                          className={cn(
                            'px-1.5 font-mono',
                            s.warn ? 'text-warning' : 'text-muted-foreground',
                          )}
                        >
                          {s.text}
                        </TableCell>
                        <TableCell
                          className={cn(
                            'px-1.5 font-mono',
                            k.is_active && expiryDue(k.expires_at)
                              ? 'text-warning'
                              : 'text-muted-foreground',
                          )}
                        >
                          {expiryText(k.expires_at)}
                        </TableCell>
                        <TableCell className="px-1.5 font-mono text-muted-foreground">
                          {lastUsed(k)}
                        </TableCell>
                        <TableCell
                          className="px-1.5"
                          onClick={(e) => {
                            e.stopPropagation()
                          }}
                        >
                          <EnabledSwitch k={k} />
                        </TableCell>
                        <TableCell className="px-1.5 pr-5 text-right">
                          <Button variant="link" size="sm" className="h-auto p-0 text-xs" asChild>
                            <Link
                              to={`/governance/virtual-keys/${k.id}`}
                              onClick={(e) => {
                                e.stopPropagation()
                              }}
                            >
                              Edit
                            </Link>
                          </Button>
                        </TableCell>
                      </TableRow>
                    )
                  })
                )}
              </TableBody>
            </Table>
          </section>
          {keys.length > 0 && (
            <Pager
              summary={`Showing ${String((current - 1) * PAGE + 1)}–${String(Math.min(current * PAGE, keys.length))} of ${String(keys.length)} ${keys.length === 1 ? 'key' : 'keys'} · ${String(enabled)} enabled · ${String(keys.length - enabled)} disabled`}
              page={current}
              pages={pages}
              onPage={setPage}
            />
          )}
        </div>
      </PageBody>
      <CreateKeySheet open={creating} onOpenChange={setCreating} onCreated={setCreated} />
      <SecretSheet
        secret={created?.key ?? null}
        title="Key created"
        subtitle={`${created?.virtual_key.client_id ?? ''} · copy the secret now`}
        onClose={() => {
          setCreated(null)
        }}
      />
    </>
  )
}
