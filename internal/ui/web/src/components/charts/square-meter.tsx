import { cn } from '@/lib/utils'

// A row of small squares that fills left to right. `fill` is the Tailwind background of a lit square
// (a budget colours it by how close it is to its limit); `spread` stretches the row to its container
// instead of packing the squares with a fixed gap.
export function SquareMeter({
  ratio,
  fill,
  label,
  cells = 20,
  size = 8,
  gap = 3,
  spread,
  className,
}: {
  ratio: number
  fill: string
  label: string
  cells?: number
  size?: number
  gap?: number
  spread?: boolean
  className?: string
}) {
  const filled = ratio > 0 ? Math.max(1, Math.round(Math.min(ratio, 1) * cells)) : 0
  return (
    <div
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(Math.min(ratio, 1) * 100)}
      className={cn('flex', spread && 'w-full justify-between', className)}
      style={spread ? undefined : { gap }}
    >
      {Array.from({ length: cells }, (_, i) => (
        <span
          key={i}
          style={{ width: size, height: size }}
          className={cn(
            'shrink-0 rounded-[2px] transition-colors',
            i < filled ? fill : 'bg-border',
          )}
        />
      ))}
    </div>
  )
}
