import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { TimeRangePicker } from '@/components/time-range-picker'
import { useHasTraffic } from '@/features/metrics/api'
import { KpiRow } from '@/features/overview/kpi-row'
import { MetricsErrorBanner } from '@/features/overview/metrics-error-banner'
import { RecentRequests } from '@/features/overview/recent-requests'
import { RequestsChart } from '@/features/overview/requests-chart'
import { useRangeWindow, useTimeRange } from '@/lib/time-range'

// 01 · Overview (ADR-001 §8). The budget alert (features/overview/budget-alert.tsx) is parked
// for now. One window for the tiles and the chart: they receive the same {from, to}, so they
// always describe one span, and zooming the chart moves that window. Recent requests are a live
// view and ignore it.
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
            <RequestsChart window={window} />
            <RecentRequests />
          </>
        )}
      </PageBody>
    </>
  )
}
