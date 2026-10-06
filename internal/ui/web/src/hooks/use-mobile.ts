import { useSyncExternalStore } from 'react'

function useMedia(query: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const mql = window.matchMedia(query)
      mql.addEventListener('change', onChange)
      return () => {
        mql.removeEventListener('change', onChange)
      }
    },
    () => window.matchMedia(query).matches,
  )
}

// Phone layout (below 768px): single-month calendar, stacked picker panels.
// Read straight from matchMedia: correct on the first render, so mobile never flashes the desktop layout.
export function useIsMobile(): boolean {
  return useMedia('(max-width: 767px)')
}

// Below 1024px the sidebar is a drawer, so the page gets the full width before content wraps.
// Keep in step with the lg: classes in components/ui/sidebar.tsx.
export function useSidebarIsDrawer(): boolean {
  return useMedia('(max-width: 1023px)')
}
