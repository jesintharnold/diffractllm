import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

// The parts every detail page is built from: a stat tile, a titled card, a label/value row.
// Radius and padding come from the page that uses them, so a screen can be squarer than the rest.
export function StatTile({
  label,
  value,
  sub,
  valueClass,
  className,
}: {
  label: string
  value: ReactNode
  sub: ReactNode
  valueClass?: string
  className?: string
}) {
  return (
    <div
      className={cn('flex flex-col gap-1.5 rounded-xl border bg-card px-[18px] py-4', className)}
    >
      <span className="text-[10px] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
        {label}
      </span>
      <span className={cn('font-mono text-2xl leading-8 font-medium tabular-nums', valueClass)}>
        {value}
      </span>
      <span className="text-[11px] text-muted-foreground">{sub}</span>
    </div>
  )
}

export function DetailCard({
  title,
  actions,
  children,
  className,
}: {
  title: string
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={cn('flex flex-col gap-3.5 rounded-xl border bg-card p-5', className)}>
      {actions ? (
        <header className="flex items-center gap-2.5">
          <h2 className="text-sm font-semibold">{title}</h2>
          {actions}
        </header>
      ) : (
        <h2 className="text-sm font-semibold">{title}</h2>
      )}
      {children}
    </section>
  )
}

// One line per row: a value too long for the card is cut short, and a plain-text one shows in full
// on hover.
export function DetailRow({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 text-xs">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span
        className="min-w-0 truncate text-right font-mono"
        title={typeof children === 'string' ? children : undefined}
      >
        {children}
      </span>
    </div>
  )
}

// A tight stack of rows for a card of facts.
export function DetailRows({ children }: { children: ReactNode }) {
  return <div className="flex flex-col gap-1.5">{children}</div>
}
