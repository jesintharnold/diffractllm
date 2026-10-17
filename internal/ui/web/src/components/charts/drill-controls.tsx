import { ChevronRight, X, ZoomIn } from 'lucide-react'
import { Fragment } from 'react'
import { cn } from '@/lib/utils'

const crumb =
  'rounded-md px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-nav-selected hover:text-foreground'

// "Last 30 days › Oct 4 – Oct 6 › Oct 5": where the chart is zoomed to, each step a way back.
// `onGo(0)` returns to the range the viewer picked; `onGo(n)` to the n-th zoom.
export function DrillBreadcrumb({
  root,
  items,
  onGo,
}: {
  root: string
  items: { label: string }[]
  onGo: (depth: number) => void
}) {
  if (items.length === 0) return null
  return (
    <nav aria-label="Zoom level" className="-mt-1 flex flex-wrap items-center gap-0.5">
      <button
        type="button"
        onClick={() => {
          onGo(0)
        }}
        className={crumb}
      >
        {root}
      </button>
      {items.map((z, i) => (
        <Fragment key={`${String(i)}:${z.label}`}>
          <ChevronRight className="size-3 text-muted-foreground/60" aria-hidden />
          {i === items.length - 1 ? (
            <span aria-current="page" className="px-1.5 py-0.5 text-[11px] font-medium">
              {z.label}
            </span>
          ) : (
            <button
              type="button"
              onClick={() => {
                onGo(i + 1)
              }}
              className={cn(crumb)}
            >
              {z.label}
            </button>
          )}
        </Fragment>
      ))}
    </nav>
  )
}

// Shown beside the totals while a bar is selected: let go of it, or go into it when it can be zoomed.
export function SelectionChips({ onClear, onZoom }: { onClear: () => void; onZoom?: () => void }) {
  return (
    <>
      <button
        type="button"
        onClick={onClear}
        className="flex items-center gap-1.5 rounded-full border bg-muted px-2.5 py-1 text-[11px] font-medium transition-colors hover:border-ring/60"
      >
        <X className="size-3 text-muted-foreground" aria-hidden />
        Clear selection
      </button>
      {onZoom && (
        <button
          type="button"
          onClick={onZoom}
          className="flex items-center gap-1.5 rounded-full border border-primary/40 bg-primary/10 px-2.5 py-1 text-[11px] font-medium text-primary transition-colors hover:border-primary"
        >
          <ZoomIn className="size-3" aria-hidden />
          Zoom in
        </button>
      )}
    </>
  )
}
