import { TZDate } from '@date-fns/tz'
import { format, isValid, parse } from 'date-fns'
import { createContext, useContext } from 'react'

// Display timezone. The server only ever sees UTC; the zone decides how times are shown and
// how custom From/To inputs are read. Conversion is date-fns + @date-fns/tz; the provider lives
// in components/timezone-provider.tsx.
export const BROWSER_ZONE = Intl.DateTimeFormat().resolvedOptions().timeZone

export interface TimezoneState {
  zone: string
  setZone: (zone: string) => void
}

export const TimezoneContext = createContext<TimezoneState | null>(null)

export function useTimezone(): TimezoneState {
  const state = useContext(TimezoneContext)
  if (!state) throw new Error('useTimezone needs <TimezoneProvider>')
  return state
}

export function listTimeZones(): string[] {
  const zones = Intl.supportedValuesOf('timeZone')
  return zones.includes('UTC') ? zones : ['UTC', ...zones]
}

// "Asia/Kolkata (UTC+05:30)"
export function zoneLabel(zone: string, now: Date = new Date()): string {
  return `${zone} (UTC${format(new TZDate(now, zone), 'xxx')})`
}

// "Oct 4, 14:00", shown in `zone`
export function formatInZone(instant: Date, zone: string, pattern = 'MMM d, HH:mm'): string {
  return format(new TZDate(instant, zone), pattern)
}

export type Meridiem = 'AM' | 'PM'

// A calendar day (as the date picker returns it) plus 24-hour "HH:mm:ss", read as wall time in `zone`.
export function zonedToUtc(day: Date, time: string, zone: string): Date {
  const [hours = 0, minutes = 0, seconds = 0] = time.split(':').map(Number)
  return new Date(
    new TZDate(
      day.getFullYear(),
      day.getMonth(),
      day.getDate(),
      hours,
      minutes,
      seconds,
      zone,
    ).getTime(),
  )
}

// The calendar day and 12-hour time ("09:00:00" + "AM") that `instant` shows as in `zone`.
export function utcToZoned(
  instant: Date,
  zone: string,
): { day: Date; time: string; meridiem: Meridiem } {
  const local = new TZDate(instant, zone)
  return {
    day: new Date(local.getFullYear(), local.getMonth(), local.getDate()),
    time: format(local, 'hh:mm:ss'),
    meridiem: format(local, 'a') === 'PM' ? 'PM' : 'AM',
  }
}

// "06:30:00" + "PM" → "18:30:00"; null when the typed time is not a valid hh:mm:ss.
export function to24Hour(time: string, meridiem: Meridiem): string | null {
  if (!/^\d{1,2}:\d{2}:\d{2}$/.test(time)) return null
  const parsed = parse(`${time} ${meridiem}`, 'hh:mm:ss a', new Date(2000, 0, 1))
  return isValid(parsed) ? format(parsed, 'HH:mm:ss') : null
}
