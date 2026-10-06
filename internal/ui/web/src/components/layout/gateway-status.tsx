import { useGatewayInfo, useReadiness, type GatewayInfo } from '@/features/system/api'
import { cn } from '@/lib/utils'

// Shown until /v1/info answers (or when the gateway is unreachable), so the footer never sits empty.
const FALLBACK_INFO: GatewayInfo = { version: '0.0.0', license: 'free' }

// Sidebar footer: is the gateway serving, which build, which edition.
export function GatewayStatus() {
  const ready = useReadiness()
  const info = useGatewayInfo().data ?? FALLBACK_INFO

  const state = ready.isPending
    ? { label: 'Checking…', tone: 'text-muted-foreground', dot: 'bg-muted-foreground' }
    : ready.data?.status === 'ready'
      ? { label: 'Ready', tone: 'text-success', dot: 'bg-success' }
      : { label: 'Not ready', tone: 'text-destructive', dot: 'bg-destructive' }

  const commercial = info.license === 'commercial'

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <span
          className={cn('flex items-center gap-1.5 text-[13px] font-medium', state.tone)}
          aria-live="polite"
        >
          <span className={cn('size-1.5 rounded-full', state.dot)} aria-hidden />
          {state.label}
        </span>
        <span className="ml-auto font-mono text-xs text-muted-foreground">v{info.version}</span>
      </div>
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        License
        <span
          className={cn(
            'rounded-sm border px-[7px] py-0.5 text-[11px] leading-none font-semibold',
            commercial
              ? 'border-tint-openai-edge bg-tint-openai text-primary'
              : 'border-tint-neutral-edge bg-tint-neutral text-foreground',
          )}
        >
          {commercial ? 'Commercial' : 'Free'}
        </span>
      </div>
    </div>
  )
}
