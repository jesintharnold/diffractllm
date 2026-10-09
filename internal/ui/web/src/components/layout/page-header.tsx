import type { ReactNode } from 'react'
import { SidebarTrigger } from '@/components/ui/sidebar'

// The header every screen shares: a small semibold title, actions on the right, no border.
// An optional icon (a provider's logo) sits before the title.
// Below 1024px the sidebar is a drawer, so the header carries its menu button.
export function PageHeader({
  title,
  icon,
  actions,
}: {
  title: string
  icon?: ReactNode
  actions?: ReactNode
}) {
  return (
    <header className="sticky top-0 z-10 flex flex-wrap items-center gap-3 bg-background px-4 pt-4 pb-1 md:px-5 md:pt-[18px]">
      <SidebarTrigger className="-ml-1 lg:hidden" />
      {icon}
      <h1 className="min-w-0 flex-1 truncate text-lg font-semibold opacity-90">{title}</h1>
      {actions && <div className="flex w-full items-center gap-2 sm:w-auto">{actions}</div>}
    </header>
  )
}
