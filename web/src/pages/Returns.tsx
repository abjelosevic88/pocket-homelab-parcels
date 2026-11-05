// Send back: parcels with a return deadline that are not decided yet.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { announceChange, useOnChange } from '../lib/data'
import { fmtDate } from '../lib/format'
import { daysLeft, returnText } from '../lib/parcels'
import type { Parcel } from '../lib/types'
import { Card, Empty, Loading, errMsg, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'

export default function Returns() {
  const { toast } = useToast()
  const { data, reload } = useAsync(() => api.get<{ parcels: Parcel[] }>(`${V1}/parcels?view=returns`).then(r => r.parcels), [])
  useOnChange(reload)
  const [busy, setBusy] = useState(0)
  const set = async (p: Parcel, state: string, msg: string) => {
    setBusy(p.id)
    try { await api.put(`${V1}/parcels/${p.id}`, { return_state: state }); toast(msg); announceChange() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(0) }
  }
  return (
    <div className="stack loose">
      <div className="page-header"><div className="grow"><h1>Send back</h1><div className="sub">Parcels with a last day to return them. You get a reminder three days before, the day before and on the day.</div></div></div>
      {!data ? <Loading /> : data.length === 0 ? (
        <Card><Empty title="Nothing to send back" icon="undo">Give a parcel a return deadline (Edit → Last day to send it back) and it shows up here until you keep it or send it back.</Empty></Card>
      ) : (
        <Card flush><div className="list">{data.map(p => {
          const n = daysLeft(p.return_by)
          return (
            <div key={p.id} className="list-row return-row">
              <span className={`list-ico ${n <= 1 ? 'st-exception' : n <= 3 ? 'st-pickup' : 'st-in_transit'}`}><Icon name="undo" size={16} /></span>
              <Link to={`/parcels/${p.id}`} className="grow row-main">
                <span className="row-title"><span className="ellipsis">{p.title}</span>{p.return_state === 'returning' && <span className="badge warn">going back</span>}</span>
                <span className="muted small ellipsis">{[p.shop, p.order_number && `Order ${p.order_number}`, `last day ${fmtDate(p.return_by)}`].filter(Boolean).join(' · ')}</span>
                <span className={`small ${n <= 3 ? 'warn-text' : 'muted'}`}>{returnText(p)}</span>
              </Link>
              <div className="row find-actions">
                <button type="button" className="btn sm" disabled={busy === p.id} onClick={() => set(p, 'keeping', 'Keeping it')}>Keep it</button>
                <button type="button" className="btn primary sm" disabled={busy === p.id} onClick={() => set(p, p.return_state === 'returning' ? 'returned' : 'returning', p.return_state === 'returning' ? 'Marked sent back' : 'Marked to send back')}>
                  {p.return_state === 'returning' ? 'Sent back' : 'Send it back'}</button>
              </div>
            </div>)
        })}</div></Card>
      )}
    </div>
  )
}
