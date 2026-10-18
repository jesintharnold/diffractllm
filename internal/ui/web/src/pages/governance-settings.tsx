import { useState } from 'react'
import { API } from '@/app/endpoints'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import type { GovernanceSettings } from '@/features/governance/api'
import { RunJobButton } from '@/features/governance/run-job-button'
import { NumberField } from '@/features/providers/form'
import { SettingsActions } from '@/features/settings/actions'
import { useSettings, type SettingsView } from '@/features/settings/api'
import { SettingsCard, FieldRow } from '@/features/settings/card'
import { useSettingsEditor } from '@/features/settings/editor'

// 07 · Governance settings. Save sends the whole form at once and stores the difference from
// server.yaml; it applies the next time the gateway starts, so the page says so and never touches
// the running workers. The Sync now / Flush now buttons are separate: they run a job right away.
const URL = API.admin.settings('governance')

type Key = keyof GovernanceSettings
const KEYS: Key[] = [
  'vkey_sync_interval_seconds',
  'budget_sync_interval_seconds',
  'budget_flush_interval_seconds',
  'usage_flush_interval_seconds',
  'usage_buffer_size',
]

// Mirrors settingsSpec in internal/governance/settings.go, which checks the same ranges again.
const BOUNDS: Record<Key, readonly [min: number, max: number]> = {
  vkey_sync_interval_seconds: [1, 3600],
  budget_sync_interval_seconds: [1, 3600],
  budget_flush_interval_seconds: [1, 3600],
  usage_flush_interval_seconds: [1, 3600],
  usage_buffer_size: [1000, 1_000_000],
}

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

function SettingsPage({ view }: { view: SettingsView<GovernanceSettings> }) {
  const editor = useSettingsEditor(URL, view)
  const [text, setText] = useState(() => toText(view.settings))

  const errors = Object.fromEntries(KEYS.map((k) => [k, problem(k, text[k])])) as Record<
    Key,
    string | undefined
  >
  const dirty = KEYS.some((k) => text[k] !== String(view.settings[k]))
  const valid = KEYS.every((k) => !errors[k])

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

  return (
    <>
      <PageHeader
        title="Governance settings"
        actions={
          <SettingsActions
            subject="governance"
            dirty={dirty}
            valid={valid}
            overridden={editor.overridden}
            saving={editor.saving}
            resetting={editor.resetting}
            onSave={() => {
              editor.save(
                Object.fromEntries(
                  KEYS.map((k) => [k, Number(text[k])]),
                ) as unknown as GovernanceSettings,
              )
            }}
            onReset={editor.reset}
          />
        }
      />
      <PageBody className="lg:grid lg:grid-cols-2 lg:items-start">
        <div className="flex flex-col gap-4">
          <SettingsCard
            title="Virtual key cache"
            detail="Keys are held in a copy-on-write snapshot. A sync swaps the whole snapshot atomically."
            actions={<RunJobButton job="virtual_keys_sync" />}
          >
            <FieldRow>{field('vkey_sync_interval_seconds', 'Sync interval', 'seconds')}</FieldRow>
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
            <FieldRow>
              {field('budget_sync_interval_seconds', 'Sync interval', 'seconds')}
              {field('budget_flush_interval_seconds', 'Flush interval', 'seconds')}
            </FieldRow>
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
          <FieldRow>
            {field('usage_flush_interval_seconds', 'Flush interval', 'seconds')}
            {field('usage_buffer_size', 'Buffer size', 'rows')}
          </FieldRow>
        </SettingsCard>
      </PageBody>
    </>
  )
}

export default function GovernanceSettingsPage() {
  const settings = useSettings<GovernanceSettings>(URL)

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
