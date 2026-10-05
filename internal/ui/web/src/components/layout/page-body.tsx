import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

// Content area under PageHeader: 20px sides (16px on phones), 12px below the header, 16px bottom.
export function PageBody({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div className={cn('flex flex-col gap-4 px-4 pt-3 pb-4 md:px-5', className)}>{children}</div>
  )
}
