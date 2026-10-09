import { formatDistanceToNowStrict } from 'date-fns'
import { ArrowLeft, Plus, Settings } from 'lucide-react'
import { Fragment, useState } from 'react'
import { Link, useParams } from 'react-router'
import { toast } from 'sonner'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Pager } from '@/components/pager'
import { ProviderLogo } from '@/components/provider-logo'
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
  isSupported,
  useAllCredentials,
  useCredentialUsage,
  useProviders,
  useUpdateCredential,
  type Credential,
} from '@/features/providers/api'
import { EXPIRY_WARNING_DAYS, expiryLabel, isLive, issueOf } from '@/features/providers/attention'
import { CredentialSheet } from '@/features/providers/credential-sheet'
import { ProviderSettingsSheet } from '@/features/providers/settings-sheet'
import { useClock } from '@/lib/clock'
import { providerLabel } from '@/lib/provider-colors'
import { cn } from '@/lib/utils'

const PAGE = 10
const head =
  'h-[38px] px-1.5 text-[11px] font-semibold tracking-[0.06em] text-muted-foreground uppercase'

const AUTH: Record<string, string> = {
  azure_api_key: 'API key',
  azure_service_principal: 'Entra ID',
  azure_default_credential: 'Default credential',
}

const modelsLabel = (c: Credential) =>
  c.allowed_models.includes('*') || c.allowed_models.length === 0
    ? 'all'
    : c.allowed_models.join(', ')

function EnabledSwitch({ c }: { c: Credential }) {
  const update = useUpdateCredential()
  return (
    <Switch
      checked={c.enabled}
      disabled={update.isPending}
      aria-label={`${c.enabled ? 'Disable' : 'Enable'} ${c.name}`}
      onCheckedChange={(enabled) => {
        update.mutate(
          { provider: c.provider, id: c.id, patch: { enabled } },
          {
            onSuccess: () => toast.success(`${c.name} ${enabled ? 'enabled' : 'disabled'}`),
            onError: (err) => toast.error(`Could not update ${c.name}: ${err.message}`),
          },
        )
      }}
    />
  )
}

// 04.2 · one provider: its credentials, each with an Enabled switch; rows that need action stand out.
export default function ProviderPage() {
  const { provider = '' } = useParams()
  const creds = useAllCredentials()
  const providers = useProviders()
  const usage = useCredentialUsage(provider)
  const [page, setPage] = useState(1)
  const [sheet, setSheet] = useState<{ open: boolean; editing: Credential | null }>({
    open: false,
    editing: null,
  })
  const [settingsOpen, setSettingsOpen] = useState(false)
  useClock(30_000) // keeps "3 min ago" and expiry labels current

  const azure = provider === 'azure'
  const label = providerLabel(provider)
  const rows = (creds.data ?? [])
    .filter((c) => c.provider === provider)
    .sort((a, b) => a.name.localeCompare(b.name))
  const row = providers.data?.find((p) => p.name === provider)
  const supported = isSupported(provider) && row?.adapter_enabled !== false
  const expiring = rows.filter((c) => {
    const i = issueOf(c)
    return i !== null && i.kind !== 'disabled'
  }).length
  const attention = rows.filter((c) => issueOf(c) !== null).length

  const pages = Math.max(1, Math.ceil(rows.length / PAGE))
  const current = Math.min(page, pages)
  const visible = rows.slice((current - 1) * PAGE, current * PAGE)
  const cols = azure ? 8 : 6

  const lastUsed = (c: Credential) => {
    const u = usage.data?.get(c.id)
    return u ? formatDistanceToNowStrict(new Date(u.last_used_at), { addSuffix: true }) : '—'
  }
  const edit = (c: Credential) => {
    setSheet({ open: true, editing: c })
  }

  const stats = [
    ['Credentials', String(rows.length), ''],
    ['Enabled', `${String(rows.filter((c) => isLive(c)).length)} of ${String(rows.length)}`, ''],
    [
      `Expiring in ${String(EXPIRY_WARNING_DAYS)} days`,
      String(expiring),
      expiring > 0 ? 'text-warning' : '',
    ],
    ['Models', row ? String(row.model_count) : '—', ''],
  ] as const

  return (
    <>
      <PageHeader
        title={label}
        icon={
          <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border bg-muted">
            <ProviderLogo provider={provider} className="size-5" />
          </span>
        }
        actions={
          supported && (
            <>
              <Button
                variant="outline"
                onClick={() => {
                  setSettingsOpen(true)
                }}
              >
                <Settings className="text-muted-foreground" />
                Settings
              </Button>
              <Button
                onClick={() => {
                  setSheet({ open: true, editing: null })
                }}
              >
                <Plus />
                Add credential
              </Button>
            </>
          )
        }
      />
      <PageBody>
        <Link
          to="/models/api-keys"
          className="flex w-fit items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeft className="size-3.5" />
          Model Providers
        </Link>

        {!supported ? (
          <p className="rounded-xl border bg-card p-6 text-sm text-muted-foreground">
            This build has no adapter for {label}, so it cannot hold credentials yet.
          </p>
        ) : (
          <>
            <section className="grid grid-cols-2 gap-y-4 rounded-xl border bg-card px-5 py-3.5 lg:flex lg:items-center">
              {stats.map(([label, value, tone], i) => (
                <Fragment key={label}>
                  {i > 0 && (
                    <span
                      className="hidden h-[30px] w-px shrink-0 bg-border lg:block"
                      aria-hidden
                    />
                  )}
                  <div className={cn('flex min-w-0 flex-1 flex-col gap-1.5', i > 0 && 'lg:pl-4')}>
                    <span className="text-[10px] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
                      {label}
                    </span>
                    {creds.isPending ? (
                      <Skeleton className="h-5 w-16" />
                    ) : (
                      <span
                        className={cn('font-mono text-base font-medium', tone || 'text-foreground')}
                      >
                        {value}
                      </span>
                    )}
                  </div>
                </Fragment>
              ))}
            </section>

            <div className="flex flex-col gap-3">
              <section className="overflow-hidden rounded-xl border bg-card">
                <header className="flex flex-col gap-1 px-5 pt-4 pb-3">
                  <h2 className="text-sm font-semibold">Credentials</h2>
                  <span className="text-[11px] text-muted-foreground">
                    Routing rotates through enabled credentials. Keys and secrets are write-only and
                    never shown again.
                  </span>
                </header>
                <Table className="text-[13px] leading-[18px]">
                  <TableHeader className="bg-background">
                    <TableRow className="hover:bg-transparent">
                      <TableHead className={`${head} pl-5`}>Name</TableHead>
                      {azure && <TableHead className={`${head} w-36`}>Auth</TableHead>}
                      {azure && <TableHead className={`${head} w-28`}>Deployments</TableHead>}
                      <TableHead className={head}>Models</TableHead>
                      <TableHead className={`${head} w-44`}>Expires</TableHead>
                      <TableHead className={`${head} w-32`}>Last used</TableHead>
                      <TableHead className={`${head} w-24`}>Enabled</TableHead>
                      <TableHead className={`${head} w-28 pr-5 text-right`} aria-label="Actions" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {creds.isPending ? (
                      Array.from({ length: 2 }, (_, i) => (
                        <TableRow key={i} className="h-12 hover:bg-transparent">
                          <TableCell colSpan={cols} className="px-5">
                            <Skeleton className="h-4 w-full" />
                          </TableCell>
                        </TableRow>
                      ))
                    ) : creds.isError ? (
                      <TableRow className="hover:bg-transparent">
                        <TableCell
                          colSpan={cols}
                          className="h-32 text-center text-sm text-destructive"
                        >
                          Could not load credentials: {creds.error.message}
                        </TableCell>
                      </TableRow>
                    ) : visible.length === 0 ? (
                      <TableRow className="hover:bg-transparent">
                        <TableCell
                          colSpan={cols}
                          className="h-32 text-center text-sm text-muted-foreground"
                        >
                          No credentials yet. Add one to start routing to {label}.
                        </TableCell>
                      </TableRow>
                    ) : (
                      visible.map((c) => {
                        const issue = issueOf(c)
                        const dueSoon = issue !== null && issue.kind !== 'disabled'
                        return (
                          <TableRow
                            key={c.id}
                            className={cn(
                              'h-12 transition-colors duration-200 hover:bg-nav-selected',
                              dueSoon && 'bg-warning/5',
                              !c.enabled && 'text-muted-foreground',
                            )}
                          >
                            <TableCell className="pl-5 font-mono">{c.name}</TableCell>
                            {azure && (
                              <TableCell className="px-1.5 text-muted-foreground">
                                {AUTH[c.settings?.azure?.auth_mode ?? ''] ?? '—'}
                              </TableCell>
                            )}
                            {azure && (
                              <TableCell className="px-1.5 font-mono text-muted-foreground">
                                {Object.keys(c.aliases ?? {}).length || '—'}
                              </TableCell>
                            )}
                            <TableCell
                              className="max-w-0 truncate px-1.5 font-mono text-muted-foreground"
                              title={modelsLabel(c)}
                            >
                              {modelsLabel(c)}
                            </TableCell>
                            <TableCell
                              className={cn(
                                'px-1.5 font-mono',
                                dueSoon ? 'text-warning' : 'text-muted-foreground',
                              )}
                            >
                              {expiryLabel(c)}
                            </TableCell>
                            <TableCell className="px-1.5 font-mono text-muted-foreground">
                              {lastUsed(c)}
                            </TableCell>
                            <TableCell className="px-1.5">
                              <EnabledSwitch c={c} />
                            </TableCell>
                            <TableCell className="px-1.5 pr-5 text-right">
                              <Button
                                variant="link"
                                size="sm"
                                className="h-auto p-0 text-xs"
                                onClick={() => {
                                  edit(c)
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
              {rows.length > 0 && (
                <Pager
                  summary={`Showing ${String((current - 1) * PAGE + 1)}–${String(Math.min(current * PAGE, rows.length))} of ${String(rows.length)} credentials on ${label}${attention ? ` · ${String(attention)} need${attention === 1 ? 's' : ''} attention` : ''}`}
                  page={current}
                  pages={pages}
                  onPage={setPage}
                />
              )}
            </div>
          </>
        )}
      </PageBody>
      {supported && (
        <>
          <CredentialSheet
            provider={provider}
            editing={sheet.editing}
            open={sheet.open}
            onOpenChange={(open) => {
              setSheet({ ...sheet, open })
            }}
          />
          <ProviderSettingsSheet
            provider={provider}
            open={settingsOpen}
            onOpenChange={setSettingsOpen}
          />
        </>
      )}
    </>
  )
}
