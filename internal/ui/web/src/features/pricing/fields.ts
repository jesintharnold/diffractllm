import type { PricingFields } from '@/features/pricing/api'

// Sections in core.Pricing's order. A key lands in the first section whose test matches.
const SECTIONS: readonly (readonly [string, (key: string) => boolean])[] = [
  ['Video', (k) => k.includes('video') || /^(input|output)_cost_per_second/.test(k)],
  ['Audio', (k) => k.includes('audio')],
  ['Image', (k) => k.includes('image') || k.includes('pixel')],
  ['Characters', (k) => k.includes('character')],
  ['Reasoning', (k) => k.includes('reasoning') || k.includes('citation')],
  ['Per request', (k) => /query|request|page|credit|session|dbu|unit/.test(k)],
  ['Cache', (k) => k.startsWith('cache_') || k.endsWith('_cache_hit')],
  ['Text', () => true],
]
const ORDER = ['Text', 'Cache', 'Reasoning', 'Audio', 'Image', 'Video', 'Characters', 'Per request']

// The token rates people edit most, shortened; their suffix becomes qualifiers.
const SHORT: readonly (readonly [string, string, number])[] = [
  ['input_cost_per_token', 'Input', 0],
  ['output_cost_per_token', 'Output', 1],
  ['cache_read_input_token_cost', 'Read', 0],
  ['cache_creation_input_token_cost', 'Write', 1],
]

// "_batches_above_272k_tokens" → "batch, >272k"
function qualifiers(rest: string): string {
  return rest
    .replace(/above_(\d+k)_tokens/g, '>$1')
    .replace(/above_1hr|(?<![a-z])1h(?![a-z])/g, '1h TTL')
    .replace('batches', 'batch')
    .split('_')
    .filter(Boolean)
    .join(', ')
}

export interface FieldInfo {
  key: string
  label: string
  sort: string
}

// "input_cost_per_token_batches" → "Input · batch"; uncommon keys read as "Output / reasoning token".
export function describe(key: string): FieldInfo {
  if (key === 'input_cost_per_token_cache_hit') return { key, label: 'Read · cache hit', sort: 'zz0' }
  for (const [prefix, short, rank] of SHORT) {
    if (key === prefix || key.startsWith(`${prefix}_`)) {
      const q = qualifiers(key.slice(prefix.length))
      // Pair rates by qualifier so Input and Output sit side by side.
      return { key, label: q ? `${short} · ${q}` : short, sort: `${q === '' ? '' : 'q'}${q} ${String(rank)}` }
    }
  }
  const cache = /^cache_(read|creation)_input_(\w+)_token_cost$/.exec(key)
  const words = (cache ? `${cache[1] === 'read' ? 'read' : 'write'}_cost_per_${cache[2] ?? ''}_token` : key)
    .replace(/^cost_per_/, 'per_')
    .replace('_cost_per_', ' / ')
    .replaceAll('_', ' ')
    .replace(/ above (\d+k) tokens/, ' · >$1')
    .replace(/ above (\d+) and (\d+) pixels/, ' · >$1×$2 px')
    .replace(/ above (\d+) pixels/, ' · >$1 px')
    .replace(/ above (\d+s) interval/, ' · >$1')
    .replace(/ (priority|flex|batches|fast)$/, ' · $1')
    .replace(/\bocr\b/, 'OCR')
    .replace('dbu', 'DBU')
  return { key, label: words.charAt(0).toUpperCase() + words.slice(1), sort: `zz${key}` }
}

export interface FieldSection {
  title: string
  fields: FieldInfo[]
}

// Every field the model's base price defines plus any the override sets, grouped into
// sections; empty sections are dropped.
export function fieldSections(bases: PricingFields[], override: PricingFields): FieldSection[] {
  const keys = new Set<string>()
  for (const p of [...bases, override]) {
    for (const [key, value] of Object.entries(p)) {
      if (value !== undefined) keys.add(key)
    }
  }
  const grouped = new Map<string, FieldInfo[]>()
  for (const key of keys) {
    const title = SECTIONS.find(([, test]) => test(key))?.[0] ?? 'Text'
    grouped.set(title, [...(grouped.get(title) ?? []), describe(key)])
  }
  return ORDER.filter((t) => grouped.has(t)).map((title) => ({
    title,
    fields: (grouped.get(title) ?? []).sort((a, b) => a.sort.localeCompare(b.sort, 'en', { numeric: true })),
  }))
}

// 1e-7 → "0.0000001": per-token prices are tiny and must never show in exponent form.
export const plain = (n: number) =>
  n.toLocaleString('en-US', { useGrouping: false, maximumFractionDigits: 20 })

// What a field charges per: "token" for token rates, else the unit its key names
// ("output_cost_per_image" → "image", "…_per_second" → "sec").
function unitOf(key: string): string {
  if (key.includes('per_second')) return 'sec'
  const m = /cost_per_([a-z]+)(?:_([a-z]+))?/.exec(key)
  if (!m || m[1] === 'token' || m[2] === 'token') return 'token'
  return m[1] === 'character' ? 'char' : (m[1] ?? 'unit')
}

// A field's rate as people read it: token rates per 1M ("$2.50/1M"), the rest per unit
// ("$0.04/image").
export function rateText(key: string, v: number): string {
  const unit = unitOf(key)
  if (unit === 'token') return per1M(v)
  return `$${v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 8 })}/${unit}`
}

// A flat price object as JSON, numbers in plain form (never 3.625e-9).
export function pricingJSON(p: PricingFields): string {
  const lines = Object.entries(p).flatMap(([k, v]) => (v === undefined ? [] : [`  ${JSON.stringify(k)}: ${plain(v)}`]))
  return lines.length === 0 ? '{}' : `{\n${lines.join(',\n')}\n}`
}

// Per-token rate → "$2.50/1M"; tiny rates keep four decimals so they don't read as $0.00.
function per1M(perToken: number): string {
  const v = perToken * 1e6
  return `$${v.toLocaleString('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: v > 0 && v < 0.01 ? 4 : 2,
  })}/1M`
}
