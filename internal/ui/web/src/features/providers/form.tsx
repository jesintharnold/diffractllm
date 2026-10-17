import { addHours, startOfDay } from 'date-fns'
import { Calendar as CalendarIcon, ChevronDown, Clock, Plus, Search, X } from 'lucide-react'
import { useDeferredValue, useState, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Switch } from '@/components/ui/switch'
import { useProviderModels } from '@/features/providers/api'
import {
  DAY_END,
  EXPIRY_PRESET_HOURS,
  atTime,
  expiryField,
  expiryOn,
  isHHmm,
  timeOf,
} from '@/features/providers/expiry'
import { inputLike } from '@/features/providers/form-utils'
import { cn } from '@/lib/utils'

// Form pieces shared by the credential and provider-settings drawers (design 04.3–04.5).

// Expiry as a moment: a quick "in N hours", or a day from the calendar at a time of day (default
// 23:59). Picking closes the popover; editing the time of a chosen day applies at once. Null never expires.
export function ExpiryPicker({
  value,
  onChange,
  compact,
  disabled,
}: {
  value: Date | null
  onChange: (d: Date | null) => void
  // A small inline button instead of a full-width field, for a row of read-only facts.
  compact?: boolean
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [time, setTime] = useState(DAY_END)
  const now = new Date()
  const today = startOfDay(now)
  const pick = (d: Date | null) => {
    onChange(d)
    setOpen(false)
  }
  const clock = isHHmm(time) ? time : DAY_END // a half-typed time falls back to the end of the day
  const timeInPast = value !== null && isHHmm(time) && atTime(value, time) <= now
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next) setTime(value ? timeOf(value) : DAY_END)
        setOpen(next)
      }}
    >
      <PopoverTrigger asChild>
        <button
          type="button"
          disabled={disabled}
          className={cn(
            compact
              ? 'flex items-center gap-1.5 rounded-md border px-2 py-1 font-mono text-xs transition-colors outline-none hover:border-ring/60 focus-visible:ring-3 focus-visible:ring-ring/50 data-[state=open]:border-ring disabled:opacity-50'
              : cn(inputLike, 'text-left'),
          )}
        >
          <CalendarIcon className="size-3.5 shrink-0 text-muted-foreground" />
          <span className={cn('truncate', !compact && 'flex-1', !value && 'text-muted-foreground')}>
            {value ? expiryField(value, now) : 'No expiry'}
          </span>
          <ChevronDown className="size-3.5 text-muted-foreground" />
        </button>
      </PopoverTrigger>
      <PopoverContent align={compact ? 'end' : 'start'} className="w-auto p-0">
        <div className="flex flex-col gap-1.5 border-b px-3 pt-3 pb-2.5">
          <span className="text-[11px] font-medium text-muted-foreground">Expires in</span>
          <div className="grid grid-cols-4 gap-1.5">
            {EXPIRY_PRESET_HOURS.map((h) => (
              <Button
                key={h}
                type="button"
                variant="outline"
                size="sm"
                className="h-7 text-[11px]"
                onClick={() => {
                  pick(addHours(new Date(), h))
                }}
              >
                {h}h
              </Button>
            ))}
          </div>
        </div>
        <Calendar
          mode="single"
          selected={value ?? undefined}
          defaultMonth={value ?? today}
          disabled={{ before: today }}
          onSelect={(d) => {
            if (d) pick(expiryOn(d, clock))
          }}
          className="p-3 [--cell-size:--spacing(8)]"
        />
        <label className="flex items-center justify-between px-3 pb-2">
          <span className="text-[11px] font-medium text-muted-foreground">Expires at</span>
          <span className="relative">
            <Clock className="pointer-events-none absolute top-1/2 left-2.5 size-3 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={time}
              inputMode="numeric"
              maxLength={5}
              placeholder={DAY_END}
              aria-invalid={(time.length === 5 && !isHHmm(time)) || timeInPast}
              onChange={(e) => {
                const t = e.target.value
                setTime(t)
                if (value && isHHmm(t) && atTime(value, t) > new Date()) onChange(atTime(value, t))
              }}
              className="h-7 w-24 pl-7 font-mono text-xs md:text-xs"
            />
          </span>
        </label>
        <div className="flex items-center justify-between border-t px-3 py-2">
          <button
            type="button"
            onClick={() => {
              pick(null)
            }}
            className="text-xs text-muted-foreground hover:text-foreground"
          >
            No expiry
          </button>
          <button
            type="button"
            onClick={() => {
              pick(expiryOn(today, clock))
            }}
            className="text-xs font-medium text-primary"
          >
            Today
          </button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

export function Group({
  label,
  tone,
  children,
}: {
  label: string
  tone?: 'warn' | 'bad'
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-3">
      <h3
        className={cn(
          'text-[10px] font-semibold tracking-[0.08em] uppercase',
          tone === 'warn' ? 'text-warning' : tone === 'bad' ? 'text-destructive' : 'text-primary',
        )}
      >
        {label}
      </h3>
      {children}
    </section>
  )
}

export function Field({
  label,
  required,
  optional,
  hint,
  error,
  children,
}: {
  label: string
  required?: boolean
  optional?: boolean
  hint?: string
  error?: string
  children: ReactNode
}) {
  return (
    <label className="flex min-w-0 flex-1 flex-col gap-1.5">
      <span className="text-xs text-muted-foreground">
        {label}
        {required && <span className="text-destructive"> *</span>}
        {optional && <span className="ml-1 text-[10px]">optional</span>}
      </span>
      {children}
      {error ? (
        <span className="text-[11px] text-destructive">{error}</span>
      ) : (
        hint && <span className="text-[11px] text-muted-foreground">{hint}</span>
      )}
    </label>
  )
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T
  options: readonly (readonly [T, string])[]
  onChange: (v: T) => void
  label: string
}) {
  return (
    <div className="flex gap-1 rounded-lg border bg-muted p-1" role="radiogroup" aria-label={label}>
      {options.map(([v, text]) => {
        const on = v === value
        return (
          <button
            key={v}
            type="button"
            role="radio"
            aria-checked={on}
            onClick={() => {
              onChange(v)
            }}
            className={cn(
              'flex-1 rounded-md border px-3 py-1.5 text-xs transition-colors',
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
  )
}

export function SwitchRow({
  title,
  detail,
  checked,
  onChange,
  warn,
}: {
  title: string
  detail: string
  checked: boolean
  onChange: (v: boolean) => void
  warn?: boolean
}) {
  return (
    <div
      className={cn(
        'flex items-center gap-3 rounded-lg border bg-muted px-3.5 py-3',
        warn && 'border-warning/50',
      )}
    >
      <div className="flex flex-1 flex-col gap-0.5">
        <span className="text-[13px] font-medium">{title}</span>
        <span className="text-[11px] text-muted-foreground">{detail}</span>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} aria-label={title} />
    </div>
  )
}

// Search a provider's catalog models and collect several. A name the catalog lacks can still
// be added as typed (fine-tunes, private deployments).
export function ModelChips({
  provider,
  value,
  onChange,
  placeholder,
}: {
  provider: string
  value: string[]
  onChange: (next: string[]) => void
  placeholder: string
}) {
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const [active, setActive] = useState('')
  const typed = q.trim()
  const models = useProviderModels(provider, useDeferredValue(typed), open)
  const options = (models.data ?? []).filter((m) => !value.includes(m))
  const offerTyped = typed !== '' && !options.includes(typed) && !value.includes(typed)
  const add = (m: string) => {
    if (m && !value.includes(m)) onChange([...value, m])
    setQ('')
  }

  return (
    <div className="flex flex-col gap-2.5">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button type="button" className={cn(inputLike, 'text-left text-muted-foreground')}>
            <Search className="size-3.5 shrink-0" />
            <span className="truncate">{placeholder}</span>
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-(--radix-popover-trigger-width) p-0">
          <Command
            shouldFilter={false}
            value={active}
            onValueChange={setActive}
            // Enter adds the highlighted model, or the typed text, and clears the search.
            onKeyDown={(e) => {
              if (e.key !== 'Enter') return
              e.preventDefault()
              add(options.includes(active) ? active : typed)
            }}
          >
            <CommandInput value={q} onValueChange={setQ} placeholder="Search models" />
            <CommandList>
              <CommandEmpty className="py-4 text-sm text-muted-foreground">
                {models.isFetching ? 'Searching…' : 'No model matches.'}
              </CommandEmpty>
              {offerTyped && (
                <CommandItem
                  value={`add:${typed}`}
                  onSelect={() => {
                    add(typed)
                  }}
                >
                  <Plus className="size-3.5" />
                  Add “<span className="font-mono text-xs md:text-xs">{typed}</span>”
                </CommandItem>
              )}
              {options.map((m) => (
                <CommandItem
                  key={m}
                  value={m}
                  onSelect={() => {
                    add(m)
                  }}
                  className="font-mono text-xs md:text-xs"
                >
                  {m}
                </CommandItem>
              ))}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {value.length > 0 && (
        // The extra bottom padding keeps the chips from crowding the next field's label.
        <div className="flex flex-wrap gap-2 pt-1 pb-2">
          {value.map((m) => (
            <span
              key={m}
              className="flex h-7 items-center gap-2 rounded-md border bg-muted pr-1.5 pl-2.5 font-mono text-xs duration-200 animate-in fade-in-0 zoom-in-95 md:text-xs"
            >
              {m}
              <button
                type="button"
                aria-label={`Remove ${m}`}
                onClick={() => {
                  onChange(value.filter((x) => x !== m))
                }}
                className="flex size-4 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-foreground/10 hover:text-foreground"
              >
                <X className="size-3" />
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
