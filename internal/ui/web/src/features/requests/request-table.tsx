import { Columns3 } from 'lucide-react'
import type { ReactNode } from 'react'
import { KindTag } from '@/components/kind-tag'
import { Swatch } from '@/components/swatch'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { COLUMNS, useVisibleColumns, type ColumnKey } from '@/features/overview/recent-columns'
import type { RequestLogRow } from '@/features/metrics/api'
import { formatCount, formatLatency, formatUSD, type Formatted } from '@/lib/format'
import { providerColor, providerLabel } from '@/lib/provider-colors'
import { formatInZone, useTimezone } from '@/lib/timezone'
import { cn } from '@/lib/utils'

const text = (f: Formatted) => (f.unit ? `${f.value} ${f.unit}` : f.value)

// A cancelled request answered 200 before the client left, so the result decides, not the code.
function statusOf(row: RequestLogRow): { label: string; tone: string } {
  if (row.result === 'client_abort') return { label: 'cancelled', tone: 'text-muted-foreground' }
  if (row.http_status === 0) {
    return { label: row.result.replaceAll('_', ' '), tone: 'text-destructive' }
  }
  return {
    label: String(row.http_status),
    tone: row.http_status < 400 ? 'text-success' : 'text-destructive',
  }
}

const Dash = () => <span className="text-muted-foreground">—</span>

function Cell({ column, row, zone }: { column: ColumnKey; row: RequestLogRow; zone: string }) {
  switch (column) {
    case 'time':
      return (
        <span className="font-mono">
          {formatInZone(new Date(row.started_at), zone, 'MMM dd HH:mm:ss')}
        </span>
      )
    case 'requestId':
      return row.request_id ? (
        <span className="block truncate font-mono text-xs" title={row.request_id}>
          {row.request_id}
        </span>
      ) : (
        <Dash />
      )
    case 'type':
      return <KindTag kind={row.request_kind} />
    case 'provider':
      return row.provider ? (
        <span className="flex items-center gap-2">
          <Swatch color={providerColor(row.provider)} />
          {providerLabel(row.provider)}
        </span>
      ) : (
        <Dash />
      )
    case 'model':
      return row.model ? <span className="font-medium">{row.model}</span> : <Dash />
    case 'vk':
      return row.client_id ? <span>{row.client_id}</span> : <Dash />
    case 'status': {
      const { label, tone } = statusOf(row)
      return (
        <span className={cn('flex items-center gap-1.5 font-mono', tone)}>
          <span className="size-1.5 shrink-0 rounded-full bg-current" aria-hidden />
          {label}
        </span>
      )
    }
    case 'latency':
      return <span className="font-mono">{text(formatLatency(row.latency_ms))}</span>
    case 'tokens': {
      const total = row.input_tokens + row.output_tokens
      if (total === 0) return <span className="font-mono text-muted-foreground">—</span>
      return (
        <span className="flex flex-col gap-0.5">
          <span className="font-mono">{text(formatCount(total))}</span>
          <span className="text-xs leading-4 text-muted-foreground">
            {text(formatCount(row.input_tokens))} in · {text(formatCount(row.output_tokens))} out
          </span>
        </span>
      )
    }
    case 'cost':
      return <span className="font-mono">{text(formatUSD(row.cost_usd))}</span>
  }
}

export function ColumnsMenu() {
  const { visible, toggle, reset } = useVisibleColumns()

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm">
          <Columns3 />
          Columns
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-56 gap-0.5 rounded-[4px] p-1.5">
        <div className="flex items-center px-2 pt-1.5 pb-2">
          <span className="flex-1 text-[10px] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
            Columns
          </span>
          <button
            type="button"
            onClick={reset}
            className="text-[11px] font-medium text-primary hover:underline"
          >
            Reset
          </button>
        </div>
        {COLUMNS.map((c) => {
          const on = visible.includes(c.key)
          return (
            <label
              key={c.key}
              className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-[7px] text-xs hover:bg-nav-selected"
            >
              <Checkbox
                checked={on}
                disabled={on && visible.length === 1}
                onCheckedChange={() => {
                  toggle(c.key)
                }}
              />
              {c.label}
            </label>
          )
        })}
      </PopoverContent>
    </Popover>
  )
}

// The rows of a request log, in the columns the viewer chose. Used by the Overview's live table and
// the Requests page, so both show the same thing the same way. Rows open the request when `onOpen`
// is given. The caller owns the card, the scrolling and the footer.
export function RequestsTable({
  rows,
  loading,
  loadingRows,
  empty,
  sticky,
  onOpen,
}: {
  rows: RequestLogRow[]
  loading: boolean
  loadingRows: number
  empty: ReactNode
  sticky?: boolean
  onOpen?: (row: RequestLogRow) => void
}) {
  const { visible } = useVisibleColumns()
  const { zone } = useTimezone()
  const columns = COLUMNS.filter((c) => visible.includes(c.key))
  const last = columns.length - 1

  return (
    <Table className="text-[13px] leading-[18px]">
      <TableHeader className={cn('bg-background', sticky && 'sticky top-0 z-10')}>
        <TableRow className="hover:bg-transparent">
          {columns.map((c, i) => (
            <TableHead
              key={c.key}
              className={cn(
                'h-[38px] px-1.5 text-[11px] font-semibold tracking-[0.06em] text-muted-foreground uppercase',
                c.width,
                i === 0 && 'pl-[18px]',
                i === last && 'pr-[18px]',
                c.key === 'cost' && 'text-right',
              )}
            >
              {c.label}
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          Array.from({ length: loadingRows }, (_, i) => (
            <TableRow key={i} className="h-[54px] hover:bg-transparent">
              <TableCell colSpan={columns.length} className="px-[18px]">
                <Skeleton className="h-4 w-full" />
              </TableCell>
            </TableRow>
          ))
        ) : rows.length === 0 ? (
          <TableRow className="hover:bg-transparent">
            <TableCell
              colSpan={columns.length}
              className="h-40 text-center text-sm text-muted-foreground"
            >
              {empty}
            </TableCell>
          </TableRow>
        ) : (
          rows.map((row) => (
            <TableRow
              key={row.id}
              tabIndex={onOpen ? 0 : undefined}
              onClick={
                onOpen &&
                (() => {
                  onOpen(row)
                })
              }
              onKeyDown={
                onOpen &&
                ((e) => {
                  if (e.key === 'Enter') onOpen(row)
                })
              }
              className={cn(
                'h-[54px] hover:bg-nav-selected',
                onOpen && 'cursor-pointer focus-visible:bg-nav-selected focus-visible:outline-none',
              )}
            >
              {columns.map((c, i) => (
                <TableCell
                  key={c.key}
                  className={cn(
                    'px-1.5',
                    i === 0 && 'pl-[18px]',
                    i === last && 'pr-[18px]',
                    c.key === 'cost' && 'text-right',
                  )}
                >
                  <Cell column={c.key} row={row} zone={zone} />
                </TableCell>
              ))}
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}
