import { RefreshCw } from 'lucide-react'
import { useNavigate } from 'react-router'
import { Button } from '@/components/ui/button'
import { useRecentRequests } from '@/features/metrics/api'
import { useSync } from '@/features/metrics/use-sync'
import { requestPath } from '@/features/requests/api'
import { ColumnsMenu, RequestsTable } from '@/features/requests/request-table'
import { cn } from '@/lib/utils'

const ROWS = 10

// The newest requests of the last few minutes, live and independent of the time picker.
// Sync refreshes the whole page, not just this table.
export function RecentRequests() {
  const recent = useRecentRequests(ROWS)
  const { sync, syncing } = useSync()
  const navigate = useNavigate()

  // A failed load is reported (with Retry) by the page's MetricsErrorBanner.
  if (recent.isError && !recent.data) return null

  return (
    <section className="flex min-h-[300px] flex-1 flex-col overflow-hidden rounded-xl border bg-card">
      <header className="flex shrink-0 items-center gap-2 px-[18px] pt-3.5 pb-3">
        <h2 className="flex-1 text-sm font-semibold">Recent requests</h2>
        <ColumnsMenu />
        <Button variant="outline" size="icon-sm" aria-label="Sync" onClick={sync}>
          <RefreshCw className={cn(syncing && 'animate-spin motion-reduce:animate-none')} />
        </Button>
      </header>

      {/* The rows scroll here, under a sticky header, with a thin scrollbar. The table's own
          wrapper must not scroll, or the sticky header would stick to it instead. */}
      <div className="min-h-0 flex-1 overflow-auto [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin] [&_[data-slot=table-container]]:overflow-visible">
        <RequestsTable
          sticky
          rows={recent.data ?? []}
          loading={recent.isPending}
          loadingRows={ROWS}
          empty="Waiting for new requests…"
          onOpen={(row) => {
            void navigate(requestPath(row.id))
          }}
        />
      </div>
    </section>
  )
}
