import { Loader2, Settings, X } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  useCatalogSettings,
  useSaveCatalogSettings,
  type CatalogSettings,
} from '@/features/catalog/api'
import { cn } from '@/lib/utils'

// Server limits (modelcatalog/settings.go): interval 30s–7d, timeout 1–300s.
const MIN_INTERVAL = 30
const MAX_INTERVAL = 7 * 86_400
const MAX_TIMEOUT = 300
// A local save answers in milliseconds; the spinner stays long enough to be seen.
const MIN_SAVING_MS = 600

const UNITS = { seconds: 1, minutes: 60, hours: 3600 } as const
type Unit = keyof typeof UNITS

const unitFor = (seconds: number): Unit =>
  seconds % 3600 === 0 ? 'hours' : seconds % 60 === 0 ? 'minutes' : 'seconds'

function Group({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-[10px] font-semibold tracking-[0.08em] text-primary uppercase">{label}</h3>
      {children}
    </section>
  )
}

function Field({ label, error, children }: { label: string; error?: string; children: ReactNode }) {
  return (
    <label className="flex min-w-0 flex-1 flex-col gap-1.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      {children}
      {error && <span className="text-[11px] text-destructive">{error}</span>}
    </label>
  )
}

function SettingsForm({ initial, onSaved }: { initial: CatalogSettings; onSaved: () => void }) {
  const save = useSaveCatalogSettings()
  const [form, setForm] = useState(initial)
  const [unit, setUnit] = useState<Unit>(unitFor(initial.interval_seconds))
  const [interval, setIntervalText] = useState(
    String(initial.interval_seconds / UNITS[unitFor(initial.interval_seconds)]),
  )
  const [saving, setSaving] = useState(false)

  const intervalSeconds = Math.round(Number(interval) * UNITS[unit])
  const next: CatalogSettings = { ...form, interval_seconds: intervalSeconds }
  const intervalError =
    !Number.isFinite(intervalSeconds) || intervalSeconds < MIN_INTERVAL || intervalSeconds > MAX_INTERVAL
      ? 'Between 30 seconds and 7 days'
      : undefined
  const timeoutError =
    !Number.isInteger(form.timeout_seconds) || form.timeout_seconds < 1 || form.timeout_seconds > MAX_TIMEOUT
      ? 'Between 1 and 300 seconds'
      : undefined
  const dirty = JSON.stringify(next) !== JSON.stringify(initial)
  const valid = !intervalError && !timeoutError

  const submit = async () => {
    setSaving(true)
    try {
      await Promise.all([
        save.mutateAsync(next),
        new Promise((resolve) => setTimeout(resolve, MIN_SAVING_MS)),
      ])
      toast.success('Catalog settings saved')
      onSaved()
    } catch (err) {
      toast.error(`Could not save catalog settings: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-5 py-5">
        <Group label="Catalog sync">
          <div className="flex items-center gap-3 rounded-lg border bg-muted px-3.5 py-3">
            <div className="flex flex-1 flex-col gap-0.5">
              <span className="text-[13px] font-medium">Auto sync</span>
              <span className="text-[11px] text-muted-foreground">
                Refresh the model list from every enabled provider
              </span>
            </div>
            <Switch
              checked={form.auto_sync}
              onCheckedChange={(auto_sync) => {
                setForm({ ...form, auto_sync })
              }}
              aria-label="Auto sync"
            />
          </div>
          <div className="flex gap-3">
            <Field label="Interval" error={intervalError}>
              <div className="flex gap-2">
                <Input
                  inputMode="numeric"
                  value={interval}
                  onChange={(e) => {
                    setIntervalText(e.target.value)
                  }}
                  aria-invalid={Boolean(intervalError)}
                  className="h-9 font-mono"
                />
                <Select
                  value={unit}
                  onValueChange={(u) => {
                    setUnit(u as Unit)
                  }}
                >
                  <SelectTrigger className="h-9 w-28 text-xs" aria-label="Interval unit">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent position="popper">
                    {Object.keys(UNITS).map((u) => (
                      <SelectItem key={u} value={u}>
                        {u}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </Field>
            <Field label="Timeout" error={timeoutError}>
              <div className="relative">
                <Input
                  inputMode="numeric"
                  value={Number.isNaN(form.timeout_seconds) ? '' : String(form.timeout_seconds)}
                  onChange={(e) => {
                    setForm({ ...form, timeout_seconds: Number(e.target.value) })
                  }}
                  aria-invalid={Boolean(timeoutError)}
                  className="h-9 pr-16 font-mono"
                />
                <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-[11px] text-muted-foreground">
                  seconds
                </span>
              </div>
            </Field>
          </div>
        </Group>

        <Group label="Pricing">
          <Field label="When a model has no price">
            <div className="flex gap-1 rounded-lg border bg-muted p-1" role="radiogroup">
              {(
                [
                  ['reject', 'Reject the request'],
                  ['charge_zero', 'Charge zero and warn'],
                ] as const
              ).map(([value, text]) => {
                const on = form.missing_price === value
                return (
                  <button
                    key={value}
                    type="button"
                    role="radio"
                    aria-checked={on}
                    onClick={() => {
                      setForm({ ...form, missing_price: value })
                    }}
                    className={cn(
                      'flex-1 rounded-md border px-3 py-2 text-xs transition-colors',
                      on
                        ? 'border-primary/30 bg-primary/10 font-semibold text-primary'
                        : 'border-transparent text-muted-foreground hover:text-foreground',
                    )}
                  >
                    {text}
                  </button>
                )
              })}
            </div>
          </Field>
          <div className="flex gap-3">
            <Field label="Ledger unit">
              <Input value={form.ledger_unit} disabled className="h-9 font-mono" />
            </Field>
            <Field label="Display currency">
              <Input value={form.display_currency} disabled className="h-9 font-mono" />
            </Field>
          </div>
        </Group>
      </div>

      <footer className="flex items-center justify-end gap-2.5 border-t bg-muted px-5 py-3.5">
        <SheetClose asChild>
          <Button variant="outline" disabled={saving}>
            Cancel
          </Button>
        </SheetClose>
        <Button onClick={() => void submit()} disabled={!dirty || !valid || saving} className="min-w-20">
          {saving ? (
            <>
              <Loader2 className="animate-spin" />
              Saving…
            </>
          ) : (
            'Save'
          )}
        </Button>
      </footer>
    </>
  )
}

// Floating drawer (design 02.3): 12px off every edge, a light backdrop, an eased slide.
export function CatalogSettingsSheet({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const settings = useCatalogSettings()

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        showCloseButton={false}
        overlayClassName="bg-scrim-soft supports-backdrop-filter:backdrop-blur-none"
        className="gap-0 overflow-hidden rounded-lg border bg-card data-[side=right]:inset-y-3 data-[side=right]:right-3 data-[side=right]:h-auto data-[side=right]:w-[480px] data-[side=right]:max-w-[calc(100vw-1.5rem)] data-[side=right]:border data-[side=right]:sm:max-w-[480px] data-open:duration-300 data-open:ease-[cubic-bezier(0.22,1,0.36,1)] data-[side=right]:data-open:slide-in-from-right-12"
      >
        <header className="flex items-center gap-3 border-b px-5 py-4">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-md border border-primary/30 bg-primary/10 text-primary">
            <Settings className="size-4" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <SheetTitle className="text-base font-semibold">Catalog settings</SheetTitle>
            <SheetDescription className="text-xs">
              Sync schedule and pricing behaviour for the model catalog
            </SheetDescription>
          </div>
          <SheetClose asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Close">
              <X />
            </Button>
          </SheetClose>
        </header>

        {settings.data ? (
          // Remount per opening so the form always starts from the saved values.
          <SettingsForm
            key={String(open)}
            initial={settings.data}
            onSaved={() => {
              onOpenChange(false)
            }}
          />
        ) : settings.isError ? (
          <p className="p-5 text-sm text-destructive">
            Could not load settings: {settings.error.message}
          </p>
        ) : (
          <div className="flex flex-col gap-3 p-5">
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
