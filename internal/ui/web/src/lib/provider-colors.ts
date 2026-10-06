// Chart colours per provider: one map for every chart, matching ui/design "Provider colors".
// Known providers have a fixed brand-inspired colour; any other name gets one of three
// fallbacks picked by a stable hash, so a provider looks the same on every load and page.
const KNOWN = ['openai', 'azure', 'anthropic', 'nvidia', 'xai', 'gemini', 'bedrock', 'mistral']
const FALLBACKS = ['other-1', 'other-2', 'other-3']

// Errors always sit on top of each bar; cancelled sits just below them.
export const ERROR_COLOR = 'var(--chart-error)'
export const CANCELLED_COLOR = 'var(--chart-cancelled)'

function hash(name: string): number {
  let h = 0
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) | 0
  return Math.abs(h)
}

export function providerColor(provider: string): string {
  const key = provider.toLowerCase()
  const slot = KNOWN.includes(key) ? key : (FALLBACKS[hash(key) % FALLBACKS.length] ?? 'other-1')
  return `var(--provider-${slot})`
}

const LABELS: Record<string, string> = {
  openai: 'OpenAI',
  azure: 'Azure',
  anthropic: 'Anthropic',
  nvidia: 'NVIDIA',
  xai: 'xAI',
  gemini: 'Gemini',
  bedrock: 'Bedrock',
  mistral: 'Mistral',
}

// Display name for a provider id; unknown ids show as sent.
export function providerLabel(provider: string): string {
  return LABELS[provider.toLowerCase()] ?? provider
}

// Stable stacking order: known providers in map order, then the rest by name.
export function sortProviders(providers: Iterable<string>): string[] {
  const rank = (p: string) => {
    const i = KNOWN.indexOf(p.toLowerCase())
    return i === -1 ? KNOWN.length : i
  }
  return [...providers].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
}
