import { differenceInCalendarDays } from 'date-fns'
import type { KeyList } from '@/features/keys/api'
import { EXPIRY_WARNING_DAYS } from '@/features/providers/attention'
import { expiresIn, expiryStamp } from '@/features/providers/expiry'

export interface KeyIssue {
  id: string
  title: string
  detail: string
  to?: string // the key's page, when there is one to open
}

const short = (id: string) => (id.length > 12 ? `${id.slice(0, 6)}…${id.slice(-3)}` : id)

// What needs a person on the keys list: unreadable keys, and active keys expiring or expired.
// A disabled key is the owner's choice, so it is not flagged.
export function keyIssues(list: KeyList, now = new Date()): KeyIssue[] {
  const issues: KeyIssue[] = []
  if (list.skipped > 0) {
    issues.push({
      id: 'skipped',
      title: `${String(list.skipped)} ${list.skipped === 1 ? 'key' : 'keys'} could not be loaded`,
      detail: `Their stored secret is unreadable, so they cannot authenticate a request. IDs: ${list.skipped_ids.map(short).join(', ')}.`,
    })
  }
  for (const k of list.virtual_keys) {
    if (!k.is_active || !k.expires_at) continue
    const at = new Date(k.expires_at)
    const to = `/governance/virtual-keys/${k.id}`
    if (at <= now) {
      issues.push({
        id: k.id,
        title: `${k.client_id} has expired`,
        detail: `Expired ${expiryStamp(at, now)}. Extend its expiry to let the client back in.`,
        to,
      })
    } else if (differenceInCalendarDays(at, now) <= EXPIRY_WARNING_DAYS) {
      issues.push({
        id: k.id,
        title: `${k.client_id} expires ${expiresIn(at, now)}`,
        detail: `${expiryStamp(at, now)}. Extend its expiry, or the client loses access.`,
        to,
      })
    }
  }
  return issues
}
