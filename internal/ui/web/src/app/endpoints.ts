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
    metrics: {
      stats: '/v1/admin/metrics/stats',
      requests: '/v1/admin/metrics/requests',
      requestSummary: '/v1/admin/metrics/requests/summary',
      request: (eventId: string) => `/v1/admin/metrics/requests/${id(eventId)}`,
      payload: (eventId: string) => `/v1/admin/metrics/requests/${id(eventId)}/payload`,
      topVirtualKeys: '/v1/admin/metrics/virtual-keys/top',
      budgetSpend: (budgetId: string) => `/v1/admin/metrics/budgets/${id(budgetId)}`,
    },
  },
} as const
