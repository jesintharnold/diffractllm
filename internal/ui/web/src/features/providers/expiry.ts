import { differenceInCalendarDays, differenceInMinutes, format, isSameYear, set } from 'date-fns'

export const EXPIRY_PRESET_HOURS = [1, 6, 12, 24]
export const DAY_END = '23:59'

export const isHHmm = (s: string) => /^([01]\d|2[0-3]):[0-5]\d$/.test(s)
export const timeOf = (d: Date) => format(d, 'HH:mm')
export const isDayEnd = (d: Date) => timeOf(d) === DAY_END

// "HH:mm" on a day; 23:59 means the end of that day, so its last minute still counts.
export function atTime(day: Date, hhmm: string): Date {
  const [hours = 0, minutes = 0] = hhmm.split(':').map(Number)
  return set(day, { hours, minutes, seconds: hhmm === DAY_END ? 59 : 0, milliseconds: 0 })
}

// The chosen time on a day, or the end of that day when that time has already passed.
export function expiryOn(day: Date, hhmm: string, now = new Date()): Date {
  const at = atTime(day, hhmm)
  return at > now ? at : atTime(day, DAY_END)
}

// What the picker button shows: the date, plus the time when it is not the end of the day.
export function expiryField(d: Date, now = new Date()): string {
  if (isDayEnd(d)) return format(d, 'MMM d, yyyy')
  return format(d, isSameYear(d, now) ? 'MMM d, HH:mm' : 'MMM d, yyyy, HH:mm')
}

// "in 5 h 12 m" in the last day, then "tomorrow" or "in 3 days"; "expired" once it has passed.
export function expiresIn(at: Date, now = new Date()): string {
  const mins = differenceInMinutes(at, now, { roundingMethod: 'ceil' })
  if (mins <= 0) return 'expired'
  if (mins < 24 * 60) {
    const h = Math.floor(mins / 60)
    const m = mins % 60
    return `in ${[h > 0 && `${String(h)} h`, m > 0 && `${String(m)} m`].filter(Boolean).join(' ')}`
  }
  const days = differenceInCalendarDays(at, now)
  return days === 1 ? 'tomorrow' : `in ${String(days)} days`
}

// "Oct 7, 22:00" within a day of now, else "Oct 7".
export function expiryStamp(at: Date, now = new Date()): string {
  return format(at, Math.abs(differenceInMinutes(at, now)) < 24 * 60 ? 'MMM d, HH:mm' : 'MMM d')
}
