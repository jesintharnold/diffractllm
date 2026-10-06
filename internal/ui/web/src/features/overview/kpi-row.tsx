import { MetricTile } from '@/components/metric-tile'
import { useOverviewStats } from '@/features/metrics/api'
import {
  formatCompact,
  formatCount,
  formatLatency,
  formatPercent,
  formatUSD,
  type Formatted,
} from '@/lib/format'
import type { RangeWindow } from '@/lib/time-range'

// The five overview tiles for the page's window (ADR-001 §8, 01 · Overview). A failed load
// shows "—" here; the page's MetricsErrorBanner carries the message and the Retry.
export function KpiRow({ window }: { window: RangeWindow }) {
  const stats = useOverviewStats(window)
  const s = stats.data

  // p95 only counts requests that reached a provider; with none, a 0 would read as "fast".
  const latency: Formatted | undefined = s
    ? s.requests === 0
      ? { value: '—' }
      : formatLatency(s.p95_latency_ms)
    : undefined

  const tiles: { label: string; value?: Formatted }[] = [
    { label: 'REQUESTS', value: s && formatCount(s.requests) },
    { label: 'ERROR RATE', value: s && formatPercent(s.error_rate) },
    { label: 'p95 LATENCY', value: latency },
    { label: 'TOKENS', value: s && formatCompact(s.total_tokens) },
    { label: 'SPEND', value: s && formatUSD(s.spend_usd) },
  ]

  // Columns follow the width the row actually has, not the window: with the sidebar open the
  // row stays at five until the sidebar turns into a drawer, and only then wraps.
  return (
    <section aria-label="Key metrics" className="@container">
      <div className="grid grid-cols-2 gap-3 @min-[440px]:grid-cols-3 @min-[740px]:grid-cols-5">
        {tiles.map((tile) => (
          <MetricTile
            key={tile.label}
            label={tile.label}
            value={tile.value}
            loading={stats.isPending && !stats.isError}
            dimmed={stats.isPlaceholderData}
          />
        ))}
      </div>
    </section>
  )
}
