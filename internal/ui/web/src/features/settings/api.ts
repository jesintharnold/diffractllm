import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiSend } from '@/lib/api'

// Every settings page answers with the same shape (internal/settings): the saved values, the
// server.yaml defaults beside them, the fields whose saved value the running gateway does not use yet
// (they wait for a restart), and, after a save, the live fields that were applied at once.
export interface SettingsView<T> {
  settings: T
  defaults: T
  restart_required: string[]
  applied?: string[]
}

const key = (url: string) => ['settings', url] as const

export function useSettings<T>(url: string) {
  return useQuery({
    queryKey: key(url),
    queryFn: ({ signal }) => apiGet<SettingsView<T>>(url, undefined, signal),
  })
}

// Save sends the whole form in one request; Reset removes every override.
export function useSaveSettings<T>(url: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (next: T) => apiSend<SettingsView<T>>('PUT', url, next),
    onSuccess: (view) => {
      queryClient.setQueryData(key(url), view)
    },
  })
}

export function useResetSettings<T>(url: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => apiSend<SettingsView<T>>('DELETE', url),
    onSuccess: (view) => {
      queryClient.setQueryData(key(url), view)
    },
  })
}
