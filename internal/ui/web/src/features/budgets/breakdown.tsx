import { ProviderLogo } from '@/components/provider-logo'
import { Skeleton } from '@/components/ui/skeleton'
import type { SpendRow } from '@/features/budgets/api'
import { SquareMeter } from '@/components/charts/square-meter'
import { aggregate } from '@/features/budgets/spend'
import { formatCount, formatUSD } from '@/lib/format'
import { providerLabel } from '@/lib/provider-colors'

const head = 'text-[10px] font-semibold tracking-[0.06em] text-muted-foreground uppercase'

// What the spend went on: one row per provider and model, with its share as small squares.
export function SpendBreakdown({
  rows,
  caption,
  loading,
}: {
  rows: SpendRow[]
  caption: string
  loading?: boolean
}) {
  const models = aggregate(rows)
  const total = models.reduce((sum, m) => sum + m.spend_usd, 0)

  return (
    <section className="overflow-hidden rounded-xl border bg-card">
      <header className="flex items-center gap-3 px-5 pt-4 pb-3">
        <h2 className="flex-1 text-sm font-semibold">Spend by provider and model</h2>
        <span className="text-[11px] text-muted-foreground">{caption}</span>
      </header>
      <div className="flex items-center gap-3 bg-background px-5 py-2.5">
        <span className={`${head} flex-1`}>Provider / model</span>
        <span className={`${head} w-24 text-right`}>Requests</span>
        <span className={`${head} w-28 text-right`}>Spend</span>
        <span className={`${head} w-48 pl-5`}>Share</span>
      </div>
      {loading ? (
        <div className="px-5 py-4">
          <Skeleton className="h-24 w-full" />
        </div>
      ) : models.length === 0 ? (
        <p className="border-t px-5 py-8 text-center text-sm text-muted-foreground">
          No spend in this range.
        </p>
      ) : (
        <div className="max-h-96 overflow-y-auto [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin]">
          {models.map((m) => {
            const share = total > 0 ? m.spend_usd / total : 0
            return (
              <div
                key={`${m.provider}/${m.model}`}
                className="flex items-center gap-3 border-t px-5 py-3 text-xs"
              >
                <span className="flex min-w-0 flex-1 items-center gap-2">
                  <ProviderLogo provider={m.provider} className="size-4" />
                  <span className="shrink-0 text-muted-foreground">
                    {providerLabel(m.provider)} /
                  </span>
                  <span className="truncate font-mono">{m.model || 'unknown'}</span>
                </span>
                <span className="w-24 text-right font-mono text-muted-foreground tabular-nums">
                  {formatCount(m.requests).value}
                </span>
                <span className="w-28 text-right font-mono tabular-nums">
                  {formatUSD(m.spend_usd).value}
                </span>
                <span className="flex w-48 items-center gap-2 pl-5">
                  <SquareMeter
                    ratio={share}
                    cells={12}
                    fill="bg-bar"
                    label={`${providerLabel(m.provider)} ${m.model} share of spend`}
                  />
                  <span className="font-mono text-[11px] text-muted-foreground tabular-nums">
                    {Math.round(share * 100)}%
                  </span>
                </span>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
