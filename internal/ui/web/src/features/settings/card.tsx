import type { ReactNode } from 'react'

// One card of a settings page (designs 07 and 09): a title and a line of explanation, optional
// actions on the right, then the fields.
export function SettingsCard({
  title,
  detail,
  actions,
  children,
}: {
  title: string
  detail?: string
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-3.5 rounded-xl border bg-card p-[18px]">
      <header className="flex items-start gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <h2 className="text-sm font-semibold">{title}</h2>
          {detail && <p className="text-[11px] leading-snug text-muted-foreground">{detail}</p>}
        </div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </header>
      {children}
    </section>
  )
}

export const FieldRow = ({ children }: { children: ReactNode }) => (
  <div className="flex gap-3">{children}</div>
)
