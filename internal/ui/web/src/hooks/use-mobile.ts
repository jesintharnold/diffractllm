import { useSyncExternalStore } from 'react'

// Matches the sidebar's breakpoint: below 768px the sidebar is a drawer.
const QUERY = '(max-width: 767px)'

function subscribe(onChange: () => void) {
  const mql = window.matchMedia(QUERY)
  mql.addEventListener('change', onChange)
  return () => {
    mql.removeEventListener('change', onChange)
  }
}

// Read straight from matchMedia: correct on the first render, so mobile never flashes the desktop layout.
export function useIsMobile(): boolean {
  return useSyncExternalStore(subscribe, () => window.matchMedia(QUERY).matches)
}
