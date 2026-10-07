// Classes for the console's right-hand drawers (design 02.3 / 03.3): 12px off every edge,
// a light backdrop and an eased slide. Pass to SheetContent; widths stay literal for Tailwind.
const base =
  'gap-0 overflow-hidden rounded-lg border bg-card data-[side=right]:inset-y-3 data-[side=right]:right-3 data-[side=right]:h-auto data-[side=right]:max-w-[calc(100vw-1.5rem)] data-[side=right]:border data-open:duration-300 data-open:ease-[cubic-bezier(0.22,1,0.36,1)] data-[side=right]:data-open:slide-in-from-right-12'

export const floatingSheet = `${base} data-[side=right]:w-[480px] data-[side=right]:sm:max-w-[480px]`

// Forms with two-column fields (pricing override, 03.3).
export const floatingSheetWide = `${base} data-[side=right]:w-[560px] data-[side=right]:sm:max-w-[560px]`

export const floatingSheetOverlay = 'bg-scrim-soft supports-backdrop-filter:backdrop-blur-none'
