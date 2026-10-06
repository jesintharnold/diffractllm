import { useCallback, useSyncExternalStore } from 'react'

// Set by refreshClock(): the exact moment of the last manual sync, used until the next tick.
let pinnedNow: number | null = null
const syncListeners = new Set<() => void>()

// Current time floored to `intervalMs`: stable between ticks, so it is safe inside a query key.
// Every component using the same interval sees the same value, which keeps queries aligned.
export function useClock(intervalMs: number): number {
  const subscribe = useCallback(
    (onTick: () => void) => {
      const timer = setInterval(() => {
        pinnedNow = null
        onTick()
      }, intervalMs)
      syncListeners.add(onTick)
      return () => {
        clearInterval(timer)
        syncListeners.delete(onTick)
      }
    },
    [intervalMs],
  )
  return useSyncExternalStore(
    subscribe,
    () => pinnedNow ?? Math.floor(Date.now() / intervalMs) * intervalMs,
  )
}

// Moves every clock to "now", unfloored. The sliding windows then end at this instant, so a
// request made a moment ago is inside them; the next tick returns to the floored time.
export function refreshClock(): void {
  pinnedNow = Date.now()
  for (const listener of syncListeners) listener()
}
