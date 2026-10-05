import {
  Activity,
  Bell,
  Boxes,
  CircleDollarSign,
  GitBranch,
  KeyRound,
  KeySquare,
  LayoutDashboard,
  List,
  Settings,
  SlidersHorizontal,
  Wallet,
  type LucideIcon,
} from 'lucide-react'

// One source for the sidebar and the router (ADR-001 §7). Disabled items are shown, not routed.
export interface NavItem {
  title: string
  to: string
  icon: LucideIcon
  disabled?: boolean
}

export interface NavSection {
  title?: string
  items: NavItem[]
}

export const NAV: NavSection[] = [
  { items: [{ title: 'Overview', to: '/', icon: LayoutDashboard }] },
  {
    title: 'Models',
    items: [
      { title: 'Catalog', to: '/models/catalog', icon: Boxes },
      { title: 'Pricing', to: '/models/pricing', icon: CircleDollarSign },
      { title: 'API keys', to: '/models/api-keys', icon: KeySquare },
    ],
  },
  {
    title: 'Governance',
    items: [
      { title: 'Virtual keys', to: '/governance/virtual-keys', icon: KeyRound },
      { title: 'Budgets', to: '/governance/budgets', icon: Wallet },
      { title: 'Settings', to: '/governance/settings', icon: SlidersHorizontal },
    ],
  },
  {
    title: 'Observability',
    items: [
      { title: 'Requests', to: '/observability/requests', icon: List },
      { title: 'Traces', to: '/observability/traces', icon: GitBranch, disabled: true },
      { title: 'Metrics', to: '/observability/metrics', icon: Activity, disabled: true },
      { title: 'Alerts', to: '/observability/alerts', icon: Bell, disabled: true },
      { title: 'Settings', to: '/observability/settings', icon: SlidersHorizontal },
    ],
  },
  { title: 'Settings', items: [{ title: 'Gateway', to: '/settings/gateway', icon: Settings }] },
]
