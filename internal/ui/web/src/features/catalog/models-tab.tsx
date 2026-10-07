import { ArrowUp, Search } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { KindTag } from '@/components/kind-tag'
import { Pager } from '@/components/pager'
import { ProviderLogo } from '@/components/provider-logo'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  MODELS_PAGE,
  useCatalogModels,
  useCatalogSummary,
  type ModelFilters,
} from '@/features/catalog/api'
import { formatCompact, formatCount } from '@/lib/format'
import { providerLabel } from '@/lib/provider-colors'
import { cn } from '@/lib/utils'

const ALL = 'all' // Radix Select has no empty value, so "all" stands for no filter
const TYPES = [
  'chat',
  'completion',
  'responses',
  'embedding',
  'image_generation',
  'image_edit',
  'audio_transcription',
  'audio_speech',
  'moderation',
  'rerank',
]
const SEARCH_DELAY_MS = 250

const head = 'h-[38px] px-1.5 text-[11px] font-semibold tracking-[0.06em] text-muted-foreground uppercase'

// Per-token price → per 1M tokens; tiny prices keep four decimals so they don't read as 0.00.
function per1M(perToken?: number): string {
  if (perToken === undefined) return '—'
  const p = perToken * 1e6
  return p.toLocaleString('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: p > 0 && p < 0.01 ? 4 : 2,
  })
}

function context(tokens?: number): string {
  if (!tokens) return '—'
  const f = formatCompact(tokens)
  return f.unit ? `${f.value} ${f.unit}` : f.value
}

function useDebounced<T>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => {
      setSettled(value)
    }, ms)
    return () => {
      clearTimeout(timer)
    }
  }, [value, ms])
  return settled
}

function Filter({
  label,
  value,
  onChange,
  children,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  children: ReactNode
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="h-9 gap-2 bg-card text-xs">
        <span className="text-muted-foreground">{label}</span>
        <SelectValue />
      </SelectTrigger>
      <SelectContent position="popper" align="end">
        <SelectItem value={ALL}>All</SelectItem>
        {children}
      </SelectContent>
    </Select>
  )
}

// Every model on the configured providers, ordered by provider then name. Search and filters
// run on the server, so they cover all pages, not just the one on screen.
export function ModelsTab() {
  const summary = useCatalogSummary()
  const [search, setSearch] = useState('')
  const [provider, setProvider] = useState(ALL)
  const [type, setType] = useState(ALL)
  const [pricing, setPricing] = useState(ALL)
  const [page, setPage] = useState(1)

  const q = useDebounced(search.trim(), SEARCH_DELAY_MS)
  const filters: ModelFilters = {
    q,
    provider: provider === ALL ? '' : provider,
    type: type === ALL ? '' : type,
    pricing: pricing === 'priced' || pricing === 'unpriced' ? pricing : '',
  }
  // Any filter change starts from the first page.
  const filterKey = JSON.stringify(filters)
  const [lastKey, setLastKey] = useState(filterKey)
  if (filterKey !== lastKey) {
    setLastKey(filterKey)
    setPage(1)
  }

  const models = useCatalogModels(filters, page)
  const rows = models.data?.models ?? []
  const total = models.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / MODELS_PAGE))

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2.5">
        <div className="relative min-w-60 flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => {
              setSearch(e.target.value)
            }}
            placeholder="Search models"
            aria-label="Search models"
            className="h-9 pl-8 text-xs"
          />
        </div>
        <Filter label="Provider" value={provider} onChange={setProvider}>
          {(summary.data?.providers ?? []).map((p) => (
            <SelectItem key={p} value={p}>
              {providerLabel(p)}
            </SelectItem>
          ))}
        </Filter>
        <Filter label="Type" value={type} onChange={setType}>
          {TYPES.map((t) => (
            <SelectItem key={t} value={t}>
              {t.replaceAll('_', ' ')}
            </SelectItem>
          ))}
        </Filter>
        <Filter label="Pricing" value={pricing} onChange={setPricing}>
          <SelectItem value="priced">Priced</SelectItem>
          <SelectItem value="unpriced">Unpriced</SelectItem>
        </Filter>
      </div>

      <div
        className={cn(
          'overflow-hidden rounded-xl border bg-card transition-opacity duration-200',
          models.isPlaceholderData && 'opacity-60',
        )}
      >
        <Table className="text-[13px] leading-[18px]">
          <TableHeader className="bg-background">
            <TableRow className="hover:bg-transparent">
              <TableHead className={`${head} pl-5`}>Model</TableHead>
              <TableHead className={cn(head, 'w-40 text-foreground')}>
                <span className="inline-flex items-center gap-1">
                  Provider <ArrowUp className="size-3" aria-label="sorted ascending" />
                </span>
              </TableHead>
              <TableHead className={`${head} w-32`}>Type</TableHead>
              <TableHead className={`${head} w-24 text-right`}>Context</TableHead>
              <TableHead className={`${head} w-28 text-right`}>Input $/1M</TableHead>
              <TableHead className={`${head} w-28 text-right`}>Output $/1M</TableHead>
              <TableHead className={`${head} w-32 text-right`}>Cache read $/1M</TableHead>
              <TableHead className={`${head} w-36 pr-5 text-right`}>Cache write $/1M</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {models.isPending ? (
              Array.from({ length: MODELS_PAGE }, (_, i) => (
                <TableRow key={i} className="h-[46px] hover:bg-transparent">
                  <TableCell colSpan={8} className="px-5">
                    <Skeleton className="h-4 w-full" />
                  </TableCell>
                </TableRow>
              ))
            ) : models.isError ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={8} className="h-32 text-center text-sm text-destructive">
                  Could not load models: {models.error.message}
                </TableCell>
              </TableRow>
            ) : rows.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={8} className="h-32 text-center text-sm text-muted-foreground">
                  No models match these filters
                </TableCell>
              </TableRow>
            ) : (
              rows.map((m) => {
                const price = m.pricing
                const cells = [
                  price?.input_cost_per_token,
                  price?.output_cost_per_token,
                  price?.cache_read_input_token_cost,
                  price?.cache_creation_input_token_cost,
                ].map(per1M)
                return (
                  <TableRow key={`${m.provider}/${m.model_name}/${m.model_type}`} className="h-[46px] hover:bg-nav-selected">
                    <TableCell className="pl-5 font-mono font-medium">{m.model_name}</TableCell>
                    <TableCell className="px-1.5">
                      <span className="flex items-center gap-2.5">
                        <ProviderLogo provider={m.provider} />
                        {providerLabel(m.provider)}
                      </span>
                    </TableCell>
                    <TableCell className="px-1.5">
                      <KindTag kind={m.model_type} />
                    </TableCell>
                    <TableCell className="px-1.5 text-right font-mono">
                      {context(m.limits.context_window)}
                    </TableCell>
                    {cells.map((v, i) => (
                      <TableCell
                        key={i}
                        className={cn(
                          'px-1.5 text-right font-mono',
                          v === '—' && 'text-muted-foreground',
                          i === cells.length - 1 && 'pr-5',
                        )}
                      >
                        {v}
                      </TableCell>
                    ))}
                  </TableRow>
                )
              })
            )}
          </TableBody>
        </Table>
      </div>

      {total > 0 && (
        <Pager
          summary={`Showing ${formatCount((page - 1) * MODELS_PAGE + 1).value}–${formatCount(Math.min(page * MODELS_PAGE, total)).value} of ${formatCount(total).value} models`}
          page={page}
          pages={pages}
          onPage={setPage}
        />
      )}
    </div>
  )
}
