import { Timer, Zap } from 'lucide-react'
import type { ReactNode } from 'react'
import { DetailCard } from '@/components/detail'
import type { RequestDetail, Stage } from '@/features/requests/api'
import { duration } from '@/features/requests/detail/format'
import { cn } from '@/lib/utils'

// The order the gateway marks a request's stages in. `first_token` is more waiting on the
// provider, so the card counts it with `upstream`; `response` is whatever follows the last mark.
const ORDER = [
  'auth',
  'read_body',
  'parse',
  'admission',
  'routing',
  'upstream',
  'first_token',
  'response',
]

// The rows every request shows, so two requests always read the same way.
const ROWS: { stage: string; label: string }[] = [
  { stage: 'auth', label: 'authenticate' },
  { stage: 'read_body', label: 'read body' },
  { stage: 'parse', label: 'parse' },
  { stage: 'admission', label: 'admission' },
  { stage: 'routing', label: 'routing' },
  { stage: 'upstream', label: 'upstream' },
  { stage: 'response', label: 'response' },
]

// Stages the gateway itself spends time in; the rest is the provider and the reply.
const GATEWAY_STAGES = new Set(['auth', 'read_body', 'parse', 'admission', 'routing'])

interface Row {
  label: string
  us: number | null // null: the request never got this far
}

// A stage that has no mark but sits before one that does ran too, in less time than the clock can
// tell. One that comes after the last mark did not run.
function rowsOf(stages: Stage[]): Row[] {
  const byName = new Map(stages.map((s) => [s.name, s]))
  const lastRan = ORDER.reduce((last, name, i) => (byName.has(name) ? i : last), -1)
  return ROWS.map(({ stage, label }) => {
    if (ORDER.indexOf(stage) > lastRan) return { label, us: null }
    const own = byName.get(stage)?.duration_us ?? 0
    const firstToken = stage === 'upstream' ? (byName.get('first_token')?.duration_us ?? 0) : 0
    return { label, us: own + firstToken }
  })
}

// One colour for the whole waterfall: green for a good response, red for an error, grey for a
// request the client cancelled.
function barTone(result: string): string {
  if (result === 'ok') return 'bg-success'
  if (result === 'client_abort') return 'bg-muted-foreground'
  return 'bg-destructive'
}

// Each bar starts where the one before it ended, so the bars add up to the whole request. A stage
// too short to see is drawn at least MIN_SHARE wide to keep the staircase readable; the time
// beside every bar is exact.
const MIN_SHARE = 0.015

function layout(durations: number[]): { left: number; width: number }[] {
  const total = durations.reduce((sum, d) => sum + d, 0) || 1
  const shares = durations.map((d) => Math.max(d / total, MIN_SHARE))
  const whole = shares.reduce((sum, share) => sum + share, 0)
  let used = 0
  return shares.map((share) => {
    const bar = { left: (used / whole) * 100, width: (share / whole) * 100 }
    used += share
    return bar
  })
}

function Chip({ icon, children }: { icon: ReactNode; children: string }) {
  return (
    <span className="flex items-center gap-1.5 rounded-[2px] border bg-foreground/5 px-2.5 py-1 text-[11px] font-medium">
      <span className="text-muted-foreground [&_svg]:size-3">{icon}</span>
      {children}
    </span>
  )
}

export function PipelineCard({ detail }: { detail: RequestDetail }) {
  const stages = detail.pipeline ?? []
  const rows = rowsOf(stages)
  const bars = layout(rows.flatMap((r) => (r.us === null ? [] : [r.us])))
  const tone = barTone(detail.result)

  const overheadMs =
    stages.filter((s) => GATEWAY_STAGES.has(s.name)).reduce((sum, s) => sum + s.duration_us, 0) /
    1000
  const overhead = stages.length > 0 ? duration(overheadMs) : '—'
  const firstToken =
    (detail.ttft_ms ?? 0) > 0 ? `${String(Math.round(detail.ttft_ms ?? 0))}ms` : '—'

  let drawn = 0
  return (
    <DetailCard
      title="Pipeline"
      className="gap-2 rounded-[4px] p-4"
      actions={
        <div className="ml-auto flex items-center gap-2">
          <Chip icon={<Timer />}>{`gateway overhead ${overhead}`}</Chip>
          <Chip icon={<Zap />}>{`first token ${firstToken}`}</Chip>
        </div>
      }
    >
      <ol className="flex flex-col gap-2">
        {rows.map((row) => {
          const bar = row.us === null ? undefined : bars[drawn++]
          return (
            <li
              key={row.label}
              className="grid grid-cols-[100px_minmax(0,1fr)_64px] items-center gap-3"
            >
              <span className="text-[11px] font-medium tracking-[0.06em] text-muted-foreground uppercase">
                {row.label}
              </span>
              <span className="relative h-[22px]">
                <span className="absolute inset-x-0 top-2.5 h-0.5 bg-border" />
                {bar && (
                  <span
                    className={cn('absolute top-1 h-3.5', tone)}
                    style={{ left: `${String(bar.left)}%`, width: `${String(bar.width)}%` }}
                  />
                )}
              </span>
              <span
                className={cn(
                  'text-right font-mono text-xs',
                  row.us === null && 'text-muted-foreground',
                )}
              >
                {row.us === null ? '—' : duration(row.us / 1000)}
              </span>
            </li>
          )
        })}
      </ol>
    </DetailCard>
  )
}
