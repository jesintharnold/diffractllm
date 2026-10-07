import { ChevronRight, Info } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Pager } from '@/components/pager'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useOverrides, useVirtualKeys, type Override, type Scope } from '@/features/pricing/api'
import { providerLabel } from '@/lib/provider-colors'
import { cn } from '@/lib/utils'

const PAGE = 10
const head = 'h-[38px] px-1.5 text-[11px] font-semibold tracking-[0.06em] text-muted-foreground uppercase'

const SCOPE: Record<Scope, { label: string; tone: string }> = {
  virtualkey: { label: 'virtual key', tone: 'border-tint-openai-edge bg-tint-openai text-primary' },
  provider: { label: 'provider', tone: 'border-tint-azure-edge bg-tint-azure text-info' },
  global: { label: 'global', tone: 'border-tint-neutral-edge bg-tint-neutral text-foreground' },
}

const rate = (perToken?: number) =>
  perToken === undefined ? '—' : (perToken * 1e6).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 4 })

// The precedence chain, behind the ⓘ next to the title (design 03.2).
function ResolutionInfo() {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="How a price is resolved"
          className="flex size-[18px] items-center justify-center rounded-full bg-nav-selected text-foreground hover:bg-muted"
        >
          <Info className="size-3" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[420px] gap-3 rounded-md p-4">
        <span className="text-[13px] font-semibold">How a price is resolved</span>
        <div className="flex items-center gap-1.5">
          {['Virtual key', 'Provider', 'Global', 'Base'].map((step, i) => (
            <span key={step} className="flex items-center gap-1.5">
              {i > 0 && <ChevronRight className="size-3 text-muted-foreground" />}
              <span
                className={cn(
                  'rounded-[4px] border px-2 py-1 text-[11px] font-medium',
                  i === 0 ? 'border-tint-openai-edge bg-tint-openai font-semibold text-primary' : 'bg-muted',
                )}
              >
                {step}
              </span>
            </span>
          ))}
        </div>
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          The narrowest scope that sets a field wins. Fields an override leaves empty fall through
          to the next scope, ending at the catalog&apos;s base price.
        </p>
      </PopoverContent>
    </Popover>
  )
}

export function OverridesTable({ onEdit }: { onEdit: (override: Override) => void }) {
  const overrides = useOverrides()
  const keys = useVirtualKeys()
  const [page, setPage] = useState(1)

  const rows = overrides.data ?? []
  const keyName = useMemo(
    () => new Map((keys.data ?? []).map((k) => [k.id, k.client_id || k.display_prefix])),
    [keys.data],
  )
  const appliesTo = (o: Override) =>
    o.scope_type === 'virtualkey'
      ? (keyName.get(o.scope_virtual_key_id ?? '') ?? o.scope_virtual_key_id ?? '—')
      : o.scope_type === 'provider'
        ? providerLabel(o.scope_provider ?? '')
        : 'all keys'

  const scopes = new Set(rows.map((o) => o.scope_type)).size
  const counts = (['virtualkey', 'provider', 'global'] as const)
    .map((s) => [rows.filter((o) => o.scope_type === s).length, SCOPE[s].label] as const)
    .filter(([n]) => n > 0)
    .map(([n, label]) => `${String(n)} ${label}`)
    .join(', ')

  const pages = Math.max(1, Math.ceil(rows.length / PAGE))
  const current = Math.min(page, pages)
  const visible = rows.slice((current - 1) * PAGE, current * PAGE)

  return (
    <div className="flex flex-col gap-3">
      <section className="overflow-hidden rounded-xl border bg-card">
        <header className="flex flex-col gap-1 px-5 pt-4 pb-3">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold">Active overrides</h2>
            <ResolutionInfo />
          </div>
          <span className="text-[11px] text-muted-foreground">
            {overrides.isPending
              ? 'Loading…'
              : `${String(rows.length)} ${rows.length === 1 ? 'rule' : 'rules'} across ${String(scopes)} ${scopes === 1 ? 'scope' : 'scopes'}`}
          </span>
        </header>
        <Table className="text-[13px] leading-[18px]">
          <TableHeader className="bg-background">
            <TableRow className="hover:bg-transparent">
              <TableHead className={`${head} w-36 pl-5`}>Scope</TableHead>
              <TableHead className={`${head} w-44`}>Applies to</TableHead>
              <TableHead className={head}>Model</TableHead>
              <TableHead className={`${head} w-32`}>Input $/1M</TableHead>
              <TableHead className={`${head} w-32`}>Output $/1M</TableHead>
              <TableHead className={`${head} w-24 pr-5 text-right`}>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {overrides.isPending ? (
              Array.from({ length: 3 }, (_, i) => (
                <TableRow key={i} className="h-12 hover:bg-transparent">
                  <TableCell colSpan={6} className="px-5">
                    <Skeleton className="h-4 w-full" />
                  </TableCell>
                </TableRow>
              ))
            ) : overrides.isError ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={6} className="h-32 text-center text-sm text-destructive">
                  Could not load overrides: {overrides.error.message}
                </TableCell>
              </TableRow>
            ) : visible.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={6} className="h-32 text-center text-sm text-muted-foreground">
                  No overrides yet. Every model is billed at its catalog base price.
                </TableCell>
              </TableRow>
            ) : (
              visible.map((o) => (
                <TableRow key={o.id} className="h-12 hover:bg-nav-selected">
                  <TableCell className="pl-5">
                    <span
                      className={cn(
                        'inline-flex rounded-[4px] border px-2 py-[3px] text-[11px] leading-[15px] font-medium',
                        SCOPE[o.scope_type].tone,
                      )}
                    >
                      {SCOPE[o.scope_type].label}
                    </span>
                  </TableCell>
                  <TableCell className="px-1.5">{appliesTo(o)}</TableCell>
                  <TableCell className="px-1.5 font-mono">{o.model_name}</TableCell>
                  <TableCell className="px-1.5 font-mono">{rate(o.pricing.input_cost_per_token)}</TableCell>
                  <TableCell className="px-1.5 font-mono">{rate(o.pricing.output_cost_per_token)}</TableCell>
                  <TableCell className="px-1.5 pr-5 text-right">
                    <Button
                      variant="link"
                      size="sm"
                      className="h-auto p-0 text-xs"
                      onClick={() => {
                        onEdit(o)
                      }}
                    >
                      Edit
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </section>
      {rows.length > 0 && (
        <Pager
          summary={`Showing ${String((current - 1) * PAGE + 1)}–${String(Math.min(current * PAGE, rows.length))} of ${String(rows.length)} overrides · ${counts}`}
          page={current}
          pages={pages}
          onPage={setPage}
        />
      )}
    </div>
  )
}
