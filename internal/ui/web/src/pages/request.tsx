import { ArrowLeft, Eye, EyeOff, List, Waves } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, useLocation, useParams } from 'react-router'
import { DetailCard, DetailRow, DetailRows, StatTile } from '@/components/detail'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Swatch } from '@/components/swatch'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { isNotFound, useRequestDetail, type RequestDetail } from '@/features/requests/api'
import { useContextNames } from '@/features/requests/detail/context-names'
import { CostCard } from '@/features/requests/detail/cost'
import {
  PILL_TONE,
  dollars,
  duration,
  nanoUSD,
  reachedProvider,
  statusPill,
} from '@/features/requests/detail/format'
import { PayloadCard } from '@/features/requests/detail/payload'
import { PipelineCard } from '@/features/requests/detail/pipeline'
import { FactsCard } from '@/features/requests/detail/side'
import { formatCount } from '@/lib/format'
import { providerColor } from '@/lib/provider-colors'
import { formatInZone, useTimezone } from '@/lib/timezone'
import { cn } from '@/lib/utils'

const LIST = '/observability/requests'
const TILE = 'rounded-[4px]'
const dash = <span className="text-muted-foreground">—</span>

// The list the admin came from, filters and page included, so Back returns to the same view.
function useBackPath(): string {
  const state = useLocation().state as { back?: unknown } | null
  return typeof state?.back === 'string' ? `${LIST}${state.back}` : LIST
}

function Tiles({ d }: { d: RequestDetail }) {
  const reached = reachedProvider(d)
  const { tokens } = d
  const billed = tokens.billed ?? 0
  const split = [
    `${(tokens.input ?? 0).toLocaleString('en-US')} in`,
    (tokens.cached_input ?? 0) > 0 &&
      `${(tokens.cached_input ?? 0).toLocaleString('en-US')} cached`,
    `${(tokens.output ?? 0).toLocaleString('en-US')} out`,
  ]
    .filter(Boolean)
    .join(' · ')

  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <StatTile
        className={TILE}
        label="Asked for"
        value={d.model.requested ?? dash}
        sub={`POST ${d.endpoint}`}
      />
      <StatTile
        className={TILE}
        label="Served by"
        value={
          d.model.provider ? (
            <span className="flex items-center gap-2">
              <Swatch color={providerColor(d.model.provider)} className="size-2.5" />
              {d.model.provider}
            </span>
          ) : (
            dash
          )
        }
        sub={
          d.model.provider
            ? [
                d.model.upstream ?? d.model.model,
                d.context.credential && `credential ${d.context.credential}`,
              ]
                .filter(Boolean)
                .join(' · ')
            : 'never reached a provider'
        }
      />
      <StatTile
        className={TILE}
        label="Tokens"
        value={billed > 0 ? formatCount(billed).value : dash}
        sub={billed > 0 ? split : reached ? 'no usage reported' : 'nothing was sent upstream'}
      />
      <StatTile
        className={TILE}
        label="Cost"
        value={dollars(d.cost.total_usd)}
        sub={d.cost.total_usd > 0 ? `${nanoUSD(d.cost.total_usd)} nano-USD` : 'nothing billed'}
      />
    </div>
  )
}

function ErrorCard({ error }: { error: NonNullable<RequestDetail['error']> }) {
  return (
    <DetailCard title="Error" className={cn('gap-2.5 p-4', TILE, 'border-destructive/40')}>
      <DetailRows>
        {error.code && <DetailRow label="code">{error.code}</DetailRow>}
        {error.category && <DetailRow label="category">{error.category}</DetailRow>}
      </DetailRows>
      {error.message && <p className="text-xs leading-[18px]">{error.message}</p>}
    </DetailCard>
  )
}

function Chip({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span
      className={cn(
        'flex items-center gap-1.5 rounded-[2px] border px-2.5 py-1 text-[11px] font-medium',
        className,
      )}
    >
      {children}
    </span>
  )
}

function Missing({ id, back, error }: { id: string; back: string; error: Error | null }) {
  return (
    <>
      <PageHeader title={id || 'Request'} titleClassName="font-mono" />
      <PageBody>
        <Link
          to={back}
          className="flex w-fit items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-[13px]" />
          Requests
        </Link>
        <p className="rounded-[4px] border bg-card p-6 text-sm text-muted-foreground">
          {error && !isNotFound(error)
            ? `Could not load this request: ${error.message}`
            : 'This request was not found. It may not have been written yet, or it was removed when its retention ended.'}
        </p>
      </PageBody>
    </>
  )
}

// 08.2 · one request: what was asked, who served it, where the time went, what it cost, and the
// bodies when the admin asks for them.
export default function RequestPage() {
  const { id = '' } = useParams()
  const request = useRequestDetail(id)
  const names = useContextNames(request.data)
  const back = useBackPath()
  const { zone } = useTimezone()
  const [showPayload, setShowPayload] = useState(false)
  const payloadRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (showPayload) payloadRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [showPayload])

  const d = request.data
  if (request.isPending) {
    return (
      <>
        <PageHeader title="Request" />
        <PageBody>
          <Skeleton className="h-[102px] w-full" />
          <Skeleton className="h-72 w-full" />
        </PageBody>
      </>
    )
  }
  if (!d) return <Missing id={id} back={back} error={request.error} />

  const pill = statusPill(d)
  const hasPayload = d.payload_state !== 'none'

  return (
    <>
      <PageHeader
        title={d.id}
        titleClassName="font-mono text-base"
        icon={
          <span className="flex size-8 shrink-0 items-center justify-center rounded-[4px] border bg-card">
            <List className="size-[18px] text-muted-foreground" />
          </span>
        }
        badges={
          <>
            <Chip className={PILL_TONE[pill.tone]}>{pill.text}</Chip>
            {d.model.stream && (
              <Chip className="bg-foreground/5">
                <Waves className="size-3 text-muted-foreground" />
                Streamed
              </Chip>
            )}
          </>
        }
        actions={
          <Button
            variant="outline"
            disabled={!hasPayload}
            title={hasPayload ? undefined : 'No body was kept for this request'}
            onClick={() => {
              setShowPayload(!showPayload)
            }}
          >
            {showPayload ? <EyeOff /> : <Eye />}
            {showPayload ? 'Hide payload' : 'Show payload'}
          </Button>
        }
      />
      <PageBody>
        <div className="flex items-center gap-3 text-xs">
          <Link
            to={back}
            className="flex items-center gap-1.5 text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="size-[13px]" />
            Requests
          </Link>
          <span className="flex-1" />
          <span className="text-right font-mono text-muted-foreground">
            {formatInZone(new Date(d.started_at), zone, 'MMM dd, HH:mm:ss.SSS')} ·{' '}
            {duration(d.latency_ms)} total
            {d.request_id && ` · request id ${d.request_id}`}
          </span>
        </div>

        <Tiles d={d} />

        {/* The facts sit on the left from lg up; on a narrow screen they follow the pipeline. */}
        <div className="grid items-start gap-4 lg:grid-cols-[340px_minmax(0,1fr)]">
          <div className="flex min-w-0 flex-col gap-4 lg:order-2">
            {d.error && <ErrorCard error={d.error} />}
            {/* Each card is only as wide as its content needs; neither is stretched across the column. */}
            <div className="w-full max-w-[640px]">
              <PipelineCard detail={d} />
            </div>
            <div className="w-full max-w-[460px]">
              <CostCard detail={d} />
            </div>
            {showPayload && (
              <div ref={payloadRef} className="scroll-mt-20">
                <PayloadCard detail={d} />
              </div>
            )}
          </div>
          <div className="lg:order-1">
            <FactsCard detail={d} names={names} />
          </div>
        </div>
      </PageBody>
    </>
  )
}
