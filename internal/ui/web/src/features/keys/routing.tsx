import { CircleCheck, Info, Plus, Trash2, TriangleAlert } from 'lucide-react'
import type { ReactNode } from 'react'
import { ProviderLogo } from '@/components/provider-logo'
import { Button } from '@/components/ui/button'
import { Command, CommandEmpty, CommandItem, CommandList } from '@/components/ui/command'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { newDraft, rebalance, weightTotal, type RouteDraft } from '@/features/keys/routing-utils'
import { useAllCredentials } from '@/features/providers/api'
import { ModelChips, Segmented } from '@/features/providers/form'
import { providerLabel } from '@/lib/provider-colors'
import { cn } from '@/lib/utils'

// The ⓘ beside the routing title: how weights work, on click (design 05.2b).
function WeightsInfo() {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="How weights work"
          className="flex size-[18px] items-center justify-center rounded-full bg-nav-selected text-foreground transition-colors hover:bg-muted data-[state=open]:bg-primary data-[state=open]:text-primary-foreground"
        >
          <Info className="size-3" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[340px] gap-2 rounded-md p-3.5">
        <span className="text-[13px] font-semibold">How weights work</span>
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          Weights split traffic between providers and must total 1.00 before the key can be saved.
        </p>
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          A single provider is fixed at 1.00. Add a second and both become editable.
        </p>
      </PopoverContent>
    </Popover>
  )
}

function AddProvider({ used, onAdd }: { used: string[]; onAdd: (provider: string) => void }) {
  const creds = useAllCredentials()
  // Only providers that hold a credential can serve traffic.
  const options = [...new Set((creds.data ?? []).map((c) => c.provider))]
    .filter((p) => !used.includes(p))
    .sort()
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" disabled={options.length === 0}>
          <Plus className="text-muted-foreground" />
          Add provider
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[260px] p-1.5">
        <Command>
          <CommandList>
            <CommandEmpty className="py-4 text-sm text-muted-foreground">
              No other provider has a credential.
            </CommandEmpty>
            {options.map((p) => (
              <CommandItem
                key={p}
                value={p}
                onSelect={() => {
                  onAdd(p)
                }}
                className="gap-2.5 py-2"
              >
                <span className="flex size-8 shrink-0 items-center justify-center rounded-md border bg-muted">
                  <ProviderLogo provider={p} className="size-5" />
                </span>
                <span className="text-sm font-medium">{providerLabel(p)}</span>
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

function TotalWeight({ drafts }: { drafts: RouteDraft[] }) {
  if (drafts.length < 2) return null
  const total = weightTotal(drafts)
  const ok = Math.abs(total - 1) <= 1e-6
  return (
    <span
      className={cn(
        'flex items-center gap-1.5 font-mono text-[11px]',
        ok ? 'text-success' : 'text-warning',
      )}
    >
      {ok ? <CircleCheck className="size-3.5" /> : <TriangleAlert className="size-3.5" />}
      Total {Number.isFinite(total) ? total.toFixed(2) : '—'}
    </span>
  )
}

function ProviderBlock({
  draft,
  single,
  onChange,
  onRemove,
}: {
  draft: RouteDraft
  single: boolean
  onChange: (next: RouteDraft) => void
  onRemove: () => void
}) {
  const name = providerLabel(draft.provider)
  return (
    <div className="flex flex-col gap-3.5 rounded-lg border bg-muted p-4">
      <div className="flex items-center gap-2.5">
        <span className="flex size-[34px] shrink-0 items-center justify-center rounded-lg border bg-card">
          <ProviderLogo provider={draft.provider} className="size-5" />
        </span>
        <span className="flex-1 truncate text-sm font-semibold">{name}</span>
        <span className="text-[10px] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
          Weight
        </span>
        <Input
          inputMode="decimal"
          aria-label={`${name} weight`}
          value={single ? '1.00' : draft.weight}
          disabled={single}
          onChange={(e) => {
            onChange({ ...draft, weight: e.target.value })
          }}
          className="h-8 w-16 text-center font-mono text-xs md:text-xs"
        />
        {!single && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Remove ${name}`}
            onClick={onRemove}
            className="text-muted-foreground hover:text-destructive"
          >
            <Trash2 />
          </Button>
        )}
      </div>

      <div className="flex flex-col gap-2">
        <span className="text-xs text-muted-foreground">
          Allowed models <span className="text-destructive">*</span>
        </span>
        <Segmented
          label={`${name} allowed models`}
          value={draft.allModels ? 'all' : 'specific'}
          options={[
            ['all', 'All models'],
            ['specific', 'Specific models'],
          ]}
          onChange={(v) => {
            onChange({ ...draft, allModels: v === 'all' })
          }}
        />
        {!draft.allModels && (
          <ModelChips
            provider={draft.provider}
            value={draft.allowed}
            onChange={(allowed) => {
              onChange({ ...draft, allowed })
            }}
            placeholder={`Search ${name} models to allow`}
          />
        )}
      </div>

      <div className="flex flex-col gap-2">
        <span className="text-xs text-muted-foreground">
          Blocked models <span className="ml-1 text-[10px]">optional</span>
        </span>
        <ModelChips
          provider={draft.provider}
          value={draft.blocked}
          onChange={(blocked) => {
            onChange({ ...draft, blocked })
          }}
          placeholder={`Search ${name} models to block`}
        />
        <span className="text-[11px] text-muted-foreground">
          A block wins over the allow list on this provider.
        </span>
      </div>
    </div>
  )
}

// A key's providers: which models each may serve and how traffic is split (designs 05.2, 05.3).
// `lead` is whatever sits at the left of the toolbar: a title on the detail page, a hint in the drawer.
export function RoutingEditor({
  value,
  onChange,
  lead,
}: {
  value: RouteDraft[]
  onChange: (next: RouteDraft[]) => void
  lead: ReactNode
}) {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2.5">
        {lead}
        <WeightsInfo />
        <span className="flex-1" />
        <TotalWeight drafts={value} />
        <AddProvider
          used={value.map((d) => d.provider)}
          onAdd={(p) => {
            onChange(rebalance([...value, newDraft(p)]))
          }}
        />
      </div>
      {value.length === 0 && (
        <p className="rounded-lg border border-dashed p-4 text-center text-xs text-muted-foreground">
          No provider yet. Add one to say where this key may send traffic.
        </p>
      )}
      {value.map((d, i) => (
        <ProviderBlock
          key={d.provider}
          draft={d}
          single={value.length === 1}
          onChange={(next) => {
            onChange(value.map((x, j) => (j === i ? next : x)))
          }}
          onRemove={() => {
            onChange(rebalance(value.filter((_, j) => j !== i)))
          }}
        />
      ))}
    </div>
  )
}
