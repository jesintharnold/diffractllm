import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { API } from '@/app/endpoints'
import { apiGet, apiSend } from '@/lib/api'
import { runAndWatch } from '@/lib/run-and-watch'

// GET /v1/admin/stats: one entry per background job. Durations are Go nanoseconds.
export interface JobStats {
  name: string
  interval: number
  running: boolean
  last_run_at?: string
  last_success_at?: string
  last_duration: number
  last_error?: string
  consecutive_failures: number
  last_detail?: Record<string, number>
}

interface GatewayStats {
  governance: JobStats[]
}

// The governance worker jobs (internal/governance/governance.go).
export type JobName = 'virtual_keys_sync' | 'budget_sync' | 'budget_flush_roll' | 'usage_flush'

const STATS_KEY = ['gateway', 'stats'] as const
const readStats = (signal?: AbortSignal) => apiGet<GatewayStats>(API.admin.stats, undefined, signal)

const WATCH_EVERY_MS = 300 // the jobs finish in milliseconds
const GIVE_UP_MS = 30_000

// Run one job now: queue it, then watch the stats until that run has finished. A run that
// recorded an error rejects, so the caller can report it.
export function useRunJob(job: JobName) {
  const queryClient = useQueryClient()
  const find = (stats: GatewayStats) => stats.governance.find((j) => j.name === job)

  return useMutation({
    mutationFn: async (): Promise<JobStats> => {
      const done = await runAndWatch({
        trigger: () => apiSend<{ status: string }>('POST', API.admin.governance.runJob(job)),
        read: () =>
          queryClient.query({
            queryKey: STATS_KEY,
            queryFn: ({ signal }) => readStats(signal),
            staleTime: 0,
          }),
        probe: (stats) => ({
          running: find(stats)?.running ?? false,
          lastRunAt: find(stats)?.last_run_at,
        }),
        everyMs: WATCH_EVERY_MS,
        giveUpMs: GIVE_UP_MS,
      })
      const stats = done && find(done)
      if (!stats) throw new Error('no result after 30 seconds')
      if (stats.last_error) throw new Error(stats.last_error)
      return stats
    },
  })
}

// GET/PUT/DELETE /v1/admin/governance/settings (internal/governance/settings.go). The values are
// whole seconds and rows. What is saved applies from the next gateway start.
export interface GovernanceSettings {
  vkey_sync_interval_seconds: number
  budget_sync_interval_seconds: number
  budget_flush_interval_seconds: number
  usage_flush_interval_seconds: number
  usage_buffer_size: number
}

export interface GovernanceSettingsView {
  settings: GovernanceSettings // saved: yaml defaults with the console's overrides on top
  defaults: GovernanceSettings // server.yaml
}

const SETTINGS_KEY = ['governance', 'settings'] as const

export function useGovernanceSettings() {
  return useQuery({
    queryKey: SETTINGS_KEY,
    queryFn: ({ signal }) =>
      apiGet<GovernanceSettingsView>(API.admin.governance.settings, undefined, signal),
  })
}

// Save sends the whole form in one request; Reset removes every override.
export function useSaveGovernanceSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (next: GovernanceSettings) =>
      apiSend<GovernanceSettingsView>('PUT', API.admin.governance.settings, next),
    onSuccess: (view) => {
      queryClient.setQueryData(SETTINGS_KEY, view)
    },
  })
}

export function useResetGovernanceSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => apiSend<GovernanceSettingsView>('DELETE', API.admin.governance.settings),
    onSuccess: (view) => {
      queryClient.setQueryData(SETTINGS_KEY, view)
    },
  })
}
