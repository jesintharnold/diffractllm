import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { DetailRow, DetailRows } from '@/components/detail'
import { KindTag } from '@/components/kind-tag'
import type { RequestDetail } from '@/features/requests/api'
import type { ContextNames, Named as NamedRef } from '@/features/requests/detail/context-names'
import { PILL_TONE, duration } from '@/features/requests/detail/format'
import { cn } from '@/lib/utils'

const dash = <span className="text-muted-foreground">—</span>

// Headers shown as their own rows are left out of the raw list.
const SHOWN_ELSEWHERE = new Set(['x-request-id', 'apim-request-id'])

// The key or budget by name, linked to where it is managed when it still exists.
function Named({ named }: { named: NamedRef | undefined }) {
  if (!named) return dash
  return named.to ? (
    <Link to={named.to} className="hover:underline">
      {named.label}
    </Link>
  ) : (
    named.label
  )
}

function Group({
  title,
  actions,
  children,
}: {
  title: string
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="flex flex-col gap-2.5">
      <header className="flex items-center gap-2.5">
        <h2 className="text-sm font-semibold">{title}</h2>
        {actions}
      </header>
      {children}
    </div>
  )
}

const Rule = () => <div className="h-px bg-border" />

// What the request was asked for and what served it. Every request shows the same rows; one the
// gateway turned away shows a dash where the provider would have answered.
function ModelGroup({ detail }: { detail: RequestDetail }) {
  const m = detail.model
  const rows: [label: string, value: ReactNode][] = [
    ['requested', m.requested ?? dash],
    ['served model', m.model ?? dash],
    ['upstream model', m.upstream ?? dash],
    ['request kind', detail.request_kind ? <KindTag kind={detail.request_kind} /> : dash],
    ['endpoint', detail.endpoint],
    ['finish reason', m.finish_reason ?? dash],
  ]
  return (
    <Group title="Model">
      <DetailRows>
        {rows.map(([label, value]) => (
          <DetailRow key={label} label={label}>
            {value}
          </DetailRow>
        ))}
      </DetailRows>
    </Group>
  )
}

// Who made the request and how the gateway routed it.
function GovernanceGroup({ detail, names }: { detail: RequestDetail; names: ContextNames }) {
  const c = detail.context
  return (
    <Group title="Governance">
      <DetailRows>
        <DetailRow label="virtual key">
          <Named named={names.key} />
        </DetailRow>
        <DetailRow label="client">{c.client_id ?? dash}</DetailRow>
        <DetailRow label="budget">
          <Named named={names.budget} />
        </DetailRow>
        <DetailRow label="credential">{c.credential ?? dash}</DetailRow>
        <DetailRow label="routing mode">{c.routing_mode ?? dash}</DetailRow>
      </DetailRows>
    </Group>
  )
}

function UpstreamGroup({ detail }: { detail: RequestDetail }) {
  const { upstream } = detail
  const status = upstream.http_status ?? 0
  const headers = Object.entries(upstream.headers ?? {})
    .filter(([name]) => !SHOWN_ELSEWHERE.has(name))
    .sort(([a], [b]) => a.localeCompare(b))
  const time = (ms: number | undefined) => (ms ? duration(ms) : dash)

  return (
    <Group
      title="Upstream response"
      actions={
        status > 0 && (
          <span
            className={cn(
              'ml-auto rounded-[2px] border px-2.5 py-1 text-[11px] font-medium',
              PILL_TONE[status < 400 ? 'good' : 'bad'],
            )}
          >
            {status}
          </span>
        )
      }
    >
      <DetailRows>
        <DetailRow label="attempts">{upstream.attempts ?? dash}</DetailRow>
        <DetailRow label="upstream request id">{upstream.request_id ?? dash}</DetailRow>
        <DetailRow label="time to first byte">{time(detail.ttfb_ms)}</DetailRow>
        <DetailRow label="time to first token">{time(detail.ttft_ms)}</DetailRow>
      </DetailRows>
      {headers.length > 0 && (
        <>
          <Rule />
          <DetailRows>
            {headers.map(([name, value]) => (
              <DetailRow key={name} label={<span className="font-mono text-[11px]">{name}</span>}>
                {value}
              </DetailRow>
            ))}
          </DetailRows>
        </>
      )}
    </Group>
  )
}

// Model, who made the request, and what the provider answered: the facts of one request in one
// section, the same rows every time.
export function FactsCard({ detail, names }: { detail: RequestDetail; names: ContextNames }) {
  return (
    <section className="flex flex-col gap-3 rounded-[4px] border bg-card p-4">
      <ModelGroup detail={detail} />
      <Rule />
      <GovernanceGroup detail={detail} names={names} />
      <Rule />
      <UpstreamGroup detail={detail} />
    </section>
  )
}
