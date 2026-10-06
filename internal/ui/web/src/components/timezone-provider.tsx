import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { BROWSER_ZONE, TimezoneContext } from '@/lib/timezone'

// Holds the display timezone and saves it in the browser so it sticks between visits.
const STORAGE_KEY = 'diffract.timezone'

function readStored(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? BROWSER_ZONE
  } catch {
    return BROWSER_ZONE
  }
}

export function TimezoneProvider({ children }: { children: ReactNode }) {
  const [zone, setZoneState] = useState(readStored)
  const setZone = useCallback((next: string) => {
    setZoneState(next)
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // Storage can be blocked (private mode); the choice still holds for this session.
    }
  }, [])
  const value = useMemo(() => ({ zone, setZone }), [zone, setZone])
  return <TimezoneContext value={value}>{children}</TimezoneContext>
}
