import { RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Pager } from '@/components/pager'
import { TimeRangePicker } from '@/components/time-range-picker'
import { Button } from '@/components/ui/button'
import { useSync } from '@/features/metrics/use-sync'
import { KpiRow } from '@/features/overview/kpi-row'
import {
  REQUEST_PAGE_SIZE,
  isFiltered,
  requestPath,
  useRequestFilterOptions,
  useRequestLog,
} from '@/features/requests/api'
import { RequestFilters } from '@/features/requests/filters'
import { useRequestQuery } from '@/features/requests/filter-state'
import { ColumnsMenu, RequestsTable } from '@/features/requests/request-table'
import { useRangeWindow, useTimeRange } from '@/lib/time-range'
import { cn } from '@/lib/utils'

// How often a live table asks for newer requests, and how far past "now" a paused one looks.
const LIVE_MS = 5_000
const PAUSED_REACH_MS = 60_000

function LivePill({
  on,
  disabled,
  onToggle,
}: {
  on: boolean
  disabled: boolean
  onToggle: () => void
}) {
  return (
    <button
      type="button"
      aria-pressed={on}
      disabled={disabled}
      title={disabled ? 'Live follows a preset window. A custom range is fixed.' : undefined}
      onClick={onToggle}
      className={cn(
        'flex items-center gap-[7px] rounded-full border bg-background px-[13px] py-[7px] text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50',
        on ? 'border-success/40 text-success' : 'text-muted-foreground hover:text-foreground',
      )}
    >
      <span
        className={cn(
          'size-[7px] rounded-full',
          on ? 'bg-success motion-safe:animate-pulse' : 'border border-current',
        )}
        aria-hidden
      />
      Live
    </button>
  )
}

// 08.1 · Requests. Every request in the window, newest first. Live keeps page 1 current with the
// newest rows; leave page 1, pick a custom range or switch it off and the list holds still.
export default function RequestsPage() {
  const [spec, setSpec] = useTimeRange()
  const range = useRangeWindow(spec)
  const { filter, page, update, searchText, setSearch, setPage, clear, search } = useRequestQuery()
  const [live, setLive] = useState(true)
  const navigate = useNavigate()
  const { sync, syncing } = useSync()

  // Live only makes sense for a window that ends now, and only on the page that shows the newest.
  // Paused, a preset window reaches a minute past the clock's last tick, so the rows that were
  // on screen a moment ago do not drop out of it.
  const liveOn = live && spec.kind === 'preset'
  const following = liveOn && page === 1
  const followed = useRangeWindow(spec, following ? LIVE_MS : undefined)
  const tableRange =
    following || spec.kind !== 'preset'
      ? followed
      : { from: range.from, to: new Date(range.to.getTime() + PAUSED_REACH_MS) }
  const log = useRequestLog(tableRange, filter, page)
  const options = useRequestFilterOptions(range)

  const total = log.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / REQUEST_PAGE_SIZE))
  const shown = log.data?.rows.length ?? 0
  const first = (page - 1) * REQUEST_PAGE_SIZE + 1

  // A narrower window or a new filter can leave the address on a page that no longer exists.
  useEffect(() => {
    if (log.data && !log.isPlaceholderData && page > pages) setPage(pages)
  }, [log.data, log.isPlaceholderData, page, pages, setPage])

  const filtered = isFiltered(filter)

  return (
    <>
      <PageHeader
        title="Requests"
        actions={
          <>
            <LivePill
              on={liveOn}
              disabled={spec.kind !== 'preset'}
              onToggle={() => {
                setLive(!live)
              }}
            />
            <TimeRangePicker spec={spec} window={range} onChange={setSpec} />
          </>
        }
      />
      <PageBody>
        <KpiRow window={range} />

        <div className="flex flex-col gap-3">
          <section className="overflow-hidden rounded-[4px] border bg-card">
            <header className="flex items-center gap-2 px-[18px] pt-3.5 pb-3">
              <h2 className="flex-1 text-sm font-semibold">Request log</h2>
              <ColumnsMenu />
              <Button variant="outline" size="icon-sm" aria-label="Sync" onClick={sync}>
                <RefreshCw className={cn(syncing && 'animate-spin motion-reduce:animate-none')} />
              </Button>
            </header>

            <RequestFilters
              filter={filter}
              searchText={searchText}
              options={options.data}
              onSearch={setSearch}
              onChange={update}
            />

            {log.isError && !log.data ? (
              <div
                role="alert"
                className="flex items-center gap-3 border-t px-[18px] py-6 text-sm text-destructive"
              >
                Could not load requests: {log.error.message}
                <Button
                  size="sm"
                  variant="outline"
                  className="ml-auto"
                  onClick={() => void log.refetch()}
                >
                  Retry
                </Button>
              </div>
            ) : (
              <RequestsTable
                rows={log.data?.rows ?? []}
                loading={log.isPending}
                loadingRows={10}
                empty={
                  filtered ? (
                    <>
                      No requests match these filters.{' '}
                      <button
                        type="button"
                        onClick={clear}
                        className="font-medium text-primary hover:underline"
                      >
                        Clear filters
                      </button>
                    </>
                  ) : (
                    'No requests in this window.'
                  )
                }
                onOpen={(row) => {
                  void navigate(requestPath(row.id), { state: { back: search } })
                }}
              />
            )}
          </section>

          {total > 0 && (
            <Pager
              summary={`Showing ${first.toLocaleString('en-US')}–${(first + shown - 1).toLocaleString('en-US')} of ${total.toLocaleString('en-US')} ${total === 1 ? 'request' : 'requests'}`}
              page={page}
              pages={pages}
              onPage={setPage}
            />
          )}
        </div>
      </PageBody>
    </>
  )
}
