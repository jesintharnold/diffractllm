import { differenceInCalendarDays, format } from 'date-fns'
import type { Credential } from '@/features/providers/api'
import { expiresIn, expiryStamp } from '@/features/providers/expiry'
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
    if (at <= now) {
      return {
        kind: 'expired',
        title: `${c.name} has expired`,
        detail: `Expired ${expiryStamp(at, now)}. Routing no longer uses it.`,
      }
    }
    if (differenceInCalendarDays(at, now) <= EXPIRY_WARNING_DAYS) {
      return {
        kind: 'expiring',
        title: `${c.name} expires ${expiresIn(at, now)}`,
        detail: `${expiryStamp(at, now)}. Rotate it before then or ${providerLabel(c.provider)} traffic runs on the remaining credentials.`,
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

// "Oct 10 · in 3 days" or "Oct 7 · in 5 h 12 m", or a dash when nothing expires.
// Shared with the virtual keys screens.
export function expiryText(expiresAt: string | undefined, now = new Date()): string {
  if (!expiresAt) return '—'
  const at = new Date(expiresAt)
  return `${format(at, 'MMM d')} · ${expiresIn(at, now)}`
}

export const expiryLabel = (c: Credential, now = new Date()) => expiryText(c.expires_at, now)

// Within the warning window or already past: the list shows it in the warning colour.
export function expiryDue(expiresAt: string | undefined, now = new Date()): boolean {
  return !!expiresAt && differenceInCalendarDays(new Date(expiresAt), now) <= EXPIRY_WARNING_DAYS
}

export const isLive = (c: Credential, now = new Date()) =>
  c.enabled && (!c.expires_at || new Date(c.expires_at) > now)
