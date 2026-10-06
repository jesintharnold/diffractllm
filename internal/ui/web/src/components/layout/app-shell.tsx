import type { CSSProperties } from 'react'
import { Outlet } from 'react-router'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { TooltipProvider } from '@/components/ui/tooltip'
import { AppSidebar } from './app-sidebar'

// ADR-001 §7 shell: sidebar beside a scrolling inset. 220px column (design), drawer below 768px.
export function AppShell() {
  return (
    <TooltipProvider>
      <SidebarProvider style={{ '--sidebar-width': '13.75rem' } as CSSProperties}>
        <AppSidebar />
        <SidebarInset className="min-w-0">
          <Outlet />
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}
