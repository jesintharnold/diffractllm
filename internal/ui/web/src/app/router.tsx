import { createBrowserRouter } from 'react-router'
import { AppShell } from '@/components/layout/app-shell'
import BudgetPage from '@/pages/budget'
import BudgetsPage from '@/pages/budgets'
import CatalogPage from '@/pages/catalog'
import ComingSoonPage from '@/pages/coming-soon'
import GovernanceSettingsPage from '@/pages/governance-settings'
import NotFoundPage from '@/pages/not-found'
import ObservabilitySettingsPage from '@/pages/observability-settings'
import OverviewPage from '@/pages/overview'
import PricingPage from '@/pages/pricing'
import ProviderPage from '@/pages/provider'
import ProvidersPage from '@/pages/providers'
import RequestPage from '@/pages/request'
import RequestsPage from '@/pages/requests'
import VirtualKeyPage from '@/pages/virtual-key'
import VirtualKeysPage from '@/pages/virtual-keys'

// Routes per ADR-001 §7. Screens not built yet render ComingSoonPage under their real path.
const later = (path: string, title: string) => ({ path, element: <ComingSoonPage title={title} /> })

export const router = createBrowserRouter([
  {
    element: <AppShell />,
    children: [
      { index: true, element: <OverviewPage /> },
      { path: 'models/catalog', element: <CatalogPage /> },
      { path: 'models/pricing', element: <PricingPage /> },
      { path: 'models/api-keys', element: <ProvidersPage /> },
      { path: 'models/api-keys/:provider', element: <ProviderPage /> },
      { path: 'governance/virtual-keys', element: <VirtualKeysPage /> },
      { path: 'governance/virtual-keys/:id', element: <VirtualKeyPage /> },
      { path: 'governance/budgets', element: <BudgetsPage /> },
      { path: 'governance/budgets/:id', element: <BudgetPage /> },
      { path: 'governance/settings', element: <GovernanceSettingsPage /> },
      { path: 'observability/requests', element: <RequestsPage /> },
      { path: 'observability/requests/:id', element: <RequestPage /> },
      { path: 'observability/settings', element: <ObservabilitySettingsPage /> },
      later('settings/gateway', 'Gateway settings'),
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])
