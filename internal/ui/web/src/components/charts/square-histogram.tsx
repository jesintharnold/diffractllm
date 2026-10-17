import { useEffect, useState, type ReactNode } from 'react'
import { Swatch } from '@/components/swatch'
import { axisLabel, bucketSpan, niceStep } from '@/lib/series'
import { cn } from '@/lib/utils'

const DEFAULT_ROWS = 13
const SQUARE = 10
const GAP = 3
const LABEL_CHAR = 6.5 // px per character of the 10px axis font
const LABEL_PAD = 12 // air between two neighbouring labels

// One coloured part of a bar. Segments stack bottom to top in the order given. `min` keeps a small but
// important part (errors) visible as at least one square; `active` is the colour shown on hover or
// selection (otherwise the colour is brightened).
export interface Segment {
  key: string
  value: number
  color: string
  active?: string
  min?: number
}

export interface Bar {
  t: string // the bucket's start, ISO
  segments: Segment[]
  quiet?: boolean // nothing to open: defaults to a total of zero
}

const total = (bar: Bar) => bar.segments.reduce((sum, s) => sum + s.value, 0)

// Squares per segment for a bar of `cells` squares, by largest remainder so they add up exactly.
function allocate(segments: Segment[], cells: number): number[] {
  const sum = segments.reduce((s, x) => s + x.value, 0)
  const out = segments.map(() => 0)
  if (cells <= 0 || sum <= 0) return out
  const exact = segments.map((s) => (s.value / sum) * cells)
  exact.forEach((v, i) => {
    out[i] = Math.floor(v)
  })
  let left = cells - out.reduce((a, b) => a + b, 0)
  exact
    .map((v, i) => ({ i, rest: v - Math.floor(v) }))
    .sort((a, b) => b.rest - a.rest)
    .forEach(({ i }) => {
      if (left > 0 && (segments[i]?.value ?? 0) > 0) {
        out[i] = (out[i] ?? 0) + 1
        left--
      }
    })
  segments.forEach((s, i) => {
    if ((s.min ?? 0) > 0 && s.value > 0 && out[i] === 0) {
      const donor = out.indexOf(Math.max(...out))
      if ((out[donor] ?? 0) > 1) {
        out[donor] = (out[donor] ?? 0) - 1
        out[i] = 1
      }
    }
  })
  return out
}

// A label and a value inside a tooltip, optionally led by a swatch.
export function TipRow({ label, value, color }: { label: string; value: string; color?: string }) {
  return (
    <span className="flex items-center gap-2 border-t pt-2">
      {color && <Swatch color={color} />}
      <span className="flex-1 text-muted-foreground">{label}</span>
      <span className="font-mono font-medium">{value}</span>
    </span>
  )
}

// Bars as stacks of small squares, each square the colour of the segment it belongs to. Hover for the
// numbers. A click is reported to the page, which opens a group (`grouped`) or selects the bar (the
// others dim); double-click zooms in. Each bar is as many squares wide as fits its slot, so a few bars
// get wide ones and 31 still stay apart. `rows` is the chart's height in squares.
export function SquareHistogram({
  bars,
  rows = DEFAULT_ROWS,
  bucketSeconds,
  selected,
  zoomable,
  grouped,
  onPick,
  onZoom,
  zone,
  label,
  formatTick,
  describe,
  tooltip,
  empty,
}: {
  bars: Bar[]
  rows?: number
  bucketSeconds: number
  selected: number | null
  zoomable: boolean
  grouped: boolean
  onPick: (index: number) => void
  onZoom: (index: number) => void
  zone: string
  label: string
  formatTick: (value: number) => string
  describe: (bar: Bar) => string
  tooltip: (bar: Bar) => ReactNode
  empty: string
}) {
  const [hover, setHover] = useState<number | null>(null)
  const [plot, setPlot] = useState<HTMLDivElement | null>(null)
  const [width, setWidth] = useState(700)
  useEffect(() => {
    if (!plot) return
    // Fires once on observe, then on every resize.
    const watch = new ResizeObserver(() => {
      setWidth(plot.clientWidth)
    })
    watch.observe(plot)
    return () => {
      watch.disconnect()
    }
  }, [plot])
  const height = rows * SQUARE + (rows - 1) * GAP
  const max = Math.max(0, ...bars.map(total))
  const step = niceStep(max)
  const top = step * 3
  const slot = width / Math.max(bars.length, 1)
  const cells = Math.min(4, Math.max(1, Math.floor((slot * 0.8 + GAP) / (SQUARE + GAP))))
  const labels = bars.map((bar) => axisLabel(new Date(bar.t), bucketSeconds, zone))
  // A group of days is named under every bar and thinned only if the names would overlap;
  // shorter bars label about eight.
  const widest = Math.max(0, ...labels.map((l) => l.length))
  const every = grouped
    ? Math.max(1, Math.ceil((widest * LABEL_CHAR + LABEL_PAD) / slot))
    : Math.ceil(bars.length / 8)
  const hovered = hover !== null ? bars[hover] : undefined

  return (
    <div role="group" aria-label={label} className="flex flex-col gap-2">
      <div className="flex gap-2">
        <div
          className="flex w-10 shrink-0 flex-col justify-between text-right font-mono text-[10px] leading-none text-muted-foreground"
          style={{ height }}
          aria-hidden
        >
          {[3, 2, 1, 0].map((k) => (
            <span key={k}>{formatTick(step * k)}</span>
          ))}
        </div>

        <div ref={setPlot} className="relative min-w-0 flex-1" style={{ height }}>
          <div className="flex h-full">
            {bars.map((bar, i) => {
              const sum = total(bar)
              const lit = sum > 0 ? Math.max(1, Math.round((sum / top) * rows)) : 0
              const counts = allocate(bar.segments, lit)
              // Squares from the bottom up: each segment's colour repeated as many times as it got.
              const stack = bar.segments.flatMap((s, k) =>
                Array.from({ length: counts[k] ?? 0 }, () => s),
              )
              const active = hover === i || selected === i
              const dim = selected !== null && !active
              const quiet = bar.quiet ?? sum === 0
              return (
                <button
                  key={bar.t}
                  type="button"
                  aria-pressed={selected === i}
                  aria-label={describe(bar)}
                  aria-disabled={quiet}
                  onClick={() => {
                    if (!quiet) onPick(i)
                  }}
                  onDoubleClick={() => {
                    if (!quiet && zoomable) onZoom(i)
                  }}
                  onMouseEnter={() => {
                    setHover(i)
                  }}
                  onMouseLeave={() => {
                    setHover(null)
                  }}
                  onFocus={() => {
                    setHover(i)
                  }}
                  onBlur={() => {
                    setHover(null)
                  }}
                  className={cn(
                    'flex h-full min-w-0 flex-1 flex-col items-center justify-end rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/60',
                    quiet ? 'cursor-default' : grouped ? 'cursor-zoom-in' : 'cursor-pointer',
                  )}
                >
                  <span
                    className="grid"
                    style={{
                      gridTemplateColumns: `repeat(${String(cells)}, ${String(SQUARE)}px)`,
                      gap: GAP,
                    }}
                  >
                    {Array.from({ length: rows * cells }, (_, n) => {
                      const seg = stack[rows - 1 - Math.floor(n / cells)]
                      return (
                        <span
                          key={n}
                          style={{
                            width: SQUARE,
                            height: SQUARE,
                            background: seg
                              ? active && seg.active
                                ? seg.active
                                : seg.color
                              : undefined,
                          }}
                          className={cn(
                            'rounded-[2px] transition-[opacity,filter] duration-150',
                            !seg && 'bg-border',
                            seg && dim && 'opacity-25',
                            seg && active && !seg.active && 'brightness-125',
                          )}
                        />
                      )
                    })}
                  </span>
                </button>
              )
            })}
          </div>

          {max === 0 && (
            <p className="pointer-events-none absolute inset-0 flex items-center justify-center text-sm text-muted-foreground">
              {empty}
            </p>
          )}

          {hovered && hover !== null && !(hovered.quiet ?? total(hovered) === 0) && (
            <div
              role="tooltip"
              className="pointer-events-none absolute top-2 z-10 flex w-52 flex-col gap-2 rounded-md border bg-muted px-3 py-2.5 text-xs shadow-md"
              style={
                (hover + 0.5) / bars.length > 0.5
                  ? { right: `calc(${String((1 - hover / bars.length) * 100)}% + 8px)` }
                  : { left: `calc(${String(((hover + 1) / bars.length) * 100)}% + 8px)` }
              }
            >
              <span className="font-medium text-muted-foreground">
                {bucketSpan(new Date(hovered.t), bucketSeconds, zone)}
              </span>
              {tooltip(hovered)}
              <span className="text-[10px] leading-snug text-muted-foreground">
                {grouped
                  ? 'Click to open its days'
                  : selected !== hover
                    ? 'Click to select'
                    : zoomable
                      ? 'Click again to zoom in'
                      : 'Click to clear'}
              </span>
            </div>
          )}
        </div>
      </div>

      <div className="flex pl-12" aria-hidden>
        {bars.map((bar, i) => (
          <span
            key={bar.t}
            className="flex min-w-0 flex-1 justify-center font-mono text-[10px] whitespace-nowrap text-muted-foreground"
          >
            {i % every === 0 ? labels[i] : ''}
          </span>
        ))}
      </div>
    </div>
  )
}
