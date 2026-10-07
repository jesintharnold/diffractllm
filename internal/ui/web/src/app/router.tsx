import { createBrowserRouter } from 'react-router'
import { AppShell } from '@/components/layout/app-shell'
import CatalogPage from '@/pages/catalog'
import ComingSoonPage from '@/pages/coming-soon'
import NotFoundPage from '@/pages/not-found'
import OverviewPage from '@/pages/overview'

// Routes per ADR-001 §7. Screens not built yet render ComingSoonPage under their real path.
const later = (path: string, title: string) => ({ path, element: <ComingSoonPage title={title} /> })

export const router = createBrowserRouter([
  {
    element: <AppShell />,
    children: [
      { index: true, element: <OverviewPage /> },
      { path: 'models/catalog', element: <CatalogPage /> },
      later('models/pricing', 'Pricing'),
      later('models/api-keys', 'API keys'),
      later('models/api-keys/:provider', 'Provider'),
      later('governance/virtual-keys', 'Virtual keys'),
      later('governance/virtual-keys/:id', 'Virtual key'),
      later('governance/budgets', 'Budgets'),
      later('governance/budgets/:id', 'Budget'),
      later('governance/settings', 'Governance settings'),
      later('observability/requests', 'Requests'),
      later('observability/requests/:id', 'Request'),
      later('observability/settings', 'Observability settings'),
      later('settings/gateway', 'Gateway settings'),
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])
