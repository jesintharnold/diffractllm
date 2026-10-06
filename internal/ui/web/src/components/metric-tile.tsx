import { Skeleton } from '@/components/ui/skeleton'
import type { Formatted } from '@/lib/format'
import { cn } from '@/lib/utils'

// KPI tile (design: MetricTile). Label left, as written (keeps "p95"), value centered;
// tabular digits keep values steady. No value and not loading (e.g. a failed load) shows "—".
export function MetricTile({
  label,
  value,
  loading,
  dimmed,
}: {
  label: string
  value?: Formatted
  loading?: boolean
  dimmed?: boolean
}) {
  return (
    // @container: the value steps down a size when the tile is narrow (five across beside the sidebar).
    <div className="@container flex flex-col gap-2 rounded-xl border bg-card px-4 pt-3.5 pb-5">
      <span className="text-xs font-semibold tracking-wider text-muted-foreground">{label}</span>
      <div
        className={cn(
          'flex items-end justify-center gap-1 transition-opacity duration-200',
          dimmed && 'opacity-60',
        )}
      >
        {loading ? (
          <Skeleton className="my-1 h-7 w-24" />
        ) : !value ? (
          <span className="text-2xl leading-[1.1] font-semibold text-muted-foreground @min-[150px]:text-3xl">
            —
          </span>
        ) : (
          <>
            <span className="text-2xl leading-[1.1] font-semibold tabular-nums @min-[150px]:text-3xl">
              {value.value}
            </span>
            {value.unit && (
              <span className="pb-0.5 text-sm font-medium text-muted-foreground">{value.unit}</span>
            )}
          </>
        )}
      </div>
    </div>
  )
}
