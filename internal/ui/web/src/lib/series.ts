import { formatCompact, formatUSD } from '@/lib/format'
import { formatInZone } from '@/lib/timezone'

// Helpers for a time series drawn as bars, shared by every chart (Overview requests, budget spend).

const DAY_SECONDS = 86_400

// "$20" · "$1.5K" · "$0.0020", for a money axis.
export function moneyTick(v: number): string {
  if (v === 0) return '$0'
  if (v < 1) return formatUSD(v).value
  const f = formatCompact(v)
  return `$${f.value}${f.unit ?? ''}`
}

// "0" · "850" · "3K", for a count axis.
export function countTick(v: number): string {
  const f = formatCompact(v)
  return `${f.value}${f.unit ?? ''}`
}

// Three equal steps of 1, 2, 5 or 10 times a power of ten that hold the tallest bar.
export function niceStep(max: number): number {
  const raw = max > 0 ? max / 3 : 1
  const pow = 10 ** Math.floor(Math.log10(raw))
  const m = raw / pow
  return (m <= 1 ? 1 : m <= 2 ? 2 : m <= 5 ? 5 : 10) * pow
}

// "Oct 18, 09:00 – 10:00", or "Oct 18" for day bars, in the chosen zone.
export function bucketSpan(start: Date, bucketSeconds: number, zone: string): string {
  if (bucketSeconds >= DAY_SECONDS) {
    const last = new Date(start.getTime() + (bucketSeconds - DAY_SECONDS) * 1000)
    const first = formatInZone(start, zone, 'MMM d')
    return bucketSeconds === DAY_SECONDS ? first : `${first} – ${formatInZone(last, zone, 'MMM d')}`
  }
  const end = new Date(start.getTime() + bucketSeconds * 1000)
  return `${formatInZone(start, zone, 'MMM d, HH:mm')} – ${formatInZone(end, zone, 'HH:mm')}`
}

// What sits under a bar: "04:00", "Oct 18", or the days a group covers, "Oct 16–18" · "Oct 30–Nov 1".
export function axisLabel(start: Date, bucketSeconds: number, zone: string): string {
  if (bucketSeconds < DAY_SECONDS) return formatInZone(start, zone, 'HH:mm')
  const first = formatInZone(start, zone, 'MMM d')
  if (bucketSeconds === DAY_SECONDS) return first
  const last = new Date(start.getTime() + (bucketSeconds - DAY_SECONDS) * 1000)
  const sameMonth = formatInZone(start, zone, 'MMM') === formatInZone(last, zone, 'MMM')
  return `${first}–${formatInZone(last, zone, sameMonth ? 'd' : 'MMM d')}`
}

// "Hourly" · "Every 10 minutes" · "Every 3 days"
export function intervalName(seconds: number): string {
  if (seconds === 60) return 'Every minute'
  if (seconds === 3600) return 'Hourly'
  if (seconds === DAY_SECONDS) return 'Daily'
  if (seconds === 604_800) return 'Weekly'
  if (seconds < 3600) return `Every ${String(seconds / 60)} minutes`
  if (seconds < DAY_SECONDS) return `Every ${String(seconds / 3600)} hours`
  return `Every ${String(seconds / DAY_SECONDS)} days`
}

// A bar of two days or more is a group of days, not something to inspect: one click opens it into its
// days. Anything shorter is selected first, and a second click goes in.
export const GROUP_SECONDS = 2 * DAY_SECONDS

// What a click on a bar zooms into: that interval re-cut one step finer, by bucket seconds. The metrics
// API needs a range of at least an hour, so bars under an hour (10 and 1 minute) select instead.
export const FINER_BUCKET: Record<number, number> = {
  2_592_000: 86_400, // 30 days → days
  604_800: 86_400, // week → days
  259_200: 86_400, // 3 days → days
  86_400: 3_600, // day → hours
  28_800: 3_600, // 8 hours → hours
  3_600: 60, // hour → minutes
}
