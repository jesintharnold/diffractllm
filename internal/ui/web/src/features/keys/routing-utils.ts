import type { KeyMode, ProviderConfig } from '@/features/keys/api'
import { providerLabel } from '@/lib/provider-colors'

// One provider row of a key's routing, as the form edits it. Weights stay text while typing.
export interface RouteDraft {
  provider: string
  allModels: boolean
  allowed: string[]
  blocked: string[]
  weight: string
}

export const newDraft = (provider: string): RouteDraft => ({
  provider,
  allModels: true,
  allowed: [],
  blocked: [],
  weight: '1.00',
})

// 0.7 → "0.70", 0.125 → "0.125": two decimals at least, nothing the server sent is cut off.
function weightText(w: number): string {
  const s = String(Number(w.toPrecision(6)))
  if (!s.includes('.')) return `${s}.00`
  return /\.\d$/.test(s) ? `${s}0` : s
}

export const fromConfigs = (configs: ProviderConfig[]): RouteDraft[] =>
  configs.map((c) => ({
    provider: c.provider,
    allModels: c.allowed_models.includes('*'),
    allowed: c.allowed_models.includes('*') ? [] : c.allowed_models,
    blocked: c.blocked_models ?? [],
    weight: weightText(c.weight ?? 1),
  }))

// One provider is direct (fixed at 1.0); more than one is weighted.
export const modeOf = (drafts: RouteDraft[]): KeyMode => (drafts.length > 1 ? 'weighted' : 'direct')

export const toConfigs = (drafts: RouteDraft[]): ProviderConfig[] =>
  drafts.map((d) => ({
    provider: d.provider,
    allowed_models: d.allModels ? ['*'] : d.allowed,
    blocked_models: d.blocked,
    weight: drafts.length > 1 ? Number(d.weight) : 1,
  }))

export const weightTotal = (drafts: RouteDraft[]) =>
  drafts.reduce((s, d) => s + Number(d.weight), 0)

// Equal shares that add up to exactly 1.00: 0.50/0.50, 0.33/0.33/0.34.
export function rebalance(drafts: RouteDraft[]): RouteDraft[] {
  const n = drafts.length
  if (n === 1) return drafts.map((d) => ({ ...d, weight: '1.00' }))
  const share = Math.floor(100 / n)
  return drafts.map((d, i) => ({
    ...d,
    weight: ((i === n - 1 ? 100 - share * (n - 1) : share) / 100).toFixed(2),
  }))
}

// Why this routing cannot be saved yet, or null. The server checks the same rules.
export function routingProblem(drafts: RouteDraft[]): string | null {
  if (drafts.length === 0) return 'Add at least one provider'
  for (const d of drafts) {
    const name = providerLabel(d.provider)
    if (!d.allModels && d.allowed.length === 0) {
      return `${name}: pick at least one allowed model, or allow all`
    }
    if (d.blocked.includes('*')) return `${name}: a provider cannot block every model`
  }
  if (drafts.length > 1) {
    for (const d of drafts) {
      const w = Number(d.weight)
      if (d.weight.trim() === '' || !Number.isFinite(w) || w < 0 || w > 1) {
        return `${providerLabel(d.provider)}: a weight is between 0 and 1`
      }
    }
    const total = weightTotal(drafts)
    if (Math.abs(total - 1) > 1e-6) return `Weights total ${total.toFixed(2)}; they must total 1.00`
  }
  return null
}
