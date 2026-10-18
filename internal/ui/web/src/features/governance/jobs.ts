import { ArrowDownToLine, RefreshCw } from 'lucide-react'
import type { JobName } from '@/features/governance/api'
import { formatCount } from '@/lib/format'

type Detail = Record<string, number> | undefined

const amount = (d: Detail, key: string, one: string, many = `${one}s`) => {
  const n = d?.[key] ?? 0
  const f = formatCount(n)
  return `${f.value}${f.unit ?? ''} ${n === 1 ? one : many}`
}

// The two kinds of button: pull from the database, or push to it. `busy` is the label while it runs.
export const ACTIONS = {
  sync: { label: 'Sync now', busy: 'Syncing…', icon: RefreshCw },
  flush: { label: 'Flush now', busy: 'Flushing…', icon: ArrowDownToLine },
} as const

// What each job says in its toast: while running, when done (with what the run reported), and on failure.
export const JOBS: Record<
  JobName,
  {
    action: keyof typeof ACTIONS
    running: string
    done: string
    failed: string
    detail: (d: Detail) => string
  }
> = {
  virtual_keys_sync: {
    action: 'sync',
    running: 'Syncing virtual keys…',
    done: 'Virtual keys synced',
    failed: 'Key sync failed',
    detail: (d) => `${amount(d, 'virtual_keys_loaded', 'key')} loaded`,
  },
  budget_sync: {
    action: 'sync',
    running: 'Syncing budgets…',
    done: 'Budgets synced',
    failed: 'Budget sync failed',
    detail: (d) => `${amount(d, 'budgets_loaded', 'budget')} loaded`,
  },
  budget_flush_roll: {
    action: 'flush',
    running: 'Flushing budgets…',
    done: 'Budgets flushed',
    failed: 'Budget flush failed',
    detail: (d) =>
      `${amount(d, 'budgets_flushed', 'budget')} flushed · ${amount(d, 'windows_rolled', 'window')} rolled`,
  },
  usage_flush: {
    action: 'flush',
    running: 'Flushing usage ledger…',
    done: 'Usage ledger flushed',
    failed: 'Usage flush failed',
    detail: (d) => `${amount(d, 'records_written', 'row')} written`,
  },
}
