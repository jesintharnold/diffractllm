import { Loader2, RotateCcw } from 'lucide-react'
import { useState } from 'react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { RESTART_NOTE } from '@/features/settings/editor'

// The header buttons of a settings page: Reset to default (behind a confirmation) and Save settings.
// Reset is only offered while something is overridden; Save only while the form is changed and valid.
export function SettingsActions({
  subject,
  dirty,
  valid,
  overridden,
  saving,
  resetting,
  onSave,
  onReset,
}: {
  subject: string
  dirty: boolean
  valid: boolean
  overridden: boolean
  saving: boolean
  resetting: boolean
  onSave: () => void
  onReset: () => void
}) {
  const [confirming, setConfirming] = useState(false)

  return (
    <>
      <Button
        variant="outline"
        disabled={!overridden || resetting}
        onClick={() => {
          setConfirming(true)
        }}
      >
        <RotateCcw className="text-muted-foreground" />
        Reset to default
      </Button>
      <Button disabled={!dirty || !valid || saving} onClick={onSave}>
        {saving ? (
          <>
            <Loader2 className="animate-spin" />
            Saving…
          </>
        ) : (
          'Save settings'
        )}
      </Button>

      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <div className="flex flex-col gap-1.5">
            <AlertDialogTitle>Reset {subject} settings?</AlertDialogTitle>
            <AlertDialogDescription>
              Your saved changes are removed and the values from server.yaml are used.{' '}
              {RESTART_NOTE}
            </AlertDialogDescription>
          </div>
          <div className="flex justify-end gap-2">
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={onReset}>Reset to default</AlertDialogAction>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
