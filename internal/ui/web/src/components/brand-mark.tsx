import { cn } from '@/lib/utils'

// The 16 px master of the DiffractLLM mark (brand/svg/symbol-16-*.svg), coloured by --brand.
export function BrandMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 2 16 12"
      shapeRendering="crispEdges"
      aria-hidden="true"
      className={cn('text-brand', className)}
      fill="currentColor"
    >
      <path d="M0 7h1v1h-1zM0 8h1v1h-1zM2 7h1v1h-1zM2 8h1v1h-1zM4 7h1v1h-1zM4 8h1v1h-1zM5 7h1v1h-1zM5 8h1v1h-1zM6 7h1v1h-1zM6 8h1v1h-1zM7 7h1v1h-1zM7 8h1v1h-1zM8 7h1v1h-1zM8 8h1v1h-1zM9 7h1v1h-1zM9 8h1v1h-1zM10 7h1v1h-1zM10 8h1v1h-1zM11 7h1v1h-1zM11 8h1v1h-1zM12 7h1v1h-1zM12 8h1v1h-1zM13 7h1v1h-1zM13 8h1v1h-1zM14 7h1v1h-1zM14 8h1v1h-1zM15 7h1v1h-1zM15 8h1v1h-1zM8 6h1v1h-1zM8 9h1v1h-1zM9 6h1v1h-1zM9 9h1v1h-1zM10 6h1v1h-1zM10 9h1v1h-1zM9 5h1v1h-1zM9 10h1v1h-1zM10 5h1v1h-1zM10 10h1v1h-1zM11 5h1v1h-1zM11 10h1v1h-1zM10 4h1v1h-1zM10 11h1v1h-1zM11 4h1v1h-1zM11 11h1v1h-1zM12 4h1v1h-1zM12 11h1v1h-1zM11 3h1v1h-1zM11 12h1v1h-1zM12 3h1v1h-1zM12 12h1v1h-1zM13 3h1v1h-1zM13 12h1v1h-1zM12 2h1v1h-1zM12 13h1v1h-1zM13 2h1v1h-1zM13 13h1v1h-1zM14 2h1v1h-1zM14 13h1v1h-1z" />
    </svg>
  )
}
