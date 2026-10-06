import { lazy, Suspense } from 'react'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { TimeRangePicker } from '@/components/time-range-picker'
import { Skeleton } from '@/components/ui/skeleton'
import { useHasTraffic } from '@/features/metrics/api'
import { KpiRow } from '@/features/overview/kpi-row'
import { MetricsErrorBanner } from '@/features/overview/metrics-error-banner'
import { RecentRequests } from '@/features/overview/recent-requests'
import { useRangeWindow, useTimeRange } from '@/lib/time-range'

// Recharts is ~100KB gzipped: load it with the chart, not with the app.
const RequestsChart = lazy(() => import('@/features/overview/requests-chart'))

// 01 · Overview (ADR-001 §8). The budget alert (features/overview/budget-alert.tsx) is parked
// for now. One window for the tiles and the chart: they receive the same {from, to}, so they
// always describe one span. Recent requests are a live view and ignore it.
export default function OverviewPage() {
  const [spec, setSpec] = useTimeRange()
  const window = useRangeWindow(spec)
  // A gateway that has never served a request shows tiles only: empty charts are noise.
  const served = useHasTraffic().data !== false

  return (
    <>
      <PageHeader
        title="Gateway Overview"
        actions={<TimeRangePicker spec={spec} window={window} onChange={setSpec} />}
      />
      {/* min-h-0: the body may be shorter than its content, so the table card scrolls inside. */}
      <PageBody className="min-h-0">
        <MetricsErrorBanner window={window} />
        <KpiRow window={window} />
        {served && (
          <>
            <Suspense fallback={<Skeleton className="h-[286px] w-full rounded-xl md:h-[334px]" />}>
              <RequestsChart window={window} />
            </Suspense>
            <RecentRequests />
          </>
        )}
      </PageBody>
    </>
  )
}
