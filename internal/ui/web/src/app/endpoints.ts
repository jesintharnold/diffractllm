// Every backend path the console calls, in one place. Mirrors internal/server/routes.go:
// when a route changes there, change it here and the compiler finds every caller.
// Hooks and components import from API; no URL string is written anywhere else.
const id = (value: string) => encodeURIComponent(value)

export const API = {
  ready: '/ready',
  info: '/v1/info',
  admin: {
    stats: '/v1/admin/stats',
    budgets: '/v1/admin/budgets',
    budget: (budgetId: string) => `/v1/admin/budgets/${id(budgetId)}`,
    virtualKeys: '/v1/admin/virtual-keys',
    virtualKey: (keyId: string) => `/v1/admin/virtual-keys/${id(keyId)}`,
    models: '/v1/admin/models',
    credentials: '/v1/admin/credentials',
    providers: {
      list: '/v1/admin/providers',
      settings: (name: string) => `/v1/admin/providers/${id(name)}/settings`,
      credentials: (name: string) => `/v1/admin/providers/${id(name)}/credentials`,
      credential: (name: string, credId: string) =>
        `/v1/admin/providers/${id(name)}/credentials/${id(credId)}`,
    },
    pricing: {
      overrides: '/v1/admin/pricing/custom',
      override: (overrideId: string) => `/v1/admin/pricing/custom/${id(overrideId)}`,
    },
    catalog: {
      summary: '/v1/admin/catalog/summary',
      settings: '/v1/admin/catalog/settings',
      models: '/v1/admin/models/catalog',
      sync: '/v1/admin/sync/catalog',
    },
    metrics: {
      stats: '/v1/admin/metrics/stats',
      requests: '/v1/admin/metrics/requests',
      requestSummary: '/v1/admin/metrics/requests/summary',
      request: (eventId: string) => `/v1/admin/metrics/requests/${id(eventId)}`,
      payload: (eventId: string) => `/v1/admin/metrics/requests/${id(eventId)}/payload`,
      topVirtualKeys: '/v1/admin/metrics/virtual-keys/top',
      providers: '/v1/admin/metrics/providers',
      credentials: '/v1/admin/metrics/credentials',
      budgetSpend: (budgetId: string) => `/v1/admin/metrics/budgets/${id(budgetId)}`,
    },
  },
} as const
