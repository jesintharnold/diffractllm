import { QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router'
import { queryClient } from '@/app/query-client'
import { router } from '@/app/router'
import './index.css'

const root = document.getElementById('root')
if (!root) throw new Error('index.html is missing #root')

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
