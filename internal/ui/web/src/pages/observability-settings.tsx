import { useState } from 'react'
import { API } from '@/app/endpoints'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { Field, NumberField, Segmented, SwitchRow } from '@/features/providers/form'
import { SettingsActions } from '@/features/settings/actions'
import { useSettings, type SettingsView } from '@/features/settings/api'
import { FieldRow, SettingsCard } from '@/features/settings/card'
import { useSettingsEditor } from '@/features/settings/editor'

// 09 · Observability settings. Same rules as the governance page: the whole form is saved at once,
// only what differs from server.yaml is stored, and it applies the next time the gateway starts.
const URL = API.admin.settings('observability')

type PayloadMode = 'none' | 'errors_only' | 'full'
type LogLevel = 'debug' | 'info' | 'warn' | 'error'

// GET/PUT/DELETE /v1/admin/observability/settings (internal/observability/settings.go).
interface ObservabilitySettings {
  request_log_enabled: boolean
  events_retention_days: number
  payload_retention_days: number
  payload_mode: PayloadMode
  payload_max_kb: number
  flush_interval_seconds: number
  buffer_capacity: number
  log_level: LogLevel
}

type NumKey =
  | 'events_retention_days'
  | 'payload_retention_days'
  | 'payload_max_kb'
  | 'flush_interval_seconds'
  | 'buffer_capacity'
const NUM_KEYS: NumKey[] = [
  'events_retention_days',
  'payload_retention_days',
  'payload_max_kb',
  'flush_interval_seconds',
  'buffer_capacity',
]

// Mirrors spec in internal/observability/settings.go, which checks the same ranges again.
const BOUNDS: Record<NumKey, readonly [min: number, max: number]> = {
  events_retention_days: [1, 3650],
  payload_retention_days: [1, 3650],
  payload_max_kb: [1, 10240],
  flush_interval_seconds: [1, 3600],
  buffer_capacity: [100, 1_000_000],
}

const PAYLOAD_MODES = [
  ['none', 'None'],
  ['errors_only', 'Errors only'],
  ['full', 'Full'],
] as const
const LOG_LEVELS = [
  ['debug', 'Debug'],
  ['info', 'Info'],
  ['warn', 'Warn'],
  ['error', 'Error'],
] as const

const count = (n: number) => n.toLocaleString('en-US')

interface Form {
  enabled: boolean
  numbers: Record<NumKey, string>
  mode: PayloadMode
  level: LogLevel
}

const toForm = (s: ObservabilitySettings): Form => ({
  enabled: s.request_log_enabled,
  numbers: Object.fromEntries(NUM_KEYS.map((k) => [k, String(s[k])])) as Record<NumKey, string>,
  mode: s.payload_mode,
  level: s.log_level,
})

const toSettings = (f: Form): ObservabilitySettings => ({
  request_log_enabled: f.enabled,
  ...(Object.fromEntries(NUM_KEYS.map((k) => [k, Number(f.numbers[k])])) as Record<NumKey, number>),
  payload_mode: f.mode,
  log_level: f.level,
})

function numberProblem(key: NumKey, text: string): string | undefined {
  const [min, max] = BOUNDS[key]
  const n = Number(text)
  return text.trim() === '' || !Number.isInteger(n) || n < min || n > max
    ? `Between ${count(min)} and ${count(max)}`
    : undefined
}

function SettingsPage({ view }: { view: SettingsView<ObservabilitySettings> }) {
  const editor = useSettingsEditor(URL, view)
  const [form, setForm] = useState(() => toForm(view.settings))

  const errors = Object.fromEntries(
    NUM_KEYS.map((k) => [k, numberProblem(k, form.numbers[k])]),
  ) as Record<NumKey, string | undefined>
  // A body is stored with its event, so it cannot be kept longer than the event.
  if (
    !errors.events_retention_days &&
    !errors.payload_retention_days &&
    Number(form.numbers.payload_retention_days) > Number(form.numbers.events_retention_days)
  ) {
    errors.payload_retention_days = 'Cannot exceed the events retention'
  }

  const saved = toForm(view.settings)
  const dirty =
    form.enabled !== saved.enabled ||
    form.mode !== saved.mode ||
    form.level !== saved.level ||
    NUM_KEYS.some((k) => form.numbers[k] !== saved.numbers[k])
  const valid = NUM_KEYS.every((k) => !errors[k])

  const field = (key: NumKey, label: string, unit: string) => (
    <NumberField
      label={label}
      unit={unit}
      value={form.numbers[key]}
      error={errors[key]}
      onChange={(v) => {
        setForm({ ...form, numbers: { ...form.numbers, [key]: v } })
      }}
    />
  )

  return (
    <>
      <PageHeader
        title="Observability settings"
        actions={
          <SettingsActions
            subject="observability"
            dirty={dirty}
            valid={valid}
            overridden={editor.overridden}
            saving={editor.saving}
            resetting={editor.resetting}
            onSave={() => {
              editor.save(toSettings(form))
            }}
            onReset={editor.reset}
          />
        }
      />
      <PageBody className="lg:grid lg:grid-cols-2 lg:items-start">
        <SettingsCard
          title="Request log"
          detail="One row per admitted request. This is what the Requests page reads."
        >
          <SwitchRow
            title="Enabled"
            detail="Turning this off leaves usage accounting intact but empties the Requests page"
            checked={form.enabled}
            onChange={(enabled) => {
              setForm({ ...form, enabled })
            }}
          />
          <FieldRow>
            {field('events_retention_days', 'Events retention', 'days')}
            {field('payload_retention_days', 'Payload retention', 'days')}
          </FieldRow>
          <Field label="Payload capture">
            <Segmented
              label="Payload capture"
              value={form.mode}
              options={PAYLOAD_MODES}
              onChange={(mode) => {
                setForm({ ...form, mode })
              }}
            />
          </Field>
          <p className="text-[11px] text-muted-foreground">
            Errors only keeps the bodies of failed requests. Full keeps every body, up to the size
            cap.
          </p>
          <FieldRow>
            {field('payload_max_kb', 'Max payload size', 'KB')}
            {field('flush_interval_seconds', 'Flush interval', 'seconds')}
          </FieldRow>
          <FieldRow>{field('buffer_capacity', 'Buffer capacity', 'rows')}</FieldRow>
        </SettingsCard>

        <SettingsCard title="Logging">
          <Field label="Level">
            <Segmented
              label="Log level"
              value={form.level}
              options={LOG_LEVELS}
              onChange={(level) => {
                setForm({ ...form, level })
              }}
            />
          </Field>
        </SettingsCard>
      </PageBody>
    </>
  )
}

export default function ObservabilitySettingsPage() {
  const settings = useSettings<ObservabilitySettings>(URL)

  if (settings.data) {
    // Remount when the saved values change, so the form starts from what is stored.
    return <SettingsPage key={JSON.stringify(settings.data.settings)} view={settings.data} />
  }
  return (
    <>
      <PageHeader title="Observability settings" />
      <PageBody>
        {settings.isError ? (
          <p className="text-sm text-destructive">
            Could not load settings: {settings.error.message}
          </p>
        ) : (
          <div className="grid gap-4 lg:grid-cols-2">
            <Skeleton className="h-64 rounded-xl" />
            <Skeleton className="h-24 rounded-xl" />
          </div>
        )}
      </PageBody>
    </>
  )
}
