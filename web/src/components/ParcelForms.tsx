// Add or change a parcel, and add a step to its journey by hand.
import React, { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange } from '../lib/data'
import { STATUS, STATUS_ORDER, followText, moneyInput, parseMoney } from '../lib/parcels'
import type { Carrier, Detect, Parcel, Status } from '../lib/types'
import { Field, Modal, Segmented, errMsg, useAsync, useDebounce, useToast } from './ui'
import { Select } from './Select'
import { Icon } from './icons'

function pad(n: number) { return String(n).padStart(2, '0') }
function localNow(): string {
  const d = new Date()
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// carriers and shops change rarely: loaded once per page view
let carriersCache: Carrier[] | null = null
export function useCarriers() {
  const { data } = useAsync(async () => carriersCache || (carriersCache = (await api.get<{ carriers: Carrier[] }>(`${V1}/carriers`)).carriers), [])
  return data || []
}

export function ParcelModal({ parcel, initialNumber = '', onClose, onSaved }: { parcel?: Parcel; initialNumber?: string; onClose: () => void; onSaved: (p: Parcel) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const editing = !!parcel
  const carriers = useCarriers()
  const { data: shops } = useAsync(() => api.get<{ shops: string[] }>(`${V1}/shops`).then(r => r.shops), [])
  const [f, setF] = useState(() => ({
    tracking_number: parcel?.tracking_number || initialNumber, title: parcel?.title || '', direction: parcel?.direction || 'in' as 'in' | 'out',
    shop: parcel?.shop || '', contact_name: parcel?.contact_name || '', carrier: parcel?.carrier || '', order_number: parcel?.order_number || '',
    ordered_on: parcel?.ordered_on || '', price: moneyInput(parcel?.price), extra_cost: moneyInput(parcel?.extra_cost), currency: parcel?.currency || settings?.currency || 'EUR',
    expected_on: parcel?.expected_manual ? parcel.expected_on : '', expected_until: parcel?.expected_manual ? parcel.expected_until : '',
    return_by: parcel?.return_by || '', notes: parcel?.notes || '',
  }))
  const [more, setMore] = useState(editing)
  const [busy, setBusy] = useState(false)
  const [dup, setDup] = useState<{ id: number; title?: string } | null>(null)
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF(x => ({ ...x, [k]: v }))

  // what the number says, while it is typed
  const num = useDebounce(f.tracking_number.trim(), 400)
  const [det, setDet] = useState<Detect | null>(null)
  useEffect(() => {
    if (num.replace(/[^A-Za-z0-9]/g, '').length < 6) { setDet(null); return }
    let live = true
    api.post<Detect>(`${V1}/detect`, { number: num }).then(d => { if (live) setDet(d) }).catch(() => {})
    return () => { live = false }
  }, [num])
  const existing = det?.existing_id && det.existing_id !== parcel?.id ? det : null

  const carrierOptions = useMemo(() => carriers.map(c => ({ value: c.key, label: c.name, hint: c.country, group: c.key.startsWith('post-') ? 'National posts' : c.key === 'other' ? 'Other' : 'Delivery companies' })), [carriers])
  const guessed = det?.carrier_name ? `${det.carrier_name} (from the number)` : 'Guess from the number'

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!f.tracking_number.trim() && !f.title.trim()) { toast('Add a tracking number, or at least say what is coming', 'err'); return }
    const price = parseMoney(f.price), extra = parseMoney(f.extra_cost)
    if (f.price.trim() && price === null) { toast('The price is a number like 24.99', 'err'); return }
    if (f.extra_cost.trim() && extra === null) { toast('The extra cost is a number like 4.50', 'err'); return }
    const body: Record<string, unknown> = {
      tracking_number: f.tracking_number, title: f.title, direction: f.direction, carrier: f.carrier, order_number: f.order_number, ordered_on: f.ordered_on,
      price, extra_cost: extra, currency: f.currency, return_by: f.return_by, notes: f.notes,
    }
    if (f.direction === 'out') { body.contact_name = f.contact_name; body.shop = f.shop } else body.shop = f.shop
    if (f.expected_on || f.expected_until || (editing && parcel?.expected_manual)) { body.expected_on = f.expected_on; body.expected_until = f.expected_until }
    if (!editing) for (const k of Object.keys(body)) if (body[k] === '' || body[k] === null) delete body[k]
    setBusy(true); setDup(null)
    try {
      const p = editing ? await api.put<Parcel>(`${V1}/parcels/${parcel!.id}`, body) : await api.post<Parcel>(`${V1}/parcels`, body)
      toast(editing ? 'Saved' : p.can_check ? 'Added — checking where it is…' : 'Added')
      announceChange()
      onSaved(p)
    } catch (err) {
      if (err instanceof ApiError && err.status === 409 && err.data.id) setDup({ id: Number(err.data.id) })
      else toast(errMsg(err), 'err')
    } finally { setBusy(false) }
  }

  return (
    <Modal title={editing ? 'Change the parcel' : 'Add a parcel'} onClose={onClose} className="parcel-modal">
      <form onSubmit={submit} className="stack form-grid">
        <Field label="Tracking number" help={editing ? undefined : 'Paste it from the shipping email or the shop. No number? Leave it empty and give the parcel a name.'}>
          <input value={f.tracking_number} onChange={e => set('tracking_number', e.target.value)} autoFocus={!editing} placeholder="e.g. LP00123456789012" autoComplete="off" spellCheck={false} className="mono-input" />
        </Field>
        <div className={`detect-line small ${det ? '' : 'is-empty'}`} aria-live="polite">
          {existing ? <span className="warn-text"><Icon name="alert" size={13} /> You already follow this number: <Link to={`/parcels/${existing.existing_id}`} onClick={onClose}>{existing.existing_title}</Link></span>
            : det ? <><span className="detect-carrier"><Icon name="package" size={13} />{det.carrier_name || 'Carrier not recognised'}</span><span className="muted">{followText(det.check_with, det.valid)}</span></>
            : <span className="muted">{f.tracking_number.trim() ? ' ' : followText(undefined, false)}</span>}
        </div>
        {dup && <div className="callout warn"><Icon name="alert" size={15} /><div>You already follow this tracking number. <Link to={`/parcels/${dup.id}`} onClick={onClose}>Open that parcel</Link></div></div>}

        <Field label="What is it?" help="Your own name for it: “Running shoes”, “Birthday present for Ana”.">
          <input value={f.title} onChange={e => set('title', e.target.value)} placeholder={f.direction === 'out' ? 'Parcel to …' : f.shop ? `Order from ${f.shop}` : 'Parcel'} maxLength={200} />
        </Field>
        <Field group label="Which way">
          <Segmented value={f.direction} onChange={v => set('direction', v)} options={[{ value: 'in', label: 'Coming to me' }, { value: 'out', label: 'Sent by me' }]} />
        </Field>
        <div className="two-col tight">
          {f.direction === 'in' ? (
            <Field group label="Shop">
              <Select ariaLabel="Shop" value={f.shop} onChange={v => set('shop', v)} placeholder="Where you bought it" clearLabel="No shop"
                options={(shops || []).map(s => ({ value: s, label: s }))} onCreate={l => l} createLabel="Use" />
            </Field>
          ) : (
            <Field label="Who gets it"><input value={f.contact_name} onChange={e => set('contact_name', e.target.value)} placeholder="Name" maxLength={120} /></Field>
          )}
          <Field group label="Carrier">
            <Select ariaLabel="Carrier" value={f.carrier} onChange={v => set('carrier', v)} placeholder={guessed} clearLabel={guessed} options={carrierOptions} />
          </Field>
        </div>

        {!more ? (
          <button type="button" className="link-btn more-toggle" onClick={() => setMore(true)}><Icon name="chevron-down" size={14} />Order number, price, expected day, return deadline, notes</button>
        ) : <>
          <div className="two-col tight">
            <Field label="Order number"><input value={f.order_number} onChange={e => set('order_number', e.target.value)} maxLength={80} /></Field>
            <Field label="Ordered on"><input type="date" value={f.ordered_on} onChange={e => set('ordered_on', e.target.value)} /></Field>
          </div>
          <div className="price-row">
            <Field label="Price" className="grow"><input inputMode="decimal" value={f.price} onChange={e => set('price', e.target.value)} placeholder="0.00" /></Field>
            <Field label="Customs, postage" className="grow"><input inputMode="decimal" value={f.extra_cost} onChange={e => set('extra_cost', e.target.value)} placeholder="0.00" /></Field>
            <Field label="Currency" className="cur-field"><input value={f.currency} onChange={e => set('currency', e.target.value.toUpperCase())} maxLength={3} /></Field>
          </div>
          <div className="two-col tight">
            <Field label="Expected on" help="Leave empty to take the carrier's estimate."><input type="date" value={f.expected_on} onChange={e => set('expected_on', e.target.value)} /></Field>
            <Field label="…or by (optional)" help="For a range like 9–14 Nov."><input type="date" value={f.expected_until} onChange={e => set('expected_until', e.target.value)} /></Field>
          </div>
          <Field label="Last day to send it back" help="You get a reminder three days before and on the day."><input type="date" value={f.return_by} onChange={e => set('return_by', e.target.value)} /></Field>
          <Field label="Notes"><textarea rows={3} value={f.notes} onChange={e => set('notes', e.target.value)} /></Field>
        </>}
        <div className="form-actions"><span className="grow" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Saving…' : editing ? 'Save' : 'Add parcel'}</button></div>
      </form>
    </Modal>
  )
}

/** A step added by hand: "Picked up from the neighbour", "Customs charged 12 KM". */
export function StepModal({ parcel, onClose, onSaved }: { parcel: Parcel; onClose: () => void; onSaved: (p: Parcel) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ description: '', status: '' as Status | '', location: '', at: localNow() })
  const [busy, setBusy] = useState(false)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!f.description.trim()) { toast('Say what happened', 'err'); return }
    setBusy(true)
    try {
      const at = new Date(f.at)
      const p = await api.post<Parcel>(`${V1}/parcels/${parcel.id}/events`, { description: f.description, status: f.status, location: f.location, at: Number.isNaN(at.getTime()) ? '' : at.toISOString() })
      toast('Step added'); announceChange(); onSaved(p)
    } catch (err) { toast(errMsg(err), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Add a step" onClose={onClose} size="sm">
      <form onSubmit={submit} className="stack form-grid">
        <Field label="What happened"><input autoFocus value={f.description} onChange={e => setF({ ...f, description: e.target.value })} placeholder="Picked up from the neighbour" maxLength={300} /></Field>
        <Field group label="Status" help="Left on “from the words”, Pocket Parcels reads it from what you wrote.">
          <Select ariaLabel="Status" value={f.status} onChange={v => setF({ ...f, status: v as Status })} placeholder="From the words" clearLabel="From the words"
            options={STATUS_ORDER.map(s => ({ value: s, label: STATUS[s].label }))} />
        </Field>
        <div className="two-col tight">
          <Field label="Where (optional)"><input value={f.location} onChange={e => setF({ ...f, location: e.target.value })} maxLength={120} /></Field>
          <Field label="When"><input type="datetime-local" value={f.at} onChange={e => setF({ ...f, at: e.target.value })} /></Field>
        </div>
        <div className="form-actions"><span className="grow" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Saving…' : 'Add step'}</button></div>
      </form>
    </Modal>
  )
}
