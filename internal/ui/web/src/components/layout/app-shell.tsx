import type { CSSProperties } from 'react'
import { Outlet } from 'react-router'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { TooltipProvider } from '@/components/ui/tooltip'
import { AppSidebar } from './app-sidebar'

// ADR-001 §7 shell: sidebar beside an inset exactly one screen tall. The inset scrolls only when a
// page's fixed parts don't fit; tables scroll inside their own card. 220px column (design),
// drawer below 1024px.
export function AppShell() {
  return (
    <TooltipProvider>
      <SidebarProvider style={{ '--sidebar-width': '13.75rem' } as CSSProperties}>
        <AppSidebar />
        <SidebarInset className="h-svh min-w-0 overflow-y-auto">
          <Outlet />
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}
