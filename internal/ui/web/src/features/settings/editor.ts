import { toast } from 'sonner'
import { useResetSettings, useSaveSettings, type SettingsView } from '@/features/settings/api'

export const RESTART_NOTE = 'Restart the gateway to apply them.'

// What a save did, in a line: fields that wait for a restart say so; a page of live fields only
// says the running gateway already has them.
function outcome(view: SettingsView<unknown>): string {
  if (view.restart_required.length === 0) return 'Applied to the running gateway.'
  return view.applied?.length
    ? 'Some are live now. Restart the gateway to apply the rest.'
    : RESTART_NOTE
}

// Save and Reset for one settings page, each ending in a toast that says when the change applies.
export function useSettingsEditor<T>(url: string, view: SettingsView<T>) {
  const save = useSaveSettings<T>(url)
  const reset = useResetSettings<T>(url)
  return {
    // True while something differs from server.yaml, which is when Reset has anything to do.
    overridden: JSON.stringify(view.settings) !== JSON.stringify(view.defaults),
    save: (next: T) => {
      save.mutate(next, {
        onSuccess: (saved) => toast.success('Settings saved', { description: outcome(saved) }),
        onError: (err) => toast.error(`Could not save settings: ${err.message}`),
      })
    },
    reset: () => {
      reset.mutate(undefined, {
        onSuccess: (saved) =>
          toast.success('Settings reset to default', { description: outcome(saved) }),
        onError: (err) => toast.error(`Could not reset settings: ${err.message}`),
      })
    },
    saving: save.isPending,
    resetting: reset.isPending,
  }
}
