import { useMemo, useSyncExternalStore } from 'react'
import { useClock } from '@/lib/clock'

// The window every metrics screen reads. Held in memory, not the URL: it survives moving
// between pages (Overview → Catalog → Overview) and resets to 24h on a full reload.
export const PRESETS = [
  { key: '1h', label: 'Last 1 hr', ms: 3_600_000 },
  { key: '6h', label: 'Last 6 hrs', ms: 6 * 3_600_000 },
  { key: '24h', label: 'Last 24 hrs', ms: 24 * 3_600_000 },
  { key: '7d', label: 'Last 7 days', ms: 7 * 86_400_000 },
  { key: '30d', label: 'Last 30 days', ms: 30 * 86_400_000 },
] as const

export type PresetKey = (typeof PRESETS)[number]['key']

export type RangeSpec =
  { kind: 'preset'; preset: PresetKey } | { kind: 'custom'; from: Date; to: Date }

// A concrete window. Every query on a page takes the same one, so they always agree.
export interface RangeWindow {
  from: Date
  to: Date
}

// Backend limits (metrics engine checkRange): at least 1 hour, at most max_window (90 days).
export const MIN_RANGE_MS = 3_600_000
export const MAX_RANGE_MS = 90 * 86_400_000

// Presets slide forward once a minute; all queries refetch together on that tick.
const SLIDE_MS = 60_000

const DEFAULT_SPEC: RangeSpec = { kind: 'preset', preset: '24h' }

export function isValidSpan(from: Date, to: Date): boolean {
  const span = to.getTime() - from.getTime()
  return span >= MIN_RANGE_MS && span <= MAX_RANGE_MS
}

export function presetLabel(key: PresetKey): string {
  return PRESETS.find((p) => p.key === key)?.label ?? key
}

export function resolveRange(spec: RangeSpec, nowMs: number): RangeWindow {
  if (spec.kind === 'custom') return { from: spec.from, to: spec.to }
  const span = PRESETS.find((p) => p.key === spec.preset)?.ms ?? MIN_RANGE_MS
  return { from: new Date(nowMs - span), to: new Date(nowMs) }
}

// Query-key and request params for a window; identical windows give identical keys.
export function windowParams(window: RangeWindow) {
  return { from: window.from.toISOString(), to: window.to.toISOString() }
}

// A module-level store: one selection for the app's lifetime, outliving page unmounts.
let currentSpec: RangeSpec = DEFAULT_SPEC
const listeners = new Set<() => void>()

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

function setSpec(next: RangeSpec) {
  currentSpec = next
  for (const listener of listeners) listener()
}

export function useTimeRange(): [RangeSpec, (next: RangeSpec) => void] {
  return [useSyncExternalStore(subscribe, () => currentSpec), setSpec]
}

// The page-level window: resolve once, hand the same object to every query on the page.
export function useRangeWindow(spec: RangeSpec): RangeWindow {
  const now = useClock(SLIDE_MS)
  const from = spec.kind === 'custom' ? spec.from.getTime() : null
  const to = spec.kind === 'custom' ? spec.to.getTime() : null
  const preset = spec.kind === 'preset' ? spec.preset : null
  return useMemo(
    () =>
      preset !== null
        ? resolveRange({ kind: 'preset', preset }, now)
        : { from: new Date(from ?? now), to: new Date(to ?? now) },
    [preset, from, to, now],
  )
}
