import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router'

// The window every metrics screen reads, kept in the URL: ?range=24h or ?from=…&to=… (UTC).
export const PRESETS = [
  { key: '1h', label: 'Last 1 hour', ms: 3_600_000 },
  { key: '6h', label: 'Last 6 hours', ms: 6 * 3_600_000 },
  { key: '24h', label: 'Last 24 hours', ms: 24 * 3_600_000 },
  { key: '7d', label: 'Last 7 days', ms: 7 * 86_400_000 },
  { key: '30d', label: 'Last 30 days', ms: 30 * 86_400_000 },
] as const

export type PresetKey = (typeof PRESETS)[number]['key']

export type RangeSpec =
  { kind: 'preset'; preset: PresetKey } | { kind: 'custom'; from: Date; to: Date }

// Backend limits (metrics engine checkRange): at least 1 hour, at most max_window (90 days).
export const MIN_RANGE_MS = 3_600_000
export const MAX_RANGE_MS = 90 * 86_400_000

const DEFAULT_SPEC: RangeSpec = { kind: 'preset', preset: '24h' }

export function isValidSpan(from: Date, to: Date): boolean {
  const span = to.getTime() - from.getTime()
  return span >= MIN_RANGE_MS && span <= MAX_RANGE_MS
}

function parseSpec(params: URLSearchParams): RangeSpec {
  const preset = PRESETS.find((p) => p.key === params.get('range'))
  if (preset) return { kind: 'preset', preset: preset.key }

  const from = new Date(params.get('from') ?? '')
  const to = new Date(params.get('to') ?? '')
  if (!Number.isNaN(from.getTime()) && !Number.isNaN(to.getTime()) && isValidSpan(from, to)) {
    return { kind: 'custom', from, to }
  }
  return DEFAULT_SPEC
}

// resolveRange turns a spec into concrete times. Call it inside a queryFn, never in render:
// a preset resolved at fetch time slides forward on every refetch.
export function resolveRange(spec: RangeSpec): { from: Date; to: Date } {
  if (spec.kind === 'custom') return { from: spec.from, to: spec.to }
  const span = PRESETS.find((p) => p.key === spec.preset)?.ms ?? MIN_RANGE_MS
  const to = new Date()
  return { from: new Date(to.getTime() - span), to }
}

// rangeKey is stable for a spec, so it is safe inside a TanStack Query key.
export function rangeKey(spec: RangeSpec): string {
  return spec.kind === 'preset'
    ? spec.preset
    : `${spec.from.toISOString()}|${spec.to.toISOString()}`
}

export function useTimeRange(): [RangeSpec, (next: RangeSpec) => void] {
  const [params, setParams] = useSearchParams()
  const spec = useMemo(() => parseSpec(params), [params])

  const setSpec = useCallback(
    (next: RangeSpec) => {
      setParams((prev) => {
        const out = new URLSearchParams(prev)
        out.delete('range')
        out.delete('from')
        out.delete('to')
        if (next.kind === 'preset') {
          out.set('range', next.preset)
        } else {
          out.set('from', next.from.toISOString())
          out.set('to', next.to.toISOString())
        }
        return out
      })
    },
    [setParams],
  )

  return [spec, setSpec]
}
