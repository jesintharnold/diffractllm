import { cn } from '@/lib/utils'

// The small rounded square that stands for a series or a provider: chart legends, tooltips and tables
// use the same one, so a colour reads the same wherever it appears.
export function Swatch({ color, className }: { color: string; className?: string }) {
  return (
    <span
      className={cn('size-2 shrink-0 rounded-[2px]', className)}
      style={{ background: color }}
      aria-hidden
    />
  )
}
