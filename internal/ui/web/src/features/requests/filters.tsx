import { Search } from 'lucide-react'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { FilterOptions, RequestFilter, StatusFilter } from '@/features/requests/api'
import { providerLabel } from '@/lib/provider-colors'

const ALL = '__all' // Radix needs a non-empty value for the "no filter" choice

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'success', label: 'success' },
  { value: 'error', label: 'error' },
  { value: 'cancelled', label: 'cancelled' },
]

function FilterSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (value: string) => void
}) {
  // A value from the address that has no rows in this window stays selectable, so it can be undone.
  const items =
    value && !options.some((o) => o.value === value)
      ? [{ value, label: value }, ...options]
      : options

  return (
    <Select
      value={value || ALL}
      onValueChange={(next) => {
        onChange(next === ALL ? '' : next)
      }}
    >
      <SelectTrigger
        aria-label={label}
        className="gap-2 rounded-md bg-card px-3 text-xs data-[size=default]:h-[34px] data-[state=open]:border-primary"
      >
        <span className="text-muted-foreground">{label}</span>
        <SelectValue className="font-medium" />
      </SelectTrigger>
      <SelectContent position="popper" align="end" className="max-h-72">
        <SelectItem value={ALL}>all</SelectItem>
        {items.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function SearchBox({ text, onChange }: { text: string; onChange: (text: string) => void }) {
  return (
    <label className="relative min-w-[200px] flex-1">
      <Search
        className="pointer-events-none absolute top-1/2 left-3 size-[13px] -translate-y-1/2 text-muted-foreground"
        aria-hidden
      />
      <Input
        value={text}
        onChange={(e) => {
          onChange(e.target.value)
        }}
        placeholder="Search request id, client or model"
        aria-label="Search requests"
        className="h-[34px] rounded-md bg-background pl-8 text-xs"
      />
    </label>
  )
}

// 08.1 filters: one search box and a dropdown per field, each listing what exists in the window.
export function RequestFilters({
  filter,
  searchText,
  options,
  onSearch,
  onChange,
}: {
  filter: RequestFilter
  searchText: string
  options: FilterOptions | undefined
  onSearch: (text: string) => void
  onChange: (patch: Partial<RequestFilter>) => void
}) {
  const names = (values: string[] | undefined) =>
    (values ?? []).map((v) => ({ value: v, label: v }))

  return (
    <div className="flex flex-wrap items-center gap-2 px-[18px] pb-3.5">
      <SearchBox text={searchText} onChange={onSearch} />
      <FilterSelect
        label="Status"
        value={filter.status}
        options={STATUS_OPTIONS}
        onChange={(status) => {
          onChange({ status: status as StatusFilter })
        }}
      />
      <FilterSelect
        label="Provider"
        value={filter.provider}
        options={(options?.providers ?? []).map((p) => ({ value: p, label: providerLabel(p) }))}
        onChange={(provider) => {
          onChange({ provider })
        }}
      />
      <FilterSelect
        label="Model"
        value={filter.model}
        options={names(options?.models)}
        onChange={(model) => {
          onChange({ model })
        }}
      />
      <FilterSelect
        label="Virtual key"
        value={filter.client}
        options={names(options?.clients)}
        onChange={(client) => {
          onChange({ client })
        }}
      />
    </div>
  )
}
