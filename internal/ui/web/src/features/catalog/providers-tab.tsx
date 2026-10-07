import { useMemo, useState } from 'react'
import { Pager } from '@/components/pager'
import { ProviderLogo } from '@/components/provider-logo'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useCatalogSummary, useProviderUsage24h } from '@/features/catalog/api'
import { formatCompact, formatCount, formatUSD, type Formatted } from '@/lib/format'
import { providerLabel } from '@/lib/provider-colors'

const PAGE = 10
const text = (f: Formatted) => (f.unit ? `${f.value} ${f.unit}` : f.value)
const head = 'h-[38px] px-1.5 text-[11px] font-semibold tracking-[0.06em] text-muted-foreground uppercase'

// Configured providers only, with their model count and the last 24 hours of traffic.
export function ProvidersTab() {
  const summary = useCatalogSummary()
  const usage = useProviderUsage24h()
  const [page, setPage] = useState(1)

  const rows = useMemo(() => {
    const configured = summary.data?.providers ?? []
    const counts = summary.data?.provider_models ?? {}
    const used = new Map((usage.data?.rows ?? []).map((u) => [u.provider, u]))
    return configured
      .map((name) => ({
        name,
        models: counts[name] ?? 0,
        requests: used.get(name)?.requests ?? 0,
        tokens: used.get(name)?.tokens ?? 0,
        spend: used.get(name)?.spend_usd ?? 0,
      }))
      .sort((a, b) => b.spend - a.spend || b.requests - a.requests || a.name.localeCompare(b.name))
  }, [summary.data, usage.data])

  const pages = Math.max(1, Math.ceil(rows.length / PAGE))
  const current = Math.min(page, pages)
  const visible = rows.slice((current - 1) * PAGE, current * PAGE)
  const models = rows.reduce((n, r) => n + r.models, 0)
  const loading = summary.isPending
  const usageMissing = usage.isError // metrics engine off: show "—" rather than zeros

  return (
    <div className="flex flex-col gap-3">
      <div className="overflow-hidden rounded-xl border bg-card">
        <Table className="text-[13px] leading-[18px]">
          <TableHeader className="bg-background">
            <TableRow className="hover:bg-transparent">
              <TableHead className={`${head} pl-5`}>Provider</TableHead>
              <TableHead className={`${head} w-40`}>Models</TableHead>
              <TableHead className={`${head} w-40`}>Requests</TableHead>
              <TableHead className={`${head} w-40`}>Tokens</TableHead>
              <TableHead className={`${head} w-40 pr-5 text-right`}>Spend · 24h</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              Array.from({ length: 3 }, (_, i) => (
                <TableRow key={i} className="h-12 hover:bg-transparent">
                  <TableCell colSpan={5} className="px-5">
                    <Skeleton className="h-4 w-full" />
                  </TableCell>
                </TableRow>
              ))
            ) : visible.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={5} className="h-32 text-center text-sm text-muted-foreground">
                  No providers configured yet. Add a key under API keys.
                </TableCell>
              </TableRow>
            ) : (
              visible.map((r) => (
                <TableRow key={r.name} className="h-12 hover:bg-nav-selected">
                  <TableCell className="pl-5">
                    <span className="flex items-center gap-2.5 font-medium">
                      <ProviderLogo provider={r.name} />
                      {providerLabel(r.name)}
                    </span>
                  </TableCell>
                  <TableCell className="px-1.5 font-mono">{formatCount(r.models).value}</TableCell>
                  <TableCell className="px-1.5 font-mono">
                    {usageMissing ? '—' : formatCount(r.requests).value}
                  </TableCell>
                  <TableCell className="px-1.5 font-mono text-muted-foreground">
                    {usageMissing ? '—' : text(formatCompact(r.tokens))}
                  </TableCell>
                  <TableCell className="px-1.5 pr-5 text-right font-mono text-primary">
                    {usageMissing ? '—' : formatUSD(r.spend).value}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
      {!loading && rows.length > 0 && (
        <Pager
          summary={`Showing ${String((current - 1) * PAGE + 1)}–${String(Math.min(current * PAGE, rows.length))} of ${String(rows.length)} providers · ${formatCount(models).value} models configured`}
          page={current}
          pages={pages}
          onPage={setPage}
        />
      )}
    </div>
  )
}
