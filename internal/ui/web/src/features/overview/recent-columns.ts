import { useSyncExternalStore } from 'react'

// The Recent requests columns, in display order. Widths are preferred sizes: spare room is shared
// across the columns in proportion, so no single column opens a gap.
export const COLUMNS = [
  { key: 'time', label: 'Time', width: 'w-[140px]' },
  { key: 'requestId', label: 'Request ID', width: 'w-[260px]' },
  { key: 'type', label: 'Type', width: 'w-[128px]' },
  { key: 'provider', label: 'Provider', width: 'w-[112px]' },
  { key: 'model', label: 'Model', width: 'w-[200px]' },
  { key: 'vk', label: 'Virtual key', width: 'w-[136px]' },
  { key: 'status', label: 'Status', width: 'w-[80px]' },
  { key: 'latency', label: 'Latency', width: 'w-[84px]' },
  { key: 'tokens', label: 'Tokens', width: 'w-[130px]' },
  { key: 'cost', label: 'Cost', width: 'w-[80px]' },
] as const

export type ColumnKey = (typeof COLUMNS)[number]['key']

const ALL: ColumnKey[] = COLUMNS.map((c) => c.key)
const STORAGE_KEY = 'diffractllm.overview.recent-columns.hidden'
const LEGACY_KEY = 'diffractllm.overview.recent-columns' // the shown list, before Request ID

const listeners = new Set<() => void>()
let cached: ColumnKey[] | null = null

const shown = (hidden: unknown[]) => ALL.filter((k) => !hidden.includes(k))

// Per-viewer preference only: storage can be blocked, so every access is guarded.
function load(): ColumnKey[] {
  try {
    const hidden = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null') as unknown
    if (Array.isArray(hidden)) {
      const keep = shown(hidden)
      if (keep.length > 0) return keep
    }
    const legacy = JSON.parse(localStorage.getItem(LEGACY_KEY) ?? 'null') as unknown
    if (Array.isArray(legacy)) {
      localStorage.removeItem(LEGACY_KEY)
      const keep = shown(ALL.filter((k) => k !== 'requestId' && !legacy.includes(k)))
      save(keep, false)
      return keep
    }
  } catch {
    // unreadable or blocked: fall through to the default
  }
  return ALL
}

function snapshot(): ColumnKey[] {
  cached ??= load()
  return cached
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

function save(next: ColumnKey[], notify = true) {
  cached = next
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(ALL.filter((k) => !next.includes(k))))
  } catch {
    // not persisted; the choice still holds for this session
  }
  if (notify) for (const listener of listeners) listener()
}

// Visible columns plus the ways to change them. The last visible column cannot be switched off.
export function useVisibleColumns() {
  const visible = useSyncExternalStore(subscribe, snapshot)

  return {
    visible,
    toggle: (key: ColumnKey) => {
      // Read the live value, not the render's copy, so quick successive clicks all count.
      const current = snapshot()
      if (current.includes(key)) {
        if (current.length > 1) save(current.filter((k) => k !== key))
      } else {
        save(ALL.filter((k) => k === key || current.includes(k)))
      }
    },
    reset: () => {
      save(ALL)
    },
  }
}
