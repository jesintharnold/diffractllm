import { format } from 'date-fns'
import {
  ArrowRight,
  Calendar as CalendarIcon,
  Check,
  ChevronDown,
  Clock3,
  Globe,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import type { DateRange } from 'react-day-picker'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useIsMobile } from '@/hooks/use-mobile'
import {
  PRESETS,
  isValidSpan,
  presetLabel,
  type RangeSpec,
  type RangeWindow,
} from '@/lib/time-range'
import {
  formatInZone,
  listTimeZones,
  to24Hour,
  useTimezone,
  utcToZoned,
  zoneLabel,
  zonedToUtc,
  type Meridiem,
} from '@/lib/timezone'
import { cn } from '@/lib/utils'

// Header control for the page window (ADR-001 §8). Presets apply on click; "Custom" opens a
// range calendar with From/To times and applies only on Apply. The server only sees UTC.
export function TimeRangePicker({
  spec,
  window,
  onChange,
}: {
  spec: RangeSpec
  window: RangeWindow
  onChange: (next: RangeSpec) => void
}) {
  const [open, setOpen] = useState(false)
  const [custom, setCustom] = useState(false)
  const { zone } = useTimezone()

  const label =
    spec.kind === 'preset'
      ? presetLabel(spec.preset)
      : `Custom · ${formatInZone(spec.from, zone)} → ${formatInZone(spec.to, zone)}`

  const onOpenChange = (next: boolean) => {
    setOpen(next)
    if (next) setCustom(spec.kind === 'custom')
  }

  const choose = (next: RangeSpec) => {
    onChange(next)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          className="w-full justify-start gap-2 px-3 font-medium data-[state=open]:border-primary sm:w-auto"
        >
          <CalendarIcon className="text-muted-foreground" aria-hidden />
          <span className="truncate">{label}</span>
          <ChevronDown className="ml-auto text-muted-foreground" aria-hidden />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className={cn(
          // Sized by its content: the range panel animates its own width, so the popover grows with it.
          'max-h-[calc(100svh-6rem)] w-[calc(100vw-2rem)] max-w-max overflow-y-auto p-0 md:w-auto',
        )}
      >
        <div className="flex flex-col-reverse md:flex-row">
          {custom && (
            // Keyed by zone: switching zones re-reads the current window in the new zone.
            <CustomPanel
              key={zone}
              window={window}
              onApply={choose}
              onCancel={() => {
                setOpen(false)
              }}
            />
          )}
          <PresetPanel
            spec={spec}
            customOpen={custom}
            onPreset={choose}
            onCustom={() => {
              setCustom(true)
            }}
          />
        </div>
      </PopoverContent>
    </Popover>
  )
}

function PresetPanel({
  spec,
  customOpen,
  onPreset,
  onCustom,
}: {
  spec: RangeSpec
  customOpen: boolean
  onPreset: (next: RangeSpec) => void
  onCustom: () => void
}) {
  const { zone, setZone } = useTimezone()
  const zones = useMemo(() => listTimeZones(), [])

  const items = [
    ...PRESETS.map((p) => ({
      key: p.key,
      label: p.label,
      active: !customOpen && spec.kind === 'preset' && spec.preset === p.key,
      onSelect: () => {
        onPreset({ kind: 'preset', preset: p.key })
      },
    })),
    { key: 'custom', label: 'Custom range', active: customOpen, onSelect: onCustom },
  ]

  return (
    <div
      className={cn(
        'flex shrink-0 flex-col gap-2.5 px-3 py-4 md:w-60',
        customOpen && 'border-b md:border-b-0 md:border-l',
      )}
    >
      <span className="text-xs font-medium text-muted-foreground">Timezone</span>
      <Select value={zone} onValueChange={setZone}>
        <SelectTrigger className="w-full">
          <SelectValue>
            <span className="flex min-w-0 items-center gap-2">
              <Globe className="shrink-0 text-muted-foreground" aria-hidden />
              <span className="truncate">{zoneLabel(zone)}</span>
            </span>
          </SelectValue>
        </SelectTrigger>
        <SelectContent className="max-h-72">
          {zones.map((z) => (
            <SelectItem key={z} value={z}>
              {z}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <div className="flex flex-col gap-0.5">
        {items.map((item) => (
          <button
            key={item.key}
            type="button"
            aria-pressed={item.active}
            onClick={item.onSelect}
            className={cn(
              'flex items-center rounded-md px-2.5 py-2 text-left text-sm transition-colors hover:bg-accent',
              item.active
                ? 'bg-nav-selected font-semibold text-foreground'
                : 'text-muted-foreground',
            )}
          >
            {item.label}
            {item.active && <Check className="ml-auto size-4 text-primary" aria-hidden />}
          </button>
        ))}
      </div>
    </div>
  )
}

interface TimeDraft {
  time: string
  meridiem: Meridiem
}

function CustomPanel({
  window,
  onApply,
  onCancel,
}: {
  window: RangeWindow
  onApply: (next: RangeSpec) => void
  onCancel: () => void
}) {
  const { zone } = useTimezone()
  const isMobile = useIsMobile()

  // Starts from the current window, shown in the chosen zone.
  const [days, setDays] = useState<DateRange | undefined>(() => ({
    from: utcToZoned(window.from, zone).day,
    to: utcToZoned(window.to, zone).day,
  }))
  const [fromTime, setFromTime] = useState<TimeDraft>(() => utcToZoned(window.from, zone))
  const [toTime, setToTime] = useState<TimeDraft>(() => utcToZoned(window.to, zone))

  const from24 = to24Hour(fromTime.time, fromTime.meridiem)
  const to24 = to24Hour(toTime.time, toTime.meridiem)
  const from = days?.from && from24 ? zonedToUtc(days.from, from24, zone) : null
  const to = days?.to && to24 ? zonedToUtc(days.to, to24, zone) : null
  const valid = from !== null && to !== null && isValidSpan(from, to)

  const summary = (day: Date | undefined, t: TimeDraft) =>
    day ? `${format(day, 'MMM d, yyyy')} · ${t.time} ${t.meridiem}` : 'Pick a day'

  // Enters from width 0 / opacity 0 (@starting-style) and eases open, revealing a fixed-width
  // inner column, so the popover widens calmly instead of jumping. Reduced motion skips it.
  return (
    <div className="overflow-hidden opacity-100 transition-[width,opacity] duration-500 ease-[cubic-bezier(0.22,1,0.36,1)] motion-reduce:transition-none starting:opacity-0 md:w-[548px] md:starting:w-0">
      <div className="flex w-full flex-col gap-3.5 p-4 md:w-[548px]">
        <span className="text-sm font-semibold">Custom range</span>

        <div className="flex flex-wrap items-center gap-2.5 rounded-md bg-muted px-3 py-2 font-mono text-xs">
          <span>{summary(days?.from, fromTime)}</span>
          <ArrowRight className="size-3.5 text-muted-foreground" aria-hidden />
          <span>{summary(days?.to, toTime)}</span>
        </div>

        <Calendar
          mode="range"
          numberOfMonths={isMobile ? 1 : 2}
          showOutsideDays={false}
          selected={days}
          onSelect={setDays}
          defaultMonth={days?.from}
          className="range-calendar w-full p-0"
        />

        <div className="grid gap-4 sm:grid-cols-2">
          <TimeField label="From time" value={fromTime} onChange={setFromTime} />
          <TimeField label="To time" value={toTime} onChange={setToTime} />
        </div>

        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={onCancel}>
            Cancel
          </Button>
          <Button
            disabled={!valid}
            onClick={() => {
              if (from && to && valid) onApply({ kind: 'custom', from, to })
            }}
          >
            Apply
          </Button>
        </div>
      </div>
    </div>
  )
}

function TimeField({
  label,
  value,
  onChange,
}: {
  label: string
  value: TimeDraft
  onChange: (next: TimeDraft) => void
}) {
  const invalid = to24Hour(value.time, value.meridiem) === null
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <Clock3
            className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
            aria-hidden
          />
          <Input
            value={value.time}
            onChange={(e) => {
              onChange({ ...value, time: e.target.value })
            }}
            placeholder="hh:mm:ss"
            inputMode="numeric"
            maxLength={8}
            aria-label={label}
            aria-invalid={invalid}
            className="h-9 pl-8 font-mono text-sm"
          />
        </div>
        <div
          className="flex rounded-md border bg-muted p-0.5"
          role="group"
          aria-label={`${label} AM or PM`}
        >
          {(['AM', 'PM'] as const).map((m) => (
            <button
              key={m}
              type="button"
              aria-pressed={value.meridiem === m}
              onClick={() => {
                onChange({ ...value, meridiem: m })
              }}
              className={cn(
                'rounded-sm px-2.5 py-1 text-xs transition-colors',
                value.meridiem === m
                  ? 'bg-card font-semibold text-foreground'
                  : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {m}
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
