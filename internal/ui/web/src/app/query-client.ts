import { QueryClient } from '@tanstack/react-query'
import { ApiError } from '@/lib/api'

// 4xx means the request itself is wrong (bad range, unknown id): retrying cannot fix it.
function shouldRetry(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError && error.status < 500) return false
  return failureCount < 2
}

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: shouldRetry,
      refetchOnWindowFocus: true,
    },
  },
})
