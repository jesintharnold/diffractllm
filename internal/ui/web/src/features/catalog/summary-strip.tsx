import { formatDistanceToNowStrict } from 'date-fns'
import { Fragment } from 'react'
import { Skeleton } from '@/components/ui/skeleton'
import { useCatalogSummary, useSpend24h } from '@/features/catalog/api'
import { useClock } from '@/lib/clock'
import { formatCount, formatUSD } from '@/lib/format'
import { cn } from '@/lib/utils'

// The strip above the Catalog tabs (design: Strip): one card, six figures, thin separators.
export function SummaryStrip() {
  const summary = useCatalogSummary()
  const spend = useSpend24h()
  useClock(30_000) // keeps "4 minutes ago" current
  const s = summary.data

  const lastSync = s?.syncing
    ? { value: 'Syncing…', tone: 'text-muted-foreground' }
    : s?.last_error
      ? { value: 'Sync failed', tone: 'text-destructive' }
      : s?.last_sync_at
        ? {
            value: formatDistanceToNowStrict(new Date(s.last_sync_at), { addSuffix: true }),
            tone: 'text-muted-foreground',
          }
        : { value: 'Never', tone: 'text-muted-foreground' }

  const stats: { label: string; value?: string; tone?: string; title?: string }[] = [
    { label: 'Providers', value: s && `${String(s.providers.length)} live` },
    { label: 'Models', value: s && formatCount(s.models).value },
    { label: 'Priced', value: s && formatCount(s.priced).value, tone: 'text-success' },
    { label: 'Unpriced', value: s && formatCount(s.unpriced).value, tone: 'text-warning' },
    {
      label: 'Spend · 24h',
      value: spend.isError ? '—' : spend.data && formatUSD(spend.data.spend_usd).value,
      tone: 'text-primary',
    },
    { label: 'Last sync', value: s && lastSync.value, tone: lastSync.tone, title: s?.last_error },
  ]

  return (
    <section
      aria-label="Catalog summary"
      className="grid grid-cols-3 gap-y-4 rounded-xl border bg-card px-5 py-3.5 lg:flex lg:items-center"
    >
      {stats.map((stat, i) => (
        <Fragment key={stat.label}>
          {i > 0 && <span className="hidden h-[30px] w-px shrink-0 bg-border lg:block" aria-hidden />}
          <div className={cn('flex min-w-0 flex-1 flex-col gap-1.5', i > 0 && 'lg:pl-4')}>
            <span className="text-[10px] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
              {stat.label}
            </span>
            {stat.value === undefined ? (
              <Skeleton className="h-5 w-16" />
            ) : (
              <span
                title={stat.title}
                className={cn('truncate font-mono text-base font-medium', stat.tone ?? 'text-foreground')}
              >
                {stat.value}
              </span>
            )}
          </div>
        </Fragment>
      ))}
    </section>
  )
}
