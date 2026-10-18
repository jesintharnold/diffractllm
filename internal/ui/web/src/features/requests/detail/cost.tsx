import { DetailCard } from '@/components/detail'
import type { CostLine, RequestDetail } from '@/features/requests/api'
import { dollars, words } from '@/features/requests/detail/format'
import { cn } from '@/lib/utils'

const GRID = 'grid grid-cols-[minmax(0,1fr)_72px_130px_80px] gap-2'
const PER_MILLION = 1_000_000

// The lines every request shows, so two requests always read the same way. Anything else the
// price list charged for (audio, video, images, searches) follows them.
const STANDARD: { item: string; label: string }[] = [
  { item: 'input', label: 'input' },
  { item: 'input_cache_read', label: 'cache read' },
  { item: 'output', label: 'output' },
]

const isTokens = (line: CostLine) => line.unit === 'token'
const isStandard = (line: CostLine) => isTokens(line) && STANDARD.some((s) => s.item === line.item)

const grouped = (n: number) => n.toLocaleString('en-US', { maximumFractionDigits: 2 })

// A price keeps the digits it has: $2.50, $0.0000025.
const price = (usd: number) => `$${usd >= 0.01 ? usd.toFixed(2) : usd.toPrecision(2)}`

// Tokens are priced per million, anything else (seconds, images, queries) per unit.
const rate = (line: CostLine) =>
  isTokens(line)
    ? `${price(line.unit_cost_usd * PER_MILLION)} / 1M`
    : `${price(line.unit_cost_usd)} / ${line.unit}`

// Short names by modality: input_audio → "audio in", output_video → "video out", and the cache
// lines keep their own name. The unit is in the Rate column, so the name does not repeat it.
function labelOf(item: string): string {
  const named = words(item)
  const direction = /^(input|output) (.+)$/.exec(named)
  if (!direction) return named
  const [, way, rest = ''] = direction
  return rest.includes('cache') ? rest : `${rest} ${way === 'input' ? 'in' : 'out'}`
}

interface Row {
  key: string
  label: string
  line?: CostLine
}

function rowsOf(lines: CostLine[]): Row[] {
  const standard = STANDARD.map(({ item, label }) => ({
    key: item,
    label,
    line: lines.find((l) => l.item === item && isTokens(l)),
  }))
  const extra = lines
    .filter((l) => !isStandard(l))
    .map((line) => ({
      key: `${line.item}-${line.unit}`,
      label: labelOf(line.item),
      line,
    }))
  return [...standard, ...extra]
}

const Dash = () => <span className="text-muted-foreground">—</span>

// The bill, line by line. The Cost tile above carries the stored nano-USD figure.
export function CostCard({ detail }: { detail: RequestDetail }) {
  const rows = rowsOf(detail.cost.lines ?? [])

  return (
    <DetailCard title="Cost breakdown" className="gap-1.5 rounded-[4px] p-4">
      <div
        className={cn(
          GRID,
          'text-[10px] font-semibold tracking-[0.06em] text-muted-foreground uppercase',
        )}
      >
        <span>Item</span>
        <span className="text-right">Qty</span>
        <span className="text-right">Rate</span>
        <span className="text-right">Amount</span>
      </div>
      <div className="flex flex-col gap-1">
        {rows.map(({ key, label, line }) => (
          <div key={key} className={cn(GRID, 'items-baseline')}>
            <span className="truncate text-[13px] font-medium">{label}</span>
            <span className="text-right font-mono text-xs text-muted-foreground">
              {line ? grouped(line.quantity) : <Dash />}
            </span>
            <span className="text-right font-mono text-xs whitespace-nowrap text-muted-foreground">
              {line ? rate(line) : <Dash />}
            </span>
            <span className="text-right font-mono text-[13px]">
              {line ? dollars(line.cost_usd) : <Dash />}
            </span>
          </div>
        ))}
      </div>
      <div className="mt-0.5 flex items-baseline justify-between border-t pt-1.5">
        <span className="text-[13px] font-semibold">Total</span>
        <span className="font-mono text-sm font-semibold">{dollars(detail.cost.total_usd)}</span>
      </div>
    </DetailCard>
  )
}
