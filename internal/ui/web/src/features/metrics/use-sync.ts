import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { HAS_TRAFFIC_KEY, RECENT_KEY } from '@/features/metrics/api'
import { refreshClock } from '@/lib/clock'
import { useTimeRange } from '@/lib/time-range'

// A local sync answers in a few milliseconds, too fast to see: the icon spins at least this long.
const MIN_SPIN_MS = 800

// The page's Sync button: refetch every metrics query (tiles, chart, recent requests) at once.
export function useSync() {
  const queryClient = useQueryClient()
  const [spec] = useTimeRange()
  const fetching = useIsFetching({ queryKey: ['metrics'] }) > 0
  const [clicked, setClicked] = useState(false)

  useEffect(() => {
    if (!clicked) return
    const timer = setTimeout(() => {
      setClicked(false)
    }, MIN_SPIN_MS)
    return () => {
      clearTimeout(timer)
    }
  }, [clicked])

  const sync = () => {
    setClicked(true)
    if (spec.kind === 'preset') {
      // Moves "now" forward: the window changes, so tiles and chart refetch under a new key.
      // Recent requests and the traffic check have fixed keys, so they are refetched directly.
      refreshClock()
      void queryClient.invalidateQueries({ queryKey: HAS_TRAFFIC_KEY })
      void queryClient.invalidateQueries({ queryKey: RECENT_KEY })
    } else {
      // A custom range is fixed, so refetch the same keys.
      void queryClient.invalidateQueries({ queryKey: ['metrics'] })
    }
  }

  return { sync, syncing: clicked || fetching }
}
