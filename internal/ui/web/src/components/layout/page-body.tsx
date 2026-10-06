import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

// Content area under PageHeader: 20px sides (16px on phones), 12px below the header, 16px bottom.
// It fills the screen, so a card with `flex-1` takes the leftover height.
export function PageBody({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div className={cn('flex flex-1 flex-col gap-4 px-4 pt-3 pb-4 md:px-5', className)}>
      {children}
    </div>
  )
}
