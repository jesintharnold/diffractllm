import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import { NO_FILTER, type RequestFilter, type StatusFilter } from '@/features/requests/api'

const STATUSES: StatusFilter[] = ['success', 'error', 'cancelled']
const SEARCH_DELAY_MS = 300

// The filters and the page live in the address, so Back from a request returns to the same list
// and a filtered view can be shared. Changing a filter goes back to page 1.
export function useRequestQuery() {
  const [params, setParams] = useSearchParams()

  const status = params.get('status') as StatusFilter
  const filter: RequestFilter = {
    q: params.get('q') ?? '',
    status: STATUSES.includes(status) ? status : '',
    provider: params.get('provider') ?? '',
    model: params.get('model') ?? '',
    client: params.get('client') ?? '',
  }
  const page = Math.max(1, Math.floor(Number(params.get('page'))) || 1)

  // Typing replaces the entry instead of adding one per keystroke.
  const write = (change: (next: URLSearchParams) => void) => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        change(next)
        return next
      },
      { replace: true },
    )
  }

  const update = (patch: Partial<RequestFilter>) => {
    write((next) => {
      for (const [key, value] of Object.entries(patch)) {
        if (value) next.set(key, value)
        else next.delete(key)
      }
      next.delete('page')
    })
  }

  // The search box shows what was typed at once and filters once the typing pauses.
  const [searchText, setSearchText] = useState(filter.q)
  const timer = useRef<number | undefined>(undefined)
  useEffect(
    () => () => {
      window.clearTimeout(timer.current)
    },
    [],
  )

  return {
    filter,
    page,
    update,
    searchText,
    setSearch: (text: string) => {
      setSearchText(text)
      window.clearTimeout(timer.current)
      timer.current = window.setTimeout(() => {
        update({ q: text.trim() })
      }, SEARCH_DELAY_MS)
    },
    setPage: (to: number) => {
      write((next) => {
        if (to > 1) next.set('page', String(to))
        else next.delete('page')
      })
    },
    clear: () => {
      window.clearTimeout(timer.current)
      setSearchText('')
      write((next) => {
        for (const key of Object.keys(NO_FILTER)) next.delete(key)
        next.delete('page')
      })
    },
    // The list's address, kept by the request page so Back returns to the same view.
    search: params.toString() ? `?${params.toString()}` : '',
  }
}
