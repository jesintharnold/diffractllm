import { useMemo, useState } from 'react'
import { DrillBreadcrumb, SelectionChips } from '@/components/charts/drill-controls'
import { SquareHistogram, TipRow, type Bar } from '@/components/charts/square-histogram'
import { drillFlags, useDrillDown } from '@/components/charts/use-drill-down'
import { TimeRangePicker } from '@/components/time-range-picker'
import { Skeleton } from '@/components/ui/skeleton'
import { useBudgetSpend } from '@/features/budgets/api'
import { SpendBreakdown } from '@/features/budgets/breakdown'
import { formatCount, formatUSD } from '@/lib/format'
import { bucketSpan, intervalName, moneyTick } from '@/lib/series'
import { specLabel, useRangeWindow, type RangeSpec } from '@/lib/time-range'
import { useTimezone } from '@/lib/timezone'
import { cn } from '@/lib/utils'

// A budget's spend over a range the viewer picks (24 hours to start): a histogram, and what the
// spend went on. The zoom, selection and click rules are the shared drill-down (see use-drill-down):
// totals and the breakdown always describe what the chart, or the selected bar, shows.
export function SpendSection({ budgetId }: { budgetId: string }) {
  const [spec, setSpec] = useState<RangeSpec>({ kind: 'preset', preset: '24h' })
  const baseWindow = useRangeWindow(spec)
  const { zone } = useTimezone()
  const drill = useDrillDown({ zone, baseWindow, baseLabel: specLabel(spec, zone) })
  const spend = useBudgetSpend(budgetId, drill.window, { bucket: drill.bucket, zone })
  const data = spend.data

  const selected = data ? drill.indexIn(data.points) : null
  const point = data && selected !== null ? data.points[selected] : undefined
  const { zoomable, grouped } = drillFlags(data?.bucket_seconds)

  const bars = useMemo<Bar[]>(
    () =>
      (data?.points ?? []).map((p) => ({
        t: p.t,
        quiet: p.spend_usd === 0 && p.requests === 0,
        segments: [
          { key: 'spend', value: p.spend_usd, color: 'var(--bar)', active: 'var(--bar-active)' },
        ],
      })),
    [data],
  )
  const requestsAt = useMemo(() => new Map((data?.points ?? []).map((p) => [p.t, p])), [data])

  const shown = point
    ? { spend: point.spend_usd, requests: point.requests }
    : {
        spend: (data?.points ?? []).reduce((sum, p) => sum + p.spend_usd, 0),
        requests: (data?.points ?? []).reduce((sum, p) => sum + p.requests, 0),
      }
  const caption =
    point && data ? bucketSpan(new Date(point.t), data.bucket_seconds, zone) : drill.viewLabel

  return (
    <>
      <section className="flex flex-col gap-3.5 rounded-xl border bg-card p-5">
        <div className="flex flex-wrap items-center gap-2.5">
          <h2 className="text-sm font-semibold">Spend</h2>
          {data && (
            <span className="text-[11px] text-muted-foreground">
              {intervalName(data.bucket_seconds)}
            </span>
          )}
          <span className="flex-1" />
          <TimeRangePicker
            spec={spec}
            window={baseWindow}
            onChange={(next) => {
              setSpec(next)
              drill.reset()
            }}
          />
        </div>

        <DrillBreadcrumb root={drill.rootLabel} items={drill.zoom} onGo={drill.goTo} />

        <div className="flex min-h-7 flex-wrap items-center gap-x-2.5 gap-y-1">
          <span className="font-mono text-[22px] leading-7 font-medium tabular-nums">
            {formatUSD(shown.spend).value}
          </span>
          <span className="text-xs text-muted-foreground">
            {formatCount(shown.requests).value} {shown.requests === 1 ? 'request' : 'requests'} ·{' '}
            {caption}
          </span>
          {point && data && (
            <SelectionChips
              onClear={drill.clearSelection}
              onZoom={
                zoomable
                  ? () => {
                      drill.zoomInto(point, data.bucket_seconds)
                    }
                  : undefined
              }
            />
          )}
        </div>

        {spend.isError && !data ? (
          <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
            Could not load spend: {spend.error.message}
          </p>
        ) : !data ? (
          <Skeleton className="h-52 w-full" />
        ) : (
          <div
            className={cn(
              'transition-opacity duration-200',
              spend.isPlaceholderData && 'opacity-60',
            )}
          >
            <SquareHistogram
              bars={bars}
              bucketSeconds={data.bucket_seconds}
              selected={selected}
              zoomable={zoomable}
              grouped={grouped}
              onPick={(i) => {
                drill.click(data.points, data.bucket_seconds, i)
              }}
              onZoom={(i) => {
                const bar = data.points[i]
                if (bar) drill.zoomInto(bar, data.bucket_seconds)
              }}
              zone={zone}
              label="Spend per interval"
              formatTick={moneyTick}
              describe={(bar) => {
                const p = requestsAt.get(bar.t)
                return `${bucketSpan(new Date(bar.t), data.bucket_seconds, zone)}: ${formatUSD(p?.spend_usd ?? 0).value}, ${formatCount(p?.requests ?? 0).value} requests`
              }}
              tooltip={(bar) => {
                const p = requestsAt.get(bar.t)
                return (
                  <>
                    <TipRow label="Spend" value={formatUSD(p?.spend_usd ?? 0).value} />
                    <TipRow label="Requests" value={formatCount(p?.requests ?? 0).value} />
                  </>
                )
              }}
              empty="No spend in this range"
            />
          </div>
        )}
      </section>

      {(data !== undefined || !spend.isError) && (
        <SpendBreakdown
          rows={(data?.breakdown ?? []).filter((r) => !point || r.t === point.t)}
          caption={caption}
          loading={!data}
        />
      )}
    </>
  )
}
