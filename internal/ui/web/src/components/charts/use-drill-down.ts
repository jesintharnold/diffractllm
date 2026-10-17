import { useMemo, useState } from 'react'
import { FINER_BUCKET, GROUP_SECONDS, bucketSpan } from '@/lib/series'
import type { RangeSpec, RangeWindow } from '@/lib/time-range'

// One step of drilling in: the interval that was clicked and the bar size to show it in.
export interface Zoom {
  from: Date
  to: Date
  bucket: number // seconds
  label: string
}

// When a chart's zoom is the whole page's window (Overview), the chart hands over the page's range.
export interface PageRange {
  spec: RangeSpec
  setSpec: (next: RangeSpec) => void
}

const sameSpan = (spec: RangeSpec, z: Zoom) =>
  spec.kind === 'custom' &&
  spec.from.getTime() === z.from.getTime() &&
  spec.to.getTime() === z.to.getTime()

// Whether a bar of this size can be zoomed, and whether it is a group of days that opens on one click.
export function drillFlags(bucketSeconds: number | undefined) {
  const finer = bucketSeconds === undefined ? undefined : FINER_BUCKET[bucketSeconds]
  return {
    zoomable: finer !== undefined,
    grouped: finer !== undefined && (bucketSeconds ?? 0) >= GROUP_SECONDS,
  }
}

// The zoom stack, the selected bar and the click rules, shared by every histogram:
// a group of days opens into its days on one click; any other bar is selected first, and clicking it
// again (or double-clicking) goes in; bars that cannot get finer select and clear.
//
// Without `page` the zoom belongs to the chart and the page's own range stays put (Budgets). With it,
// zooming sets the page's range to the interval, so everything on the page follows (Overview), and a
// range changed elsewhere drops the zoom.
export function useDrillDown({
  zone,
  baseWindow,
  baseLabel,
  page,
}: {
  zone: string
  baseWindow: RangeWindow
  baseLabel: string
  page?: PageRange
}) {
  const [stack, setStack] = useState<Zoom[]>([])
  const [origin, setOrigin] = useState<{ spec: RangeSpec; label: string } | null>(null)
  const [pickedAt, setPickedAt] = useState<string | null>(null)

  const top = stack.at(-1)
  const live = page === undefined || (top !== undefined && sameSpan(page.spec, top))
  const zoom = live ? stack : []
  const current = zoom.at(-1)
  const window = useMemo(
    () => (current ? { from: current.from, to: current.to } : baseWindow),
    [current, baseWindow],
  )
  const rootLabel = page && zoom.length > 0 ? (origin?.label ?? baseLabel) : baseLabel

  const zoomInto = (bar: { t: string }, bucketSeconds: number) => {
    const finer = FINER_BUCKET[bucketSeconds]
    if (finer === undefined) return
    const from = new Date(bar.t)
    const entry: Zoom = {
      from,
      to: new Date(from.getTime() + bucketSeconds * 1000),
      bucket: finer,
      label: bucketSpan(from, bucketSeconds, zone),
    }
    if (page) {
      if (zoom.length === 0) setOrigin({ spec: page.spec, label: baseLabel })
      page.setSpec({ kind: 'custom', from: entry.from, to: entry.to })
    }
    setStack([...zoom, entry])
    setPickedAt(null)
  }

  const goTo = (depth: number) => {
    if (page) {
      const target = zoom[depth - 1]
      if (depth === 0 && origin) page.setSpec(origin.spec)
      else if (target) page.setSpec({ kind: 'custom', from: target.from, to: target.to })
    }
    setStack(zoom.slice(0, depth))
    setPickedAt(null)
  }

  const click = (bars: { t: string }[], bucketSeconds: number, index: number) => {
    const bar = bars[index]
    if (!bar) return
    const { zoomable, grouped } = drillFlags(bucketSeconds)
    if (grouped) zoomInto(bar, bucketSeconds)
    else if (pickedAt !== bar.t) setPickedAt(bar.t)
    else if (zoomable) zoomInto(bar, bucketSeconds)
    else setPickedAt(null)
  }

  return {
    window,
    bucket: current?.bucket,
    zoom,
    rootLabel,
    viewLabel: current?.label ?? rootLabel,
    // The selected bar's position in `bars`, or null when none is selected (or it left the view).
    indexIn: (bars: { t: string }[]): number | null => {
      const i = pickedAt ? bars.findIndex((b) => b.t === pickedAt) : -1
      return i >= 0 ? i : null
    },
    click,
    zoomInto,
    goTo,
    clearSelection: () => {
      setPickedAt(null)
    },
    // The range picker chose something new: forget the zoom.
    reset: () => {
      setStack([])
      setPickedAt(null)
    },
  }
}
