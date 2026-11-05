// Found in your email: tracking numbers Pocket Mail brought in, to add or ignore.
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useOnChange } from '../lib/data'
import { ago, plural } from '../lib/format'
import { STATUS, spacedNumber } from '../lib/parcels'
import type { Find, Parcel, Status } from '../lib/types'
import { Card, CoreHint, Empty, Field, Loading, MenuButton, Modal, errMsg, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'

export default function Email() {
  const { settings } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const { data, reload } = useAsync(() => api.get<{ finds: Find[] }>(`${V1}/finds`).then(r => r.finds), [])
  const { data: done, reload: reloadDone } = useAsync(() => api.get<{ finds: Find[] }>(`${V1}/finds?state=all&limit=40`).then(r => r.finds.filter(f => f.state !== 'new')), [])
  useOnChange(() => { reload(); reloadDone() })
  const [busy, setBusy] = useState(0)
  const [scanning, setScanning] = useState(false)
  const [named, setNamed] = useState<Find | null>(null)
  const mailOk = !!settings?.mail_configured
  const coreOn = !!settings && settings.core_configured && settings.core_registered !== false
  const add = async (f: Find, body: { title?: string; shop?: string } = {}) => {
    setBusy(f.id)
    try { const p = await api.post<Parcel>(`${V1}/finds/${f.id}/add`, body); toast(<>Added: <Link to={`/parcels/${p.id}`}>{p.title}</Link></>); announceChange() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(0) }
  }
  const ignore = async (f: Find) => { setBusy(f.id); try { await api.post(`${V1}/finds/${f.id}/dismiss`); announceChange() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(0) } }
  const scan = async () => {
    setScanning(true)
    try {
      const r = await api.post<{ ok: boolean; error?: string; result: { read: number; found: number; added: number; updated: number } }>(`${V1}/mail/scan`)
      if (!r.ok) toast(r.error || 'Could not look through the email', 'err')
      else toast(r.result.read === 0 ? 'No new shipping emails' : `Read ${plural(r.result.read, 'shipping email')}: ${r.result.found} new, ${r.result.added} added, ${r.result.updated} updates`)
      announceChange()
    } catch (e) { toast(errMsg(e), 'err') } finally { setScanning(false) }
  }
  return (
    <div className="stack loose">
      <div className="page-header">
        <div className="grow"><h1>Found in your email</h1><div className="sub">Tracking numbers from your shipping emails in Pocket Mail. Add the ones you want to follow.</div></div>
        <div className="actions"><button type="button" className="btn" onClick={scan} disabled={!mailOk || scanning}><Icon name="refresh" size={15} className={scanning ? 'spin' : ''} />{scanning ? 'Looking…' : 'Look through my email now'}</button></div>
      </div>
      {!mailOk && <div className="callout info"><Icon name="mail" size={15} /><div>
        Pocket Mail is not connected{settings?.mail_detail ? ` (${settings.mail_detail})` : ''}. Once it is, Pocket Parcels reads your new receipts and shipping updates every half hour, finds the tracking numbers and follows “delivered” and “ready for pick-up” emails. <Link to="/settings/apps">Settings → Connected apps</Link>
        {!coreOn && <CoreHint />}
      </div></div>}
      {mailOk && settings && !settings.mail_scan && <div className="callout warn"><Icon name="alert" size={15} /><div>Looking through new email is switched off. <Link to="/settings/email">Settings → Email</Link></div></div>}

      {!data ? <Loading /> : data.length === 0 ? (
        <Card><Empty title="Nothing waiting" icon="mailbox">{mailOk ? 'New parcels from your email show up here.' : 'Connect Pocket Mail and new parcels from your email show up here.'}</Empty></Card>
      ) : (
        <Card flush title={`${plural(data.length, 'parcel')} to look at`}>
          <div className="list">{data.map(f => (
            <div key={f.id} className="list-row find-row">
              <span className="list-ico"><Icon name="mail" size={16} /></span>
              <div className="grow row-main">
                <span className="row-title"><span className="ellipsis">{f.title || f.subject}</span></span>
                <span className="muted small ellipsis">{[f.shop || f.from_name || f.from_addr, f.carrier_name, f.order_number && `Order ${f.order_number}`].filter(Boolean).join(' · ')}</span>
                <span className="small ellipsis find-meta"><span className="mono">{spacedNumber(f.tracking_number)}</span><span className="muted"> · “{f.subject}” · {ago(f.received_at)}</span>
                  {f.status_hint && <span className="muted"> · {STATUS[f.status_hint as Status]?.label}</span>}</span>
              </div>
              <div className="row find-actions">
                <button type="button" className="btn primary sm" disabled={busy === f.id} onClick={() => add(f)}><Icon name="plus" size={14} />Add</button>
                <button type="button" className="btn sm hide-xs" disabled={busy === f.id} onClick={() => ignore(f)}>Ignore</button>
                <MenuButton label="More" align="right" width={230} items={[
                  { label: 'Change the name, then add', icon: 'edit', onSelect: () => setNamed(f) },
                  ...(f.mail_url ? [{ label: 'Open the email', icon: 'external', onSelect: () => window.open(f.mail_url, '_blank', 'noopener') }] : []),
                  { label: 'Ignore', icon: 'x', onSelect: () => ignore(f) },
                ]}><Icon name="more" size={16} /></MenuButton>
              </div>
            </div>))}</div>
        </Card>
      )}

      {done && done.length > 0 && <Card flush title="Already handled" sub="Added, news for a parcel you follow, or ignored">
        <div className="list">{done.map(f => (
          <div key={f.id} className="list-row is-quiet">
            <span className="list-ico"><Icon name={f.state === 'dismissed' ? 'x' : f.state === 'updated' ? 'refresh' : 'check'} size={15} /></span>
            <div className="grow row-main">
              <span className="row-title"><span className="ellipsis">{f.subject}</span></span>
              <span className="muted small ellipsis">{[f.shop || f.from_name, f.state === 'dismissed' ? 'ignored' : f.state === 'updated' ? 'news for a parcel you follow' : 'added', ago(f.received_at)].filter(Boolean).join(' · ')}</span>
            </div>
            {f.parcel_id ? <button type="button" className="btn ghost sm" onClick={() => navigate(`/parcels/${f.parcel_id}`)}>Open</button>
              : f.state === 'dismissed' ? <button type="button" className="btn ghost sm" disabled={busy === f.id} onClick={() => add(f)}>Add anyway</button> : null}
          </div>))}</div>
      </Card>}
      {named && <NameModal f={named} onClose={() => setNamed(null)} onAdd={b => { const f = named; setNamed(null); add(f, b) }} />}
    </div>
  )
}

function NameModal({ f, onClose, onAdd }: { f: Find; onClose: () => void; onAdd: (b: { title: string; shop: string }) => void }) {
  const [title, setTitle] = useState(f.title)
  const [shop, setShop] = useState(f.shop)
  return (
    <Modal title="Add this parcel" onClose={onClose} size="sm">
      <form className="stack form-grid" onSubmit={e => { e.preventDefault(); onAdd({ title, shop }) }}>
        <Field label="What is it?"><input autoFocus value={title} onChange={e => setTitle(e.target.value)} maxLength={200} /></Field>
        <Field label="Shop"><input value={shop} onChange={e => setShop(e.target.value)} maxLength={120} /></Field>
        <p className="muted small">Tracking number <span className="mono">{f.tracking_number}</span>{f.carrier_name ? ` · ${f.carrier_name}` : ''}</p>
        <div className="form-actions"><span className="grow" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary">Add parcel</button></div>
      </form>
    </Modal>
  )
}
