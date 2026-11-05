// What every screen says about a parcel: its status in words and icons,
// when it arrives, money, and where the news came from.
import { addDays, fmtDate, todayISO, weekday } from './format'
import type { Parcel, Status } from './types'

export const STATUS: Record<Status, { label: string; icon: string }> = {
  pending: { label: 'Waiting for news', icon: 'hourglass' },
  info_received: { label: 'Label created', icon: 'tag' },
  in_transit: { label: 'On the way', icon: 'route' },
  out_for_delivery: { label: 'Out for delivery', icon: 'truck' },
  pickup: { label: 'Ready to pick up', icon: 'store' },
  delivered: { label: 'Delivered', icon: 'check-circle' },
  exception: { label: 'Problem', icon: 'alert' },
  returned: { label: 'Sent back', icon: 'undo' },
  expired: { label: 'Tracking ended', icon: 'x-circle' },
}
export const STATUS_ORDER: Status[] = ['pending', 'info_received', 'in_transit', 'out_for_delivery', 'pickup', 'delivered', 'exception', 'returned', 'expired']

export const isFinal = (s: Status) => s === 'delivered' || s === 'returned' || s === 'expired'
export const isActive = (p: Pick<Parcel, 'status' | 'archived'>) => !p.archived && !isFinal(p.status)

/** The four stages of a normal journey; the third and fourth change with the status. */
export function stages(p: Pick<Parcel, 'status'>): { label: string; state: 'done' | 'now' | 'todo' | 'bad' }[] {
  const s = p.status
  const at: Record<Status, number> = { pending: -1, info_received: 0, in_transit: 1, out_for_delivery: 2, pickup: 2, delivered: 3, exception: 1, returned: 3, expired: 3 }
  const n = at[s]
  const third = s === 'pickup' ? 'Ready to pick up' : s === 'exception' ? 'Problem' : 'Out for delivery'
  const fourth = s === 'returned' ? 'Sent back' : s === 'expired' ? 'Tracking ended' : 'Delivered'
  const labels = ['Label created', 'On the way', third, fourth]
  return labels.map((label, i) => {
    if (s === 'exception' && i === 2) return { label, state: 'bad' }
    if ((s === 'returned' || s === 'expired') && i === 3) return { label, state: 'bad' }
    if (i < n) return { label, state: 'done' }
    if (i === n) return { label, state: isFinal(s) ? 'done' : 'now' }
    return { label, state: 'todo' }
  })
}

/** "Arrives today", "Tomorrow", "In 3 days (Fri 7 Nov)", "9–14 Nov", "2 days late". */
export function arrivesText(p: Pick<Parcel, 'arrives_in' | 'expected_on' | 'expected_until' | 'status'>): string {
  if (p.status === 'out_for_delivery') return 'Arrives today'
  if (p.status === 'pickup') return 'Waiting for you'
  if (!p.expected_on) return ''
  if (p.expected_until && p.expected_until !== p.expected_on) {
    const today = todayISO()
    if (p.expected_on <= today && p.expected_until >= today) return `Any day now, by ${fmtDate(p.expected_until, false)}`
    if (p.expected_until < today) return `Late: expected by ${fmtDate(p.expected_until, false)}`
    return `Expected ${fmtDate(p.expected_on, false)} – ${fmtDate(p.expected_until, false)}`
  }
  const n = p.arrives_in
  if (n === null || n === undefined) return `Expected ${fmtDate(p.expected_on, false)}`
  if (n === 0) return 'Arrives today'
  if (n === 1) return 'Arrives tomorrow'
  if (n > 1 && n < 7) return `In ${n} days, ${weekday(p.expected_on)} ${fmtDate(p.expected_on, false)}`
  if (n >= 7) return `Expected ${fmtDate(p.expected_on, false)}`
  return n === -1 ? 'A day late' : `${-n} days late`
}

/** The day line for a delivered parcel: "Delivered 3 Nov · took 6 days". */
export function deliveredText(p: Pick<Parcel, 'delivered_at' | 'days' | 'status' | 'status_at'>): string {
  const when = p.delivered_at || p.status_at
  if (!when) return ''
  const d = localDay(when)
  const day = d === todayISO() ? 'today' : d === addDays(todayISO(), -1) ? 'yesterday' : fmtDate(d, false)
  const took = p.status === 'delivered' && p.days != null ? ` · took ${p.days === 0 ? 'less than a day' : p.days === 1 ? '1 day' : `${p.days} days`}` : ''
  return `${p.status === 'delivered' ? 'Delivered' : STATUS[p.status]?.label || ''} ${day}${took}`
}

/** The local day of an RFC 3339 time. */
export function localDay(at: string): string {
  const t = new Date(at)
  if (Number.isNaN(t.getTime())) return at.slice(0, 10)
  return `${t.getFullYear()}-${String(t.getMonth() + 1).padStart(2, '0')}-${String(t.getDate()).padStart(2, '0')}`
}

export function daysLeft(iso: string): number {
  const [y, m, d] = iso.split('-').map(Number)
  const t = new Date(); t.setHours(0, 0, 0, 0)
  return Math.round((new Date(y, m - 1, d).getTime() - t.getTime()) / 86400000)
}

export function returnText(p: Pick<Parcel, 'return_by' | 'return_state'>): string {
  if (!p.return_by) return ''
  if (p.return_state === 'returned') return 'Sent back'
  if (p.return_state === 'keeping') return 'Keeping it'
  const n = daysLeft(p.return_by)
  if (n < 0) return 'Return window closed'
  if (n === 0) return 'Last day to send back'
  if (n === 1) return '1 day left to send back'
  return `${n} days left to send back`
}

export const SOURCE: Record<string, string> = { mail: 'Email', manual: 'You', '17track': '17TRACK', cainiao: 'Cainiao', demo: 'Carrier', import: 'Import' }
export const sourceName = (s: string) => SOURCE[s] || (s ? s.charAt(0).toUpperCase() + s.slice(1) : '')

export function money(minor: number | null | undefined, currency = 'EUR', whole = false): string {
  const v = (minor || 0) / 100
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency, minimumFractionDigits: whole ? 0 : 2, maximumFractionDigits: whole ? 0 : 2 }).format(v)
  } catch {
    return `${v.toFixed(whole ? 0 : 2)} ${currency}`
  }
}

/** A typed amount ("12,50" or "12.50") → minor units, null when empty or bad. */
export function parseMoney(s: string): number | null {
  const t = s.trim().replace(/\s/g, '').replace(/,(\d{1,2})$/, '.$1').replace(/,/g, '')
  if (!t) return null
  const n = Number(t)
  if (!Number.isFinite(n) || n < 0) return null
  return Math.round(n * 100)
}
export const moneyInput = (minor: number | null | undefined) => minor == null ? '' : (minor / 100).toFixed(2)

/** Tracking numbers read better in groups: RR 123 456 785 CN. */
export function spacedNumber(n: string): string {
  if (/^[A-Z]{2}\d{9}[A-Z]{2}$/.test(n)) return `${n.slice(0, 2)} ${n.slice(2, 5)} ${n.slice(5, 8)} ${n.slice(8, 11)} ${n.slice(11)}`
  return n.replace(/(.{4})(?=.)/g, '$1 ')
}

/** Who follows a parcel, in words. */
export function followText(checkWith: string | undefined, hasNumber: boolean): string {
  if (!hasNumber) return 'No tracking number: update it yourself, or Pocket Mail brings the news.'
  if (checkWith === '17track') return 'Followed automatically through 17TRACK.'
  if (checkWith === 'cainiao') return 'Followed automatically through Cainiao (free).'
  return 'Add a 17TRACK key in Settings → Tracking to follow this carrier automatically, or let Pocket Mail bring the news.'
}
