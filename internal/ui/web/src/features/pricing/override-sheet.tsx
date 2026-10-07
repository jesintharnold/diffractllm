import { ChevronRight, ChevronsUpDown, Loader2, Search, Tag, X } from 'lucide-react'
import { useDeferredValue, useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { floatingSheetOverlay, floatingSheetWide } from '@/components/floating-sheet'
import { Button } from '@/components/ui/button'
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetTitle } from '@/components/ui/sheet'
import { useCatalogSummary } from '@/features/catalog/api'
import {
  useBasePrices,
  useCreateOverride,
  useDeleteOverride,
  useModelSearch,
  useUpdateOverride,
  useVirtualKeys,
  type ModelOption,
  type Override,
  type PricingFields,
  type Scope,
} from '@/features/pricing/api'
import { describe, fieldSections, plain, pricingJSON, rateText, type FieldInfo } from '@/features/pricing/fields'
import { providerLabel } from '@/lib/provider-colors'
import { cn } from '@/lib/utils'

const MIN_SAVING_MS = 600 // a local save answers in milliseconds; keep the spinner visible

function Group({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-[10px] font-semibold tracking-[0.08em] text-primary uppercase">{label}</h3>
      {children}
    </section>
  )
}

function Field({
  label,
  title,
  required,
  children,
}: {
  label: string
  title?: string
  required?: boolean
  children: ReactNode
}) {
  return (
    <label className="flex min-w-0 flex-1 flex-col gap-1.5">
      <span title={title} className="truncate text-xs text-muted-foreground">
        {label}
        {required && <span className="text-destructive"> *</span>}
      </span>
      {children}
    </label>
  )
}

function Segmented<T extends string>({
  value,
  options,
  onChange,
  disabled,
}: {
  value: T
  options: readonly (readonly [T, string])[]
  onChange: (v: T) => void
  disabled?: boolean
}) {
  return (
    <div className="flex gap-1 rounded-lg border bg-muted p-1" role="radiogroup">
      {options.map(([v, text]) => {
        const on = v === value
        return (
          <button
            key={v}
            type="button"
            role="radio"
            aria-checked={on}
            disabled={disabled}
            onClick={() => {
              onChange(v)
            }}
            className={cn(
              'flex-1 rounded-md border px-3 py-1.5 text-xs transition-colors disabled:cursor-not-allowed',
              on
                ? 'border-primary/30 bg-primary/10 font-semibold text-primary'
                : 'border-transparent text-muted-foreground enabled:hover:text-foreground',
            )}
          >
            {text}
          </button>
        )
      })}
    </div>
  )
}

// Searchable model list: the server matches the typed text as a substring, so "gpt-5.6" shows
// only gpt-5.6 models. Only priced models are offered; an override needs a base price.
function ModelPicker({
  value,
  provider,
  disabled,
  placeholder,
  onChange,
}: {
  value: ModelOption | null
  provider: string
  disabled: boolean
  placeholder: string
  onChange: (m: ModelOption) => void
}) {
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const search = useModelSearch(useDeferredValue(q.trim()), provider, open)
  const options = search.data ?? []
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="h-9 w-full justify-start gap-2 px-3 font-mono text-xs font-normal"
        >
          <Search className="size-3.5 text-muted-foreground" />
          {value ? (
            <span className="truncate">{value.model_name}</span>
          ) : (
            <span className="truncate font-sans text-muted-foreground">{placeholder}</span>
          )}
          <ChevronsUpDown className="ml-auto size-3.5 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-(--radix-popover-trigger-width) p-0">
        <Command shouldFilter={false}>
          <CommandInput value={q} onValueChange={setQ} placeholder="Search models" className="text-xs" />
          <CommandList>
            <CommandEmpty className="py-4 text-xs text-muted-foreground">
              {search.isFetching ? 'Searching…' : 'No priced model matches.'}
            </CommandEmpty>
            {options.map((m) => (
              <CommandItem
                key={`${m.model_name}/${m.model_type}`}
                value={`${m.model_name}/${m.model_type}`}
                data-checked={value?.model_name === m.model_name && value.model_type === m.model_type}
                onSelect={() => {
                  onChange(m)
                  setOpen(false)
                }}
                className="font-mono text-xs"
              >
                <span className="min-w-0 flex-1 truncate">{m.model_name}</span>
                <span className="font-sans text-[11px] text-muted-foreground">
                  {m.model_type.replaceAll('_', ' ')}
                </span>
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

// One price input: the per-token value typed, its per-1M echo, the base price as placeholder.
function PriceInput({
  field,
  value,
  base,
  onChange,
}: {
  field: FieldInfo
  value: string
  base?: number
  onChange: (v: string) => void
}) {
  const n = Number(value)
  const valid = value === '' || (Number.isFinite(n) && n >= 0)
  return (
    <Field label={field.label} title={field.key}>
      <div className="relative">
        <Input
          inputMode="decimal"
          value={value}
          onChange={(e) => {
            onChange(e.target.value)
          }}
          placeholder={base === undefined ? 'not priced' : `base ${rateText(field.key, base)}`}
          aria-invalid={!valid}
          className={cn('h-9 font-mono text-xs', value !== '' && 'pr-28')}
        />
        {value !== '' && valid && (
          <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 font-mono text-[11px] text-primary">
            {rateText(field.key, n)}
          </span>
        )}
      </div>
    </Field>
  )
}

// A collapsible group of price fields (Text, Cache, Audio…), design 03.3.
function Section({
  title,
  summary,
  open,
  onToggle,
  children,
}: {
  title: string
  summary: string
  open: boolean
  onToggle: () => void
  children: ReactNode
}) {
  return (
    <div className="overflow-hidden rounded-lg border">
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="flex w-full items-center gap-2 bg-muted px-3 py-2.5 text-left hover:bg-nav-selected"
      >
        <ChevronRight className={cn('size-3.5 transition-transform duration-200', open && 'rotate-90')} />
        <span className="text-xs font-medium">{title}</span>
        <span className="ml-auto font-mono text-[11px] text-muted-foreground">{summary}</span>
      </button>
      {open && <div className="grid grid-cols-2 gap-3 border-t p-3">{children}</div>}
    </div>
  )
}

const toText = (p: PricingFields) =>
  Object.fromEntries(Object.entries(p).map(([k, v]) => [k, v === undefined ? '' : plain(v)]))

function OverrideForm({ editing, onDone }: { editing: Override | null; onDone: () => void }) {
  const summary = useCatalogSummary()
  const keys = useVirtualKeys()
  const create = useCreateOverride()
  const update = useUpdateOverride()
  const remove = useDeleteOverride()

  const [name, setName] = useState(editing?.name ?? '')
  const [model, setModel] = useState<ModelOption | null>(editing)
  const [scope, setScope] = useState<Scope>(editing?.scope_type ?? 'global')
  const [provider, setProvider] = useState(editing?.scope_provider ?? '')
  const [vk, setVk] = useState(editing?.scope_virtual_key_id ?? '')
  const [prices, setPrices] = useState<Record<string, string>>(toText(editing?.pricing ?? {}))
  const [mode, setMode] = useState<'fields' | 'json'>('fields')
  const [json, setJson] = useState(pricingJSON(editing?.pricing ?? {}))
  const [toggled, setToggled] = useState<Record<string, boolean>>({})
  const [busy, setBusy] = useState<'save' | 'delete' | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const locked = editing !== null // the update endpoint changes prices only

  const base = useBasePrices(model?.model_name ?? '', model?.model_type ?? '')
  const bases = (base.data ?? []).filter((b) => scope !== 'provider' || b.provider === provider)
  const baseValue = (key: string) => bases.find((b) => b.pricing?.[key] !== undefined)?.pricing?.[key]
  const sections = fieldSections(
    bases.map((b) => b.pricing ?? {}),
    Object.fromEntries(Object.entries(prices).map(([k, v]) => [k, v === '' ? undefined : Number(v)])),
  )

  const parsed = (): PricingFields | string => {
    if (mode === 'json') {
      try {
        const obj = JSON.parse(json) as unknown
        if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return 'Raw JSON must be an object'
        return obj as PricingFields
      } catch {
        return 'Raw JSON is not valid JSON'
      }
    }
    const out: PricingFields = {}
    for (const [k, v] of Object.entries(prices)) {
      if (v === '') continue
      const n = Number(v)
      if (!Number.isFinite(n) || n < 0) return `${describe(k).label} must be a number ≥ 0`
      out[k] = n
    }
    return out
  }

  const switchMode = (next: 'fields' | 'json') => {
    if (next === mode) return
    const p = parsed()
    if (next === 'json') {
      // The whole price: what the override sets, every other field at its base value.
      const set = typeof p === 'string' ? {} : p
      const full = sections
        .flatMap((s) => s.fields)
        .map((f) => [f.key, set[f.key] ?? baseValue(f.key)] as const)
        .filter(([, v]) => v !== undefined)
      setJson(pricingJSON(Object.fromEntries(full)))
    } else {
      if (typeof p === 'string') {
        toast.error(p)
        return
      }
      // Values equal to the base go back to placeholders, so only real changes read as set.
      setPrices(toText(Object.fromEntries(Object.entries(p).filter(([k, v]) => v !== baseValue(k)))))
    }
    setMode(next)
  }

  // Prices belong to one model; picking another starts the form's pricing over.
  const pickModel = (next: ModelOption | null) => {
    setModel(next)
    setPrices({})
    setMode('fields')
    setToggled({})
  }

  // A provider override can only target a model that provider prices, so the choice resets.
  const changeScope = (next: Scope) => {
    setScope(next)
    pickModel(null)
  }

  const target = scope === 'provider' ? provider : scope === 'virtualkey' ? vk : 'all'
  const ready = name.trim() !== '' && model !== null && target !== ''

  const save = async () => {
    const pricing = parsed()
    if (typeof pricing === 'string') {
      toast.error(pricing)
      return
    }
    if (!model) return
    setBusy('save')
    try {
      const request = editing
        ? update.mutateAsync({ id: editing.id, pricing })
        : create.mutateAsync({
            name: name.trim(),
            model_name: model.model_name,
            model_type: model.model_type,
            pricing,
            scope_type: scope,
            ...(scope === 'provider' ? { scope_provider: provider } : {}),
            ...(scope === 'virtualkey' ? { scope_virtual_key_id: vk } : {}),
          })
      await Promise.all([request, new Promise((r) => setTimeout(r, MIN_SAVING_MS))])
      toast.success(editing ? 'Override saved' : 'Override created')
      onDone()
    } catch (err) {
      toast.error(`Could not save the override: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      setBusy(null)
    }
  }

  const del = async () => {
    if (!editing) return
    if (!confirmDelete) {
      setConfirmDelete(true)
      return
    }
    setBusy('delete')
    try {
      await remove.mutateAsync(editing.id)
      toast.success('Override deleted')
      onDone()
    } catch (err) {
      toast.error(`Could not delete the override: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      setBusy(null)
    }
  }

  const priced = bases.map((b) => providerLabel(b.provider))
  const hint =
    model === null
      ? 'Choose a model to see the fields its base price defines.'
      : base.isPending
        ? 'Loading the base price…'
        : priced.length > 0
          ? `USD per token, image, second… as each field names. ${model.model_name} is priced on ${priced.join(' and ')}; empty fields fall back to that base price.`
          : `${model.model_name} has no base price${scope === 'provider' ? ` on ${providerLabel(provider)}` : ''}, so it cannot be overridden.`

  return (
    <>
      <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-5 py-5 [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin]">
        <Group label="Identity">
          <Field label="Override name" required>
            <Input
              value={name}
              disabled={locked}
              onChange={(e) => {
                setName(e.target.value)
              }}
              placeholder="acme gpt-4o discount"
              className="h-9"
            />
          </Field>
        </Group>

        <Group label="Scope">
          <Segmented
            value={scope}
            disabled={locked}
            onChange={changeScope}
            options={[
              ['global', 'Global'],
              ['provider', 'Provider'],
              ['virtualkey', 'Virtual key'],
            ]}
          />
          {scope === 'provider' && (
            <Field label="Provider" required>
              <Select
                value={provider}
                disabled={locked}
                onValueChange={(p) => {
                  setProvider(p)
                  pickModel(null)
                }}
              >
                <SelectTrigger className="h-9 w-full text-xs">
                  <SelectValue placeholder="Choose a provider" />
                </SelectTrigger>
                <SelectContent position="popper">
                  {(summary.data?.providers ?? []).map((p) => (
                    <SelectItem key={p} value={p}>
                      {providerLabel(p)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
          {scope === 'virtualkey' && (
            <Field label="Virtual key" required>
              <Select value={vk} disabled={locked} onValueChange={setVk}>
                <SelectTrigger className="h-9 w-full font-mono text-xs">
                  <SelectValue placeholder="Choose a virtual key" />
                </SelectTrigger>
                <SelectContent position="popper" className="max-h-72">
                  {(keys.data ?? []).map((k) => (
                    <SelectItem key={k.id} value={k.id} className="font-mono text-xs">
                      {k.display_prefix} · {k.client_id}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
        </Group>

        <Group label="Model">
          <Field label="Model" required>
            <ModelPicker
              value={model}
              provider={scope === 'provider' ? provider : ''}
              disabled={locked || (scope === 'provider' && provider === '')}
              placeholder={scope === 'provider' && provider === '' ? 'Choose a provider first' : 'Search models'}
              onChange={pickModel}
            />
          </Field>
          <p className="text-[11px] text-muted-foreground">
            {locked
              ? 'Name, model and scope are fixed once created. To change them, delete this override and add a new one.'
              : 'One override per model and scope.'}
          </p>
        </Group>

        <Group label="Pricing">
          <Segmented
            value={mode}
            onChange={switchMode}
            options={[
              ['fields', 'Fields'],
              ['json', 'Raw JSON'],
            ]}
          />
          {mode === 'json' ? (
            <textarea
              value={json}
              onChange={(e) => {
                setJson(e.target.value)
              }}
              spellCheck={false}
              aria-label="Pricing as JSON"
              className="min-h-72 rounded-lg border bg-muted p-3 font-mono text-xs outline-none focus-visible:border-ring"
            />
          ) : (
            <>
              <p
                className={cn(
                  'text-[11px]',
                  model && !base.isPending && priced.length === 0 ? 'text-warning' : 'text-muted-foreground',
                )}
              >
                {hint}
              </p>
              {model !== null &&
                sections.map(({ title, fields }) => {
                  const set = fields.filter((f) => (prices[f.key] ?? '') !== '').length
                  const open = toggled[title] ?? (title === 'Text' || set > 0)
                  return (
                    <Section
                      key={title}
                      title={title}
                      summary={`${String(fields.length)} ${fields.length === 1 ? 'field' : 'fields'}${set > 0 ? ` · ${String(set)} set` : ''}`}
                      open={open}
                      onToggle={() => {
                        setToggled({ ...toggled, [title]: !open })
                      }}
                    >
                      {fields.map((f) => (
                        <PriceInput
                          key={f.key}
                          field={f}
                          value={prices[f.key] ?? ''}
                          base={baseValue(f.key)}
                          onChange={(v) => {
                            setPrices({ ...prices, [f.key]: v })
                          }}
                        />
                      ))}
                    </Section>
                  )
                })}
            </>
          )}
        </Group>
      </div>

      <footer className="flex items-center gap-2.5 border-t bg-muted px-5 py-3.5">
        {editing && (
          <Button
            variant="destructive"
            disabled={busy !== null}
            onClick={() => void del()}
            onBlur={() => {
              setConfirmDelete(false)
            }}
          >
            {busy === 'delete' && <Loader2 className="animate-spin" />}
            {confirmDelete ? 'Confirm delete' : 'Delete override'}
          </Button>
        )}
        <span className="flex-1" />
        <SheetClose asChild>
          <Button variant="outline" disabled={busy !== null}>
            Cancel
          </Button>
        </SheetClose>
        <Button onClick={() => void save()} disabled={!ready || busy !== null} className="min-w-20">
          {busy === 'save' ? (
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

// Add or edit a pricing override (design 03.3), in the console's floating drawer.
export function OverrideSheet({
  open,
  editing,
  onOpenChange,
}: {
  open: boolean
  editing: Override | null
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent showCloseButton={false} overlayClassName={floatingSheetOverlay} className={floatingSheetWide}>
        <header className="flex items-center gap-3 border-b px-5 py-4">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-md border border-primary/30 bg-primary/10 text-primary">
            <Tag className="size-4" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <SheetTitle className="text-base font-semibold">
              {editing ? 'Edit pricing override' : 'Add pricing override'}
            </SheetTitle>
            <SheetDescription className="text-xs">
              Only the fields you set are overridden. Clearing one lets it fall back to the base price.
            </SheetDescription>
          </div>
          <SheetClose asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Close">
              <X />
            </Button>
          </SheetClose>
        </header>
        {/* Remount per opening so the form always starts from the saved values. */}
        <OverrideForm
          key={`${String(open)}-${editing?.id ?? 'new'}`}
          editing={editing}
          onDone={() => {
            onOpenChange(false)
          }}
        />
      </SheetContent>
    </Sheet>
  )
}
