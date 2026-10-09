import { Loader2, Plus, Trash2, X } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { floatingSheetOverlay, floatingSheetWide } from '@/components/floating-sheet'
import { ProviderLogo } from '@/components/provider-logo'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  useProviderSettings,
  useSaveProviderSettings,
  type NetworkConfig,
  type ProviderSettings,
  type ProxyConfig,
  type ProxyType,
} from '@/features/providers/api'
import { Field, Group, Segmented, SwitchRow } from '@/features/providers/form'
import { useTabIndicator } from '@/features/providers/form-utils'
import { providerLabel } from '@/lib/provider-colors'

const MIN_SAVING_MS = 600
const NS_PER_SEC = 1e9
const NS_PER_MS = 1e6
const BYTES_PER_MB = 1 << 20

// One form value per override: '' means "use the gateway default" and is left out of the PUT.
interface Form {
  requestTimeout: string // seconds
  streamIdle: string // seconds
  maxConns: string
  maxResponseMB: string
  maxRetries: string
  backoffMS: string
  retryAmbiguous: boolean
  allowPrivate: boolean
  skipTLS: boolean
  proxyType: ProxyType | 'none'
  proxyURL: string
  proxyUser: string
  proxyPass: string
  headers: { key: string; value: string }[]
}

const num = (v: number | undefined, div = 1) => (v === undefined ? '' : String(v / div))

function toForm(s: ProviderSettings): Form {
  const o = s.overrides
  const p = s.proxy_config
  return {
    requestTimeout: num(o.request_timeout, NS_PER_SEC),
    streamIdle: num(o.stream_idle_timeout, NS_PER_SEC),
    maxConns: num(o.max_conns_per_host),
    maxResponseMB: o.max_response_bytes ? num(o.max_response_bytes, BYTES_PER_MB) : '',
    maxRetries: num(o.max_retries),
    backoffMS: num(o.retry_backoff, NS_PER_MS),
    retryAmbiguous: o.retry_ambiguous_status ?? false,
    allowPrivate: o.allow_private_network ?? false,
    skipTLS: o.insecure_skip_verify ?? false,
    proxyType: p?.type ?? 'none',
    proxyURL: p?.url ?? '',
    proxyUser: p?.username ?? '',
    proxyPass: p?.password ?? '',
    headers: Object.entries(o.headers ?? {}).map(([key, value]) => ({ key, value })),
  }
}

// The PUT body, or a message saying what is wrong. Durations go back as nanoseconds.
function toBody(f: Form): { network_config: NetworkConfig; proxy_config?: ProxyConfig } | string {
  const n: NetworkConfig = {}
  const positive = (text: string, label: string, scale: number, integer = true) => {
    if (text.trim() === '') return undefined
    const v = Number(text)
    if (!Number.isFinite(v) || v <= 0 || (integer && !Number.isInteger(v)))
      return `${label} must be a positive ${integer ? 'whole number' : 'number'}`
    return Math.round(v * scale)
  }
  const fields = [
    ['request_timeout', positive(f.requestTimeout, 'Request timeout', NS_PER_SEC, false)],
    ['stream_idle_timeout', positive(f.streamIdle, 'Stream idle timeout', NS_PER_SEC, false)],
    ['max_conns_per_host', positive(f.maxConns, 'Max connections per host', 1)],
    ['max_response_bytes', positive(f.maxResponseMB, 'Max response size', BYTES_PER_MB, false)],
    ['max_retries', positive(f.maxRetries, 'Max retries', 1)],
    ['retry_backoff', positive(f.backoffMS, 'Retry backoff', NS_PER_MS)],
  ] as const
  for (const [key, v] of fields) {
    if (typeof v === 'string') return v
    if (v !== undefined) n[key] = v
  }
  if (f.retryAmbiguous) n.retry_ambiguous_status = true
  if (f.allowPrivate) n.allow_private_network = true
  if (f.skipTLS) n.insecure_skip_verify = true
  const headers = f.headers.filter((h) => h.key.trim() !== '')
  if (headers.length) n.headers = Object.fromEntries(headers.map((h) => [h.key.trim(), h.value]))

  if (f.proxyType === 'none') return { network_config: n }
  if (f.proxyType !== 'environment' && !/^(https?|socks5):\/\/\S+$/.test(f.proxyURL.trim())) {
    return 'Proxy URL must be a full URL, such as http://proxy:3128'
  }
  return {
    network_config: n,
    proxy_config: {
      type: f.proxyType,
      ...(f.proxyType === 'environment' ? {} : { url: f.proxyURL.trim() }),
      ...(f.proxyUser ? { username: f.proxyUser } : {}),
      ...(f.proxyPass ? { password: f.proxyPass } : {}),
    },
  }
}

function NumberField({
  label,
  unit,
  value,
  placeholder,
  onChange,
}: {
  label: string
  unit?: string
  value: string
  placeholder: string
  onChange: (v: string) => void
}) {
  return (
    <Field label={label}>
      <div className="relative">
        <Input
          inputMode="decimal"
          value={value}
          placeholder={placeholder}
          onChange={(e) => {
            onChange(e.target.value)
          }}
          className="h-9 pr-12 font-mono text-xs md:text-xs"
        />
        {unit && (
          <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-[11px] text-muted-foreground">
            {unit}
          </span>
        )}
      </div>
    </Field>
  )
}

function SettingsForm({
  provider,
  initial,
  onSaved,
}: {
  provider: string
  initial: ProviderSettings
  onSaved: () => void
}) {
  const save = useSaveProviderSettings()
  const [form, setForm] = useState(() => toForm(initial))
  const [saving, setSaving] = useState(false)
  const [confirmUnsafe, setConfirmUnsafe] = useState(false)
  const set = (patch: Partial<Form>) => {
    setForm({ ...form, ...patch })
    setConfirmUnsafe(false)
  }
  const eff = initial.network_config
  const body = toBody(form)
  const problem = typeof body === 'string' ? body : null
  const dirty = JSON.stringify(form) !== JSON.stringify(toForm(initial))
  // The server refuses a PUT that sets nothing at all.
  const empty =
    typeof body !== 'string' && Object.keys(body.network_config).length === 0 && !body.proxy_config
  const unsafe = form.allowPrivate || form.skipTLS

  const submit = async () => {
    if (typeof body === 'string') return
    if (unsafe && !confirmUnsafe) {
      setConfirmUnsafe(true)
      return
    }
    setSaving(true)
    try {
      await Promise.all([
        save.mutateAsync({
          provider,
          body: { ...body, ...(unsafe ? { confirm_unsafe: true } : {}) },
        }),
        new Promise((r) => setTimeout(r, MIN_SAVING_MS)),
      ])
      toast.success(`${providerLabel(provider)} settings saved`)
      onSaved()
    } catch (err) {
      toast.error(`Could not save settings: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      setSaving(false)
    }
  }

  const sec = (ns?: number) => (ns === undefined ? '' : String(ns / NS_PER_SEC))
  const [tab, setTab] = useState('network')
  const { list, box } = useTabIndicator(tab)

  return (
    <>
      <Tabs value={tab} onValueChange={setTab} className="min-h-0 flex-1 gap-0">
        <TabsList
          ref={list}
          variant="line"
          className="relative h-auto w-full justify-start gap-1 border-b p-0 px-3.5"
        >
          {['Network', 'Retries', 'Proxy', 'Headers'].map((t) => (
            <TabsTrigger
              key={t}
              value={t.toLowerCase()}
              data-value={t.toLowerCase()}
              className="h-10 flex-none px-2 text-[13px] after:hidden data-active:text-primary dark:data-active:text-primary"
            >
              {t}
            </TabsTrigger>
          ))}
          <span
            aria-hidden
            className="absolute -bottom-px h-0.5 bg-primary transition-[left,width] duration-300 ease-[cubic-bezier(0.22,1,0.36,1)] motion-reduce:transition-none"
            style={{ left: box.left, width: box.width }}
          />
        </TabsList>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5 [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin]">
          <TabsContent value="network" className="flex flex-col gap-6">
            <Group label="Timeouts">
              <div className="flex gap-3">
                <NumberField
                  label="Request timeout"
                  unit="sec"
                  value={form.requestTimeout}
                  placeholder={sec(eff.request_timeout)}
                  onChange={(requestTimeout) => {
                    set({ requestTimeout })
                  }}
                />
                <NumberField
                  label="Stream idle timeout"
                  unit="sec"
                  value={form.streamIdle}
                  placeholder={sec(eff.stream_idle_timeout)}
                  onChange={(streamIdle) => {
                    set({ streamIdle })
                  }}
                />
              </div>
              <p className="text-[11px] text-muted-foreground">
                Grey values are the gateway default; type one to override it for{' '}
                {providerLabel(provider)}.
              </p>
            </Group>
            <Group label="Connections">
              <div className="flex gap-3">
                <NumberField
                  label="Max connections per host"
                  value={form.maxConns}
                  placeholder={num(eff.max_conns_per_host)}
                  onChange={(maxConns) => {
                    set({ maxConns })
                  }}
                />
                <NumberField
                  label="Max response size"
                  unit="MB"
                  value={form.maxResponseMB}
                  placeholder={num(eff.max_response_bytes, BYTES_PER_MB)}
                  onChange={(maxResponseMB) => {
                    set({ maxResponseMB })
                  }}
                />
              </div>
            </Group>
            <Group label="Unsafe" tone="warn">
              <SwitchRow
                warn
                title="Allow private network"
                detail="Lets credentials point at private or loopback addresses, such as an on-prem gateway."
                checked={form.allowPrivate}
                onChange={(allowPrivate) => {
                  set({ allowPrivate })
                }}
              />
              <SwitchRow
                warn
                title="Skip TLS verification"
                detail="Accepts any upstream certificate. Use only for testing."
                checked={form.skipTLS}
                onChange={(skipTLS) => {
                  set({ skipTLS })
                }}
              />
              <p className="text-[11px] text-warning">
                Saving with either on asks you to confirm first.
              </p>
            </Group>
          </TabsContent>

          <TabsContent value="retries" className="flex flex-col gap-6">
            <Group label="Retries">
              <div className="flex gap-3">
                <NumberField
                  label="Max retries"
                  value={form.maxRetries}
                  placeholder={num(eff.max_retries)}
                  onChange={(maxRetries) => {
                    set({ maxRetries })
                  }}
                />
                <NumberField
                  label="Retry backoff"
                  unit="ms"
                  value={form.backoffMS}
                  placeholder={num(eff.retry_backoff, NS_PER_MS)}
                  onChange={(backoffMS) => {
                    set({ backoffMS })
                  }}
                />
              </div>
              <SwitchRow
                title="Retry ambiguous responses"
                detail="Also retry statuses where the upstream may already have done the work. Can bill twice."
                checked={form.retryAmbiguous}
                onChange={(retryAmbiguous) => {
                  set({ retryAmbiguous })
                }}
              />
            </Group>
          </TabsContent>

          <TabsContent value="proxy" className="flex flex-col gap-6">
            <Group label="Proxy">
              <Segmented
                label="Proxy type"
                value={form.proxyType}
                options={[
                  ['none', 'None'],
                  ['http', 'HTTP'],
                  ['socks5', 'SOCKS5'],
                  ['environment', 'Environment'],
                ]}
                onChange={(proxyType) => {
                  set({ proxyType })
                }}
              />
              {form.proxyType === 'environment' && (
                <p className="text-[11px] text-muted-foreground">
                  Uses HTTP_PROXY, HTTPS_PROXY and NO_PROXY from the gateway's environment.
                </p>
              )}
              {(form.proxyType === 'http' || form.proxyType === 'socks5') && (
                <>
                  <Field label="Proxy URL" required>
                    <Input
                      value={form.proxyURL}
                      placeholder={
                        form.proxyType === 'http' ? 'http://proxy:3128' : 'socks5://proxy:1080'
                      }
                      onChange={(e) => {
                        set({ proxyURL: e.target.value })
                      }}
                      className="h-9 font-mono text-xs md:text-xs"
                    />
                  </Field>
                  <div className="flex gap-3">
                    <Field label="Username" optional>
                      <Input
                        value={form.proxyUser}
                        onChange={(e) => {
                          set({ proxyUser: e.target.value })
                        }}
                        className="h-9 font-mono text-xs md:text-xs"
                      />
                    </Field>
                    <Field label="Password" optional>
                      <Input
                        type="password"
                        autoComplete="new-password"
                        value={form.proxyPass}
                        onChange={(e) => {
                          set({ proxyPass: e.target.value })
                        }}
                        className="h-9 font-mono text-xs md:text-xs"
                      />
                    </Field>
                  </div>
                </>
              )}
            </Group>
          </TabsContent>

          <TabsContent value="headers" className="flex flex-col gap-6">
            <Group label="Extra headers">
              <p className="text-[11px] text-muted-foreground">
                Sent with every upstream request to {providerLabel(provider)}.
              </p>
              {form.headers.map((h, i) => (
                <div key={i} className="flex gap-2">
                  <Input
                    value={h.key}
                    placeholder="Header"
                    aria-label="Header name"
                    onChange={(e) => {
                      set({
                        headers: form.headers.map((x, j) =>
                          j === i ? { ...x, key: e.target.value } : x,
                        ),
                      })
                    }}
                    className="h-9 w-48 font-mono text-xs md:text-xs"
                  />
                  <Input
                    value={h.value}
                    placeholder="Value"
                    aria-label="Header value"
                    onChange={(e) => {
                      set({
                        headers: form.headers.map((x, j) =>
                          j === i ? { ...x, value: e.target.value } : x,
                        ),
                      })
                    }}
                    className="h-9 flex-1 font-mono text-xs md:text-xs"
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label="Remove header"
                    onClick={() => {
                      set({ headers: form.headers.filter((_, j) => j !== i) })
                    }}
                  >
                    <Trash2 />
                  </Button>
                </div>
              ))}
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="self-start"
                onClick={() => {
                  set({ headers: [...form.headers, { key: '', value: '' }] })
                }}
              >
                <Plus />
                Add header
              </Button>
            </Group>
          </TabsContent>
        </div>
      </Tabs>

      <footer className="flex items-center gap-2.5 border-t bg-muted px-5 py-3.5">
        <span
          className={
            confirmUnsafe
              ? 'flex-1 text-[11px] text-warning'
              : 'flex-1 text-[11px] text-muted-foreground'
          }
        >
          {problem ??
            (confirmUnsafe
              ? 'Unsafe options are on. Click again to save anyway.'
              : empty && dirty
                ? 'Set at least one value; the gateway keeps one config per provider.'
                : 'Changes apply to new connections.')}
        </span>
        <SheetClose asChild>
          <Button variant="outline" disabled={saving}>
            Cancel
          </Button>
        </SheetClose>
        <Button
          onClick={() => void submit()}
          disabled={!dirty || problem !== null || empty || saving}
          className="min-w-28"
        >
          {saving ? (
            <>
              <Loader2 className="animate-spin" />
              Saving…
            </>
          ) : confirmUnsafe ? (
            'Confirm and save'
          ) : (
            'Save settings'
          )}
        </Button>
      </footer>
    </>
  )
}

// Provider-wide upstream settings in tabs (design 04.5).
export function ProviderSettingsSheet({
  provider,
  open,
  onOpenChange,
}: {
  provider: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const settings = useProviderSettings(provider, open)
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        showCloseButton={false}
        overlayClassName={floatingSheetOverlay}
        className={floatingSheetWide}
      >
        <header className="flex items-center gap-3 px-5 pt-4 pb-3">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-lg border bg-muted">
            <ProviderLogo provider={provider} className="size-6" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <SheetTitle className="text-base font-semibold">
              {providerLabel(provider)} settings
            </SheetTitle>
            <SheetDescription className="text-xs">
              Applies to every credential on this provider. Empty fields use the gateway default.
            </SheetDescription>
          </div>
          <SheetClose asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Close">
              <X />
            </Button>
          </SheetClose>
        </header>
        {settings.data ? (
          <SettingsForm
            key={String(open)}
            provider={provider}
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
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
