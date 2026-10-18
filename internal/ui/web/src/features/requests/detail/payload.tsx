import { useState } from 'react'
import { DetailCard } from '@/components/detail'
import { Skeleton } from '@/components/ui/skeleton'
import { isNotFound, useRequestPayload, type RequestDetail } from '@/features/requests/api'
import { cn } from '@/lib/utils'

type Side = 'request' | 'response'

const SIDES: { key: Side; label: string }[] = [
  { key: 'request', label: 'Request' },
  { key: 'response', label: 'Response' },
]

const size = (bytes: number) =>
  bytes < 1024 ? `${String(bytes)} B` : `${(bytes / 1024).toFixed(1)} KB`

// A body is JSON when the gateway could parse it, and the raw text when it could not.
const pretty = (body: unknown) => (typeof body === 'string' ? body : JSON.stringify(body, null, 2))

// What the client sent and what it got back. Fetched only once the admin asks for it.
export function PayloadCard({ detail }: { detail: RequestDetail }) {
  const payload = useRequestPayload(detail.id, true)
  const [side, setSide] = useState<Side>('request')

  const body = side === 'request' ? payload.data?.client_request : payload.data?.client_response
  const text = body === undefined || body === null ? undefined : pretty(body)

  return (
    <DetailCard
      title="Payload"
      className="gap-2.5 rounded-[4px] p-4"
      actions={
        <div
          role="tablist"
          className="ml-auto flex gap-0.5 rounded-md border bg-background p-[3px]"
        >
          {SIDES.map((s) => (
            <button
              key={s.key}
              type="button"
              role="tab"
              aria-selected={side === s.key}
              onClick={() => {
                setSide(s.key)
              }}
              className={cn(
                'rounded-[4px] px-3 py-[5px] text-[11px]',
                side === s.key
                  ? 'bg-card font-semibold'
                  : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {s.label}
            </button>
          ))}
        </div>
      }
    >
      {payload.isPending ? (
        <Skeleton className="h-40 w-full" />
      ) : payload.isError ? (
        <p className="text-xs text-muted-foreground">
          {isNotFound(payload.error)
            ? 'No body was kept for this request.'
            : `Could not load the payload: ${payload.error.message}`}
        </p>
      ) : (
        <>
          <pre className="max-h-[420px] overflow-auto rounded-md border bg-background p-3.5 font-mono text-xs leading-[18px] whitespace-pre-wrap [overflow-wrap:anywhere] [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin]">
            {text ?? `No ${side} body was kept.`}
          </pre>
          <p className="text-[11px] text-muted-foreground">
            Client {side}
            {text !== undefined && ` · ${size(new TextEncoder().encode(text).length)}`}
            {detail.payload_state === 'truncated' && ' · cut at the size cap'}
          </p>
        </>
      )}
    </DetailCard>
  )
}
