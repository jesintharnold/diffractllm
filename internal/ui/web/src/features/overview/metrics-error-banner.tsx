import { Button } from '@/components/ui/button'
import { useOverviewStats, useRecentRequests, useRequestSummary } from '@/features/metrics/api'
import type { RangeWindow } from '@/lib/time-range'

// One banner for the whole page: if the tiles, the chart or the recent requests failed to load,
// say so once and retry them all. Same query keys as the widgets, so this adds no requests.
export function MetricsErrorBanner({ window }: { window: RangeWindow }) {
  const stats = useOverviewStats(window)
  const summary = useRequestSummary(window)
  const recent = useRecentRequests()
  const failed = [stats, summary, recent].filter((q) => q.isError && !q.data)
  const first = failed[0]
  if (!first) return null

  return (
    <div
      role="alert"
      className="flex items-center gap-3 rounded-xl border border-destructive/40 px-3.5 py-2 text-sm text-destructive"
    >
      Could not load metrics: {first.error.message}
      <Button
        size="sm"
        variant="outline"
        className="ml-auto"
        onClick={() => void Promise.all(failed.map((q) => q.refetch()))}
      >
        Retry
      </Button>
    </div>
  )
}
