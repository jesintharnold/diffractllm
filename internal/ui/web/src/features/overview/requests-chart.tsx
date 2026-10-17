import { useMemo } from 'react'
import { DrillBreadcrumb, SelectionChips } from '@/components/charts/drill-controls'
import { SquareHistogram, TipRow, type Bar } from '@/components/charts/square-histogram'
import { drillFlags, useDrillDown } from '@/components/charts/use-drill-down'
import { Skeleton } from '@/components/ui/skeleton'
import { useRequestSummary, type RequestSummary } from '@/features/metrics/api'
import { formatCount } from '@/lib/format'
import { CANCELLED_COLOR, ERROR_COLOR, providerColor, sortProviders } from '@/lib/provider-colors'
import { bucketSpan, countTick } from '@/lib/series'
import { specLabel, useTimeRange, type RangeWindow } from '@/lib/time-range'
import { useTimezone } from '@/lib/timezone'
import { cn } from '@/lib/utils'

// Squares tall: 18 is six between each y tick. Budgets keep the histogram's default of 13.
const CHART_ROWS = 18

interface Row {
  t: string
  providers: Record<string, number>
  errors: number
  cancelled: number
  total: number
}

function toRows(summary: RequestSummary): { rows: Row[]; providers: string[] } {
  const seen = new Set<string>()
  const rows = summary.points.map((p) => {
    const providers: Record<string, number> = {}
    let total = p.errors + p.cancelled
    for (const { provider, success } of p.providers) {
      providers[provider] = success
      total += success
      seen.add(provider)
    }
    return { t: p.t, providers, errors: p.errors, cancelled: p.cancelled, total }
  })
  return { rows, providers: sortProviders(seen) }
}

// Requests per bar, stacked by provider with cancelled above them and errors always on top. The
// histogram, zoom and selection are the shared ones (see components/charts); zooming sets the
// page's range, so the tiles above follow the chart.
export function RequestsChart({ window }: { window: RangeWindow }) {
  const [spec, setSpec] = useTimeRange()
  const { zone } = useTimezone()
  const drill = useDrillDown({
    zone,
    baseWindow: window,
    baseLabel: specLabel(spec, zone),
    page: { spec, setSpec },
  })
  const summary = useRequestSummary(drill.window, { bucket: drill.bucket, zone })
  const data = summary.data

  const model = useMemo(() => (data ? toRows(data) : null), [data])
  const bars = useMemo<Bar[]>(
    () =>
      (model?.rows ?? []).map((r) => ({
        t: r.t,
        segments: [
          ...(model?.providers ?? []).map((p) => ({
            key: p,
            value: r.providers[p] ?? 0,
            color: providerColor(p),
          })),
          { key: 'cancelled', value: r.cancelled, color: CANCELLED_COLOR },
          { key: 'errors', value: r.errors, color: ERROR_COLOR, min: 1 },
        ],
      })),
    [model],
  )
  const rowAt = useMemo(() => new Map((model?.rows ?? []).map((r) => [r.t, r])), [model])

  // A failed load is reported (with Retry) by the page's MetricsErrorBanner, not here.
  if (summary.isError && !data) return null

  const selected = model ? drill.indexIn(model.rows) : null
  const picked = model && selected !== null ? model.rows[selected] : undefined
  const { zoomable, grouped } = drillFlags(data?.bucket_seconds)

  return (
    <section className="flex flex-col gap-3.5 rounded-xl border bg-card p-4 md:p-5">
      <h2 className="text-sm font-semibold">Requests</h2>

      <DrillBreadcrumb root={drill.rootLabel} items={drill.zoom} onGo={drill.goTo} />

      {/* The tiles above carry the totals and the tooltip names each provider, so the only row
          left is the one that lets go of a selected bar. */}
      {picked && data && (
        <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
          <span className="text-xs text-muted-foreground">
            {bucketSpan(new Date(picked.t), data.bucket_seconds, zone)}
          </span>
          <SelectionChips
            onClear={drill.clearSelection}
            onZoom={
              zoomable
                ? () => {
                    drill.zoomInto(picked, data.bucket_seconds)
                  }
                : undefined
            }
          />
        </div>
      )}

      {!model || !data ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <div
          className={cn(
            'transition-opacity duration-200',
            summary.isPlaceholderData && 'opacity-60',
          )}
        >
          <SquareHistogram
            bars={bars}
            rows={CHART_ROWS}
            bucketSeconds={data.bucket_seconds}
            selected={selected}
            zoomable={zoomable}
            grouped={grouped}
            onPick={(i) => {
              drill.click(model.rows, data.bucket_seconds, i)
            }}
            onZoom={(i) => {
              const row = model.rows[i]
              if (row) drill.zoomInto(row, data.bucket_seconds)
            }}
            zone={zone}
            label="Requests per interval"
            formatTick={countTick}
            describe={(bar) =>
              `${bucketSpan(new Date(bar.t), data.bucket_seconds, zone)}: ${formatCount(rowAt.get(bar.t)?.total ?? 0).value} requests`
            }
            tooltip={(bar) => {
              const row = rowAt.get(bar.t)
              if (!row) return null
              return (
                <>
                  {model.providers
                    .filter((p) => (row.providers[p] ?? 0) > 0)
                    .map((p) => (
                      <TipRow
                        key={p}
                        label={p}
                        value={formatCount(row.providers[p] ?? 0).value}
                        color={providerColor(p)}
                      />
                    ))}
                  <TipRow
                    label="errors"
                    value={formatCount(row.errors).value}
                    color={ERROR_COLOR}
                  />
                  <TipRow
                    label="cancelled"
                    value={formatCount(row.cancelled).value}
                    color={CANCELLED_COLOR}
                  />
                  <TipRow label="Total" value={formatCount(row.total).value} />
                </>
              )
            }}
            empty="No requests in this window"
          />
        </div>
      )}
    </section>
  )
}
