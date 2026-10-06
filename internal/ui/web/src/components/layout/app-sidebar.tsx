import { Aperture } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { Link, useLocation } from 'react-router'
import { NAV, type NavItem } from '@/app/nav'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/ui/sidebar'
import { GatewayStatus } from './gateway-status'

// "/" matches only itself; any other item also owns its detail pages (/governance/budgets/:id).
function isActive(pathname: string, to: string): boolean {
  return to === '/' ? pathname === '/' : pathname === to || pathname.startsWith(`${to}/`)
}

// Design: 14px label, 17px icon, label and icon in accent when selected. The selected background
// is a separate <span class="nav-indicator"> so a view transition can glide it between rows.
const ITEM_CLASS =
  'relative isolate h-auto gap-2.5 px-2.5 py-[9px] text-sm transition-colors duration-200 [&_svg]:size-[17px] data-active:bg-transparent data-active:font-semibold data-active:text-sidebar-primary'

function NavRow({
  item,
  active,
  onNavigate,
}: {
  item: NavItem
  active: boolean
  onNavigate: () => void
}) {
  const Icon = item.icon
  const ref = useRef<HTMLAnchorElement>(null)
  const firstRun = useRef(true)

  // On short screens the selected row can sit below the fold; bring it into view, only if needed.
  // Jump on page load, glide on later clicks (unless the user prefers reduced motion).
  useEffect(() => {
    const onLoad = firstRun.current
    firstRun.current = false
    if (!active) return
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    ref.current?.scrollIntoView({
      block: 'nearest',
      behavior: onLoad || reduced ? 'auto' : 'smooth',
    })
  }, [active])

  if (item.disabled) {
    return (
      <SidebarMenuButton className={ITEM_CLASS} aria-disabled title={`${item.title} — coming soon`}>
        <Icon />
        <span>{item.title}</span>
      </SidebarMenuButton>
    )
  }
  return (
    <SidebarMenuButton asChild isActive={active} className={ITEM_CLASS}>
      <Link
        ref={ref}
        to={item.to}
        viewTransition
        onClick={onNavigate}
        aria-current={active ? 'page' : undefined}
      >
        {active && (
          <span
            aria-hidden
            className="nav-indicator absolute inset-0 -z-10 rounded-[inherit] bg-nav-selected"
          />
        )}
        <Icon />
        <span>{item.title}</span>
      </Link>
    </SidebarMenuButton>
  )
}

export function AppSidebar() {
  const { pathname } = useLocation()
  const { setOpenMobile } = useSidebar()
  const closeOnMobile = () => {
    setOpenMobile(false)
  }

  return (
    <Sidebar>
      <SidebarHeader className="px-4 py-5">
        <Link to="/" viewTransition onClick={closeOnMobile} className="flex items-center gap-2.5">
          <Aperture className="size-[19px] text-sidebar-primary" />
          <span className="text-base font-semibold">DiffractLLM</span>
        </Link>
      </SidebarHeader>

      <SidebarContent className="gap-0">
        {NAV.map((section, index) => (
          <SidebarGroup key={section.title ?? index} className="py-0">
            {section.title && (
              <SidebarGroupLabel className="mt-2 h-auto px-1.5 pt-2.5 pb-1.5 text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">
                {section.title}
              </SidebarGroupLabel>
            )}
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {section.items.map((item) => (
                  <SidebarMenuItem key={item.to}>
                    <NavRow
                      item={item}
                      active={isActive(pathname, item.to)}
                      onNavigate={closeOnMobile}
                    />
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarFooter className="px-4 pt-4 pb-5">
        <GatewayStatus />
      </SidebarFooter>
    </Sidebar>
  )
}
