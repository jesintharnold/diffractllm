import { useMemo } from 'react'
import { Bar, BarChart, CartesianGrid, Tooltip, XAxis, YAxis } from 'recharts'
import { ChartContainer } from '@/components/ui/chart'
import { Skeleton } from '@/components/ui/skeleton'
import { useRequestSummary, type RequestSummary } from '@/features/metrics/api'
import { formatCompact } from '@/lib/format'
import { CANCELLED_COLOR, ERROR_COLOR, providerColor, sortProviders } from '@/lib/provider-colors'
import type { RangeWindow } from '@/lib/time-range'
import { formatInZone, useTimezone } from '@/lib/timezone'
import { cn } from '@/lib/utils'

const DAY_SECONDS = 86_400

interface Row {
  index: number
  start: Date
  label: string
  providers: Record<string, number>
  errors: number
  cancelled: number
  total: number
}

// Bucket → chart rows. Axis labels follow the bucket size the server chose for the range,
// in the chosen timezone (Bifrost's formatTimestamp: "MMM d" from a day up, else "HH:mm").
function toRows(summary: RequestSummary, zone: string): { rows: Row[]; providers: string[] } {
  const pattern = summary.bucket_seconds >= DAY_SECONDS ? 'MMM d' : 'HH:mm'
  const seen = new Set<string>()
  const rows = summary.points.map((p, index) => {
    const providers: Record<string, number> = {}
    let total = p.errors + p.cancelled
    for (const { provider, success } of p.providers) {
      providers[provider] = success
      total += success
      seen.add(provider)
    }
    const start = new Date(p.t)
    return {
      index,
      start,
      label: formatInZone(start, zone, pattern),
      providers,
      errors: p.errors,
      cancelled: p.cancelled,
      total,
    }
  })
  return { rows, providers: sortProviders(seen) }
}

// "Oct 5, 13:00 – 14:00", or "Oct 5" for day buckets, shown in the chosen zone.
function bucketSpan(start: Date, bucketSeconds: number, zone: string): string {
  if (bucketSeconds >= DAY_SECONDS) {
    const last = new Date(start.getTime() + (bucketSeconds - DAY_SECONDS) * 1000)
    const first = formatInZone(start, zone, 'MMM d')
    return bucketSeconds === DAY_SECONDS ? first : `${first} – ${formatInZone(last, zone, 'MMM d')}`
  }
  const end = new Date(start.getTime() + bucketSeconds * 1000)
  return `${formatInZone(start, zone, 'MMM d, HH:mm')} – ${formatInZone(end, zone, 'HH:mm')}`
}

const count = new Intl.NumberFormat('en-US')

function SeriesRow({ color, label, value }: { color: string; label: string; value: number }) {
  return (
    <div className="flex items-center gap-2">
      <span className="size-2 shrink-0 rounded-[2px]" style={{ background: color }} aria-hidden />
      <span className="flex-1">{label}</span>
      <span className="font-mono tabular-nums">{count.format(value)}</span>
    </div>
  )
}

// Requests per bucket, stacked by provider, cancelled above them and errors always on top.
// Mirrors Bifrost's logVolumeChart: dashed horizontal grid, ~30px bars, no legend.
export default function RequestsChart({ window }: { window: RangeWindow }) {
  const summary = useRequestSummary(window)
  const { zone } = useTimezone()
  const data = summary.data

  const model = useMemo(() => (data ? toRows(data, zone) : null), [data, zone])
  const empty = model?.rows.every((r) => r.total === 0) ?? false

  // A failed load is reported (with Retry) by the page's MetricsErrorBanner, not here.
  if (summary.isError && !data) return null

  return (
    <section className="flex flex-col gap-3 rounded-xl border bg-card p-4 md:p-[18px]">
      <h2 className="text-sm font-semibold">Requests</h2>

      {!model || !data ? (
        <Skeleton className="h-56 w-full md:h-[280px]" />
      ) : (
        <div
          className={cn(
            'relative transition-opacity duration-200',
            summary.isPlaceholderData && 'opacity-60',
          )}
        >
          {/* The vendored chart paints the hover band with --muted (invisible on our card); override it. */}
          <ChartContainer
            config={{}}
            className="aspect-auto h-56 w-full md:h-[280px] [&_.recharts-rectangle.recharts-tooltip-cursor]:fill-foreground/10"
          >
            <BarChart
              data={model.rows}
              margin={{ top: 6, right: 4, left: 0, bottom: 0 }}
              barCategoryGap={1}
            >
              <CartesianGrid
                strokeDasharray="4 4"
                vertical={false}
                stroke="var(--muted-foreground)"
                strokeOpacity={0.35}
              />
              <XAxis
                dataKey="index"
                type="number"
                domain={[-0.5, model.rows.length - 0.5]}
                tick={{ fontSize: 11, dy: 5 }}
                tickLine
                axisLine={false}
                allowDecimals={false}
                interval="preserveStartEnd"
                tickFormatter={(i: number) => model.rows[Math.round(i)]?.label ?? ''}
              />
              <YAxis
                width={40}
                tick={{ fontSize: 11 }}
                tickLine={false}
                axisLine={false}
                allowDecimals={false}
                // Bifrost's logsVolumeChart: at least 5, Recharts picks the ticks. An all-zero
                // window gives Recharts no ticks to pick, so it gets a fixed 0–5.
                domain={empty ? [0, 5] : [0, (max: number) => Math.max(max, 5)]}
                allowDataOverflow={empty}
                tickFormatter={(v: number) => {
                  const f = formatCompact(v)
                  return `${f.value}${f.unit ?? ''}`
                }}
              />
              <Tooltip
                cursor
                content={({ active, payload }) => {
                  const row = (payload as readonly { payload?: Row }[] | undefined)?.[0]?.payload
                  if (!active || !row || row.total === 0) return null
                  return (
                    <div className="min-w-48 rounded-md border bg-background px-3 py-2.5 text-xs shadow-md">
                      <div className="mb-2 font-mono text-muted-foreground">
                        {bucketSpan(row.start, data.bucket_seconds, zone)}
                      </div>
                      <div className="flex flex-col gap-1.5">
                        {model.providers
                          .filter((p) => (row.providers[p] ?? 0) > 0)
                          .map((p) => (
                            <SeriesRow
                              key={p}
                              color={providerColor(p)}
                              label={p}
                              value={row.providers[p] ?? 0}
                            />
                          ))}
                        <SeriesRow color={ERROR_COLOR} label="errors" value={row.errors} />
                        <SeriesRow
                          color={CANCELLED_COLOR}
                          label="cancelled"
                          value={row.cancelled}
                        />
                      </div>
                      <div className="mt-2 flex justify-between border-t pt-2 font-semibold">
                        <span>Total</span>
                        <span className="font-mono tabular-nums">{count.format(row.total)}</span>
                      </div>
                    </div>
                  )
                }}
              />
              {model.providers.map((p) => (
                <Bar
                  key={p}
                  dataKey={(row: Row) => row.providers[p] ?? 0}
                  name={p}
                  stackId="requests"
                  fill={providerColor(p)}
                  fillOpacity={0.9}
                  maxBarSize={30}
                  isAnimationActive={false}
                />
              ))}
              <Bar
                dataKey="cancelled"
                stackId="requests"
                fill={CANCELLED_COLOR}
                fillOpacity={0.9}
                maxBarSize={30}
                isAnimationActive={false}
              />
              <Bar
                dataKey="errors"
                stackId="requests"
                fill={ERROR_COLOR}
                fillOpacity={0.9}
                radius={[2, 2, 0, 0]}
                maxBarSize={30}
                isAnimationActive={false}
              />
            </BarChart>
          </ChartContainer>
          {empty && (
            <p className="pointer-events-none absolute inset-0 flex items-center justify-center text-sm text-muted-foreground">
              No requests in this window
            </p>
          )}
        </div>
      )}
    </section>
  )
}
