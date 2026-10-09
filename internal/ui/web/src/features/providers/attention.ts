import { differenceInCalendarDays, format } from 'date-fns'
import type { Credential } from '@/features/providers/api'
import { providerLabel } from '@/lib/provider-colors'

export const EXPIRY_WARNING_DAYS = 7

export interface Issue {
  kind: 'expired' | 'expiring' | 'disabled'
  title: string
  detail: string
}

// Why a credential needs someone to act, or null. Expiry beats disabled: it stops routing either way.
export function issueOf(c: Credential, now = new Date()): Issue | null {
  if (c.expires_at) {
    const at = new Date(c.expires_at)
    const days = differenceInCalendarDays(at, now)
    if (at <= now) {
      return {
        kind: 'expired',
        title: `${c.name} has expired`,
        detail: `Expired ${format(at, 'MMM d')}. Routing no longer uses it.`,
      }
    }
    if (days <= EXPIRY_WARNING_DAYS) {
      const when = days <= 0 ? 'today' : days === 1 ? 'tomorrow' : `in ${String(days)} days`
      return {
        kind: 'expiring',
        title: `${c.name} expires ${when}`,
        detail: `${format(at, 'MMM d')}. Rotate it before then or ${providerLabel(c.provider)} traffic runs on the remaining credentials.`,
      }
    }
  }
  if (!c.enabled) {
    return {
      kind: 'disabled',
      title: `${c.name} is disabled`,
      detail: 'Routing skips it until it is turned back on.',
    }
  }
  return null
}

// "Oct 10 · in 3 days", or a dash when the credential never expires.
export function expiryLabel(c: Credential, now = new Date()): string {
  if (!c.expires_at) return '—'
  const at = new Date(c.expires_at)
  const days = differenceInCalendarDays(at, now)
  const rel =
    at <= now
      ? 'expired'
      : days === 0
        ? 'today'
        : days === 1
          ? 'tomorrow'
          : `in ${String(days)} days`
  return `${format(at, 'MMM d')} · ${rel}`
}

export const isLive = (c: Credential, now = new Date()) =>
  c.enabled && (!c.expires_at || new Date(c.expires_at) > now)
