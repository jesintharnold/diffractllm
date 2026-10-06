// Display formatting via Intl.NumberFormat (compact notation, percent, currency, units).
// Formatters return the number and its unit apart, so tiles can style the unit smaller.
export interface Formatted {
  value: string
  unit?: string
}

const UNIT_PARTS = new Set<string>(['compact', 'percentSign', 'unit'])

// Splits Intl parts into the number ("14.2") and its unit ("M", "%", "ms").
function split(formatter: Intl.NumberFormat, n: number): Formatted {
  let value = ''
  let unit = ''
  for (const part of formatter.formatToParts(n)) {
    if (UNIT_PARTS.has(part.type)) unit += part.value
    else if (part.type !== 'literal') value += part.value
  }
  return unit ? { value, unit } : { value }
}

const grouped = new Intl.NumberFormat('en-US')
const compact = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 })
const percent = new Intl.NumberFormat('en-US', { style: 'percent', maximumSignificantDigits: 2 })
const millis = new Intl.NumberFormat('en-US', {
  style: 'unit',
  unit: 'millisecond',
  unitDisplay: 'narrow',
  maximumFractionDigits: 0,
})
const seconds = new Intl.NumberFormat('en-US', {
  style: 'unit',
  unit: 'second',
  unitDisplay: 'narrow',
  maximumFractionDigits: 2,
})
const dollars = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' })
const cents = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  maximumSignificantDigits: 2,
})

// 12,481 · above a million, compact: 14.2 M
export const formatCount = (n: number): Formatted =>
  n < 1_000_000 ? { value: grouped.format(n) } : split(compact, n)

// 2,186 → 2.2 K · 14,200,000 → 14.2 M
export const formatCompact = (n: number): Formatted => split(compact, n)

// 0.0183 → 1.8 %
export const formatPercent = (ratio: number): Formatted => split(percent, ratio)

// 842 → 842 ms · 4181 → 4.18 s
export const formatLatency = (ms: number): Formatted =>
  ms < 1000 ? split(millis, ms) : split(seconds, ms / 1000)

// $284.19 · below a dollar keep two significant digits: $0.0017
export const formatUSD = (usd: number): Formatted => ({
  value: (usd > 0 && usd < 1 ? cents : dollars).format(usd),
})
