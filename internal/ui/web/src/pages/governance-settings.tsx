import { Loader2, RotateCcw } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useGovernanceSettings,
  useResetGovernanceSettings,
  useSaveGovernanceSettings,
  type GovernanceSettings,
  type GovernanceSettingsView,
} from '@/features/governance/api'
import { RunJobButton } from '@/features/governance/run-job-button'
import { NumberField } from '@/features/providers/form'

// 07 · Governance settings. Save sends the whole form at once and stores the difference from
// server.yaml; it applies the next time the gateway starts, so the page says so and never touches
// the running workers. The Sync now / Flush now buttons are separate: they run a job right away.
type Key = keyof GovernanceSettings
const KEYS: Key[] = [
  'vkey_sync_interval_seconds',
  'budget_sync_interval_seconds',
  'budget_flush_interval_seconds',
  'usage_flush_interval_seconds',
  'usage_buffer_size',
]

// Mirrors settingFields in internal/governance/settings.go, which checks the same ranges again.
const BOUNDS: Record<Key, readonly [min: number, max: number]> = {
  vkey_sync_interval_seconds: [1, 3600],
  budget_sync_interval_seconds: [1, 3600],
  budget_flush_interval_seconds: [1, 3600],
  usage_flush_interval_seconds: [1, 3600],
  usage_buffer_size: [1000, 1_000_000],
}

const RESTART_NOTE = 'Restart the gateway to apply them.'
const count = (n: number) => n.toLocaleString('en-US')

type Text = Record<Key, string>
const toText = (s: GovernanceSettings) =>
  Object.fromEntries(KEYS.map((k) => [k, String(s[k])])) as Text

function problem(key: Key, text: string): string | undefined {
  const [min, max] = BOUNDS[key]
  const n = Number(text)
  return text.trim() === '' || !Number.isInteger(n) || n < min || n > max
    ? `Between ${count(min)} and ${count(max)}`
    : undefined
}

function SettingsCard({
  title,
  detail,
  actions,
  children,
}: {
  title: string
  detail: string
  actions: ReactNode
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-3.5 rounded-xl border bg-card p-[18px]">
      <header className="flex items-start gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <h2 className="text-sm font-semibold">{title}</h2>
          <p className="text-[11px] leading-snug text-muted-foreground">{detail}</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">{actions}</div>
      </header>
      {children}
    </section>
  )
}

const Row = ({ children }: { children: ReactNode }) => <div className="flex gap-3">{children}</div>

function SettingsPage({ view }: { view: GovernanceSettingsView }) {
  const save = useSaveGovernanceSettings()
  const reset = useResetGovernanceSettings()
  const [text, setText] = useState(() => toText(view.settings))
  const [confirmingReset, setConfirmingReset] = useState(false)

  const errors = Object.fromEntries(KEYS.map((k) => [k, problem(k, text[k])])) as Record<
    Key,
    string | undefined
  >
  const dirty = KEYS.some((k) => text[k] !== String(view.settings[k]))
  const valid = KEYS.every((k) => !errors[k])
  const overridden = KEYS.some((k) => view.settings[k] !== view.defaults[k])

  const field = (key: Key, label: string, unit: string) => (
    <NumberField
      label={label}
      unit={unit}
      value={text[key]}
      error={errors[key]}
      onChange={(v) => {
        setText({ ...text, [key]: v })
      }}
    />
  )

  const submit = () => {
    const next = Object.fromEntries(
      KEYS.map((k) => [k, Number(text[k])]),
    ) as unknown as GovernanceSettings
    save.mutate(next, {
      onSuccess: () => toast.success('Settings saved', { description: RESTART_NOTE }),
      onError: (err) => toast.error(`Could not save settings: ${err.message}`),
    })
  }

  const doReset = () => {
    reset.mutate(undefined, {
      onSuccess: () => toast.success('Settings reset to default', { description: RESTART_NOTE }),
      onError: (err) => toast.error(`Could not reset settings: ${err.message}`),
    })
  }

  return (
    <>
      <PageHeader
        title="Governance settings"
        actions={
          <>
            <Button
              variant="outline"
              disabled={!overridden || reset.isPending}
              onClick={() => {
                setConfirmingReset(true)
              }}
            >
              <RotateCcw className="text-muted-foreground" />
              Reset to default
            </Button>
            <Button disabled={!dirty || !valid || save.isPending} onClick={submit}>
              {save.isPending ? (
                <>
                  <Loader2 className="animate-spin" />
                  Saving…
                </>
              ) : (
                'Save settings'
              )}
            </Button>
          </>
        }
      />
      <PageBody className="lg:grid lg:grid-cols-2 lg:items-start">
        <div className="flex flex-col gap-4">
          <SettingsCard
            title="Virtual key cache"
            detail="Keys are held in a copy-on-write snapshot. A sync swaps the whole snapshot atomically."
            actions={<RunJobButton job="virtual_keys_sync" />}
          >
            <Row>{field('vkey_sync_interval_seconds', 'Sync interval', 'seconds')}</Row>
          </SettingsCard>

          <SettingsCard
            title="Budgets"
            detail="Spend is accrued in memory and flushed on a timer. A crash loses at most one interval."
            actions={
              <>
                <RunJobButton job="budget_sync" />
                <RunJobButton job="budget_flush_roll" />
              </>
            }
          >
            <Row>
              {field('budget_sync_interval_seconds', 'Sync interval', 'seconds')}
              {field('budget_flush_interval_seconds', 'Flush interval', 'seconds')}
            </Row>
            <p className="text-[11px] text-muted-foreground">
              Flush interval is also the longest a budget reset can lag: a window that has ended is
              reset on the next run.
            </p>
          </SettingsCard>
        </div>

        <SettingsCard
          title="Usage ledger"
          detail="Every settled request appends a row. The buffer is drained on shutdown before the process exits."
          actions={<RunJobButton job="usage_flush" />}
        >
          <Row>
            {field('usage_flush_interval_seconds', 'Flush interval', 'seconds')}
            {field('usage_buffer_size', 'Buffer size', 'rows')}
          </Row>
        </SettingsCard>
      </PageBody>

      <AlertDialog open={confirmingReset} onOpenChange={setConfirmingReset}>
        <AlertDialogContent>
          <div className="flex flex-col gap-1.5">
            <AlertDialogTitle>Reset governance settings?</AlertDialogTitle>
            <AlertDialogDescription>
              Your saved changes are removed and the values from server.yaml are used.{' '}
              {RESTART_NOTE}
            </AlertDialogDescription>
          </div>
          <div className="flex justify-end gap-2">
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={doReset}>Reset to default</AlertDialogAction>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

export default function GovernanceSettingsPage() {
  const settings = useGovernanceSettings()

  if (settings.data) {
    // Remount when the saved values change, so the form starts from what is stored.
    return <SettingsPage key={JSON.stringify(settings.data.settings)} view={settings.data} />
  }
  return (
    <>
      <PageHeader title="Governance settings" />
      <PageBody>
        {settings.isError ? (
          <p className="text-sm text-destructive">
            Could not load settings: {settings.error.message}
          </p>
        ) : (
          <div className="grid gap-4 lg:grid-cols-2">
            <Skeleton className="h-40 rounded-xl" />
            <Skeleton className="h-40 rounded-xl" />
          </div>
        )}
      </PageBody>
    </>
  )
}
