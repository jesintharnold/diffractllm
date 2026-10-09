import { useLayoutEffect, useRef, useState } from 'react'

// A button dressed as the Input component, so pickers sit flush with the text fields.
export const inputLike =
  'flex h-9 w-full min-w-0 items-center gap-2 rounded-lg border border-input bg-transparent px-2.5 text-sm transition-colors outline-none hover:border-ring/60 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 data-[state=open]:border-ring dark:bg-input/30'

// One underline that slides between tabs instead of jumping (as on the Catalog page).
export function useTabIndicator(active: string) {
  const list = useRef<HTMLDivElement>(null)
  const [box, setBox] = useState({ left: 0, width: 0 })
  useLayoutEffect(() => {
    const el = list.current?.querySelector<HTMLElement>(`[data-value="${active}"]`)
    if (el) setBox({ left: el.offsetLeft, width: el.offsetWidth })
  }, [active])
  return { list, box }
}
