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
const STORAGE_KEY = 'diffractllm.overview.recent-columns'

const listeners = new Set<() => void>()
let cached: ColumnKey[] | null = null

// Per-viewer preference only: storage can be blocked, so every access is guarded.
function load(): ColumnKey[] {
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null') as unknown
    if (Array.isArray(saved)) {
      // New columns should appear for existing viewers too; they could not have opted out of
      // a column that did not exist when their preference was saved.
      const keep = ALL.filter((k) => k === 'requestId' || saved.includes(k))
      if (keep.length > 0) return keep
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

function save(next: ColumnKey[]) {
  cached = next
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
  } catch {
    // not persisted; the choice still holds for this session
  }
  for (const listener of listeners) listener()
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
