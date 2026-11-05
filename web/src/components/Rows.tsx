// The rows and marks every list shares: a parcel row, its status pill, the
// stage stepper (full on a parcel's page, a thin bar in lists).
import { Link } from 'react-router-dom'
import { ago } from '../lib/format'
import { STATUS, arrivesText, deliveredText, isActive, returnText, stages } from '../lib/parcels'
import type { Parcel } from '../lib/types'
import { Icon } from './icons'

export function StatusPill({ p }: { p: Pick<Parcel, 'status' | 'status_label' | 'stale' | 'archived'> }) {
  const stale = p.stale && isActive(p)
  return <span className={`pill st-${p.status}${stale ? ' st-stale' : ''}`}><Icon name={stale ? 'clock' : STATUS[p.status]?.icon || 'package'} size={12} />{stale ? 'No news' : p.status_label || STATUS[p.status]?.label}</span>
}

/** Four short segments: how far along the parcel is. */
export function Progress({ p }: { p: Pick<Parcel, 'status'> }) {
  return (
    <span className="prog" aria-hidden="true">
      {stages(p).map((s, i) => <i key={i} className={`prog-${s.state}`} />)}
    </span>
  )
}

/** Label created → On the way → Out for delivery → Delivered, with the variants. */
export function Stepper({ p }: { p: Pick<Parcel, 'status'> }) {
  const st = stages(p)
  return (
    <ol className="stepper" aria-label="Where the parcel is">
      {st.map((s, i) => (
        <li key={i} className={`step step-${s.state}`} aria-current={s.state === 'now' ? 'step' : undefined}>
          <span className="step-dot">{s.state === 'done' ? <Icon name="check" size={12} strokeWidth={2.6} /> : s.state === 'bad' ? <Icon name="alert" size={12} strokeWidth={2.4} /> : null}</span>
          <span className="step-label">{s.label}</span>
        </li>
      ))}
    </ol>
  )
}

export function ParcelRow({ p, progress = false, showReturn = false }: { p: Parcel; progress?: boolean; showReturn?: boolean }) {
  const active = isActive(p)
  const who = p.direction === 'out' ? (p.contact_name ? `To ${p.contact_name}` : 'Sent by you') : p.shop
  const sub = [who, p.carrier_name].filter(Boolean).join(' · ')
  const latest = [p.status_text, p.location].filter(Boolean).join(' · ')
  const when = showReturn ? returnText(p) : active ? arrivesText(p) : deliveredText(p)
  return (
    <Link to={`/parcels/${p.id}`} className={`list-row parcel-row ${p.archived ? 'is-archived' : ''}`}>
      <span className={`list-ico st-${p.status}${p.stale && active ? ' st-stale' : ''}`}><Icon name={STATUS[p.status]?.icon || 'package'} size={16} /></span>
      <span className="grow row-main">
        <span className="row-title"><span className="ellipsis">{p.title}</span>{p.direction === 'out' && <span className="badge">sent</span>}</span>
        {sub && <span className="muted small ellipsis">{sub}</span>}
        {latest && <span className="row-latest small ellipsis">{latest}{p.status_at && <span className="muted"> · {ago(p.status_at)}</span>}</span>}
        {progress && active && (p.tracking_number || p.status !== 'pending') && <Progress p={p} />}
      </span>
      <span className="row-side">
        <StatusPill p={p} />
        {when && <span className={`row-when small ${showReturn && p.return_state !== 'returned' && p.return_by ? 'warn-text' : 'muted'}`}>{when}</span>}
      </span>
    </Link>
  )
}
