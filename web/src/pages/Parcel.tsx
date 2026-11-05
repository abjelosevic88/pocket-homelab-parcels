// One parcel: where it is, its journey, the facts, sending it back, and what it is in the other Pocket apps.
import React, { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useOnChange } from '../lib/data'
import { ago, fmtDate, fmtDateTime, fmtISOTime } from '../lib/format'
import { STATUS, arrivesText, deliveredText, isActive, localDay, money, returnText, sourceName, spacedNumber, followText } from '../lib/parcels'
import type { AppSlug, Attachment, ContactItem, DocumentItem, MoneyTx, Parcel, ParcelEvent, ReturnState, Wish } from '../lib/types'
import { Card, Confirm, CoreHint, Field, Loading, MenuButton, Segmented, copyText, errMsg, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Stepper } from '../components/Rows'
import { ParcelModal, StepModal } from '../components/ParcelForms'
import { PickModal, linkURL } from '../components/Pickers'
import { useCrumb } from '../components/Layout'

const APP_NAME: Record<AppSlug, string> = { mail: 'Pocket Mail', money: 'Pocket Money', wishlist: 'Pocket Wishlist', inventory: 'Pocket Inventory', calendar: 'Pocket Calendar', tasks: 'Pocket Tasks', contacts: 'Pocket Contacts', documents: 'Pocket Documents' }
const PROVIDER: Record<string, string> = { '17track': '17TRACK', cainiao: 'Cainiao' }
const ADDED_BY: Record<string, string> = { mail: 'from your email', manual: 'by you', import: 'from an import', demo: 'demo data' }

export default function ParcelPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { data: p, setData, reload, error } = useAsync(() => api.get<Parcel>(`${V1}/parcels/${id}`), [id])
  useOnChange(reload)
  useCrumb(p?.title)
  const [edit, setEdit] = useState(false)
  const [step, setStep] = useState(false)
  const [del, setDel] = useState(false)
  const [busy, setBusy] = useState('')
  if (error) return <div className="callout danger"><Icon name="alert" size={15} />{error === 'not found' ? 'This parcel does not exist, or it is someone else’s.' : error}</div>
  if (!p) return <Loading />
  const active = isActive(p)
  const run = async (key: string, fn: () => Promise<Parcel | { parcel: Parcel }>, ok?: string) => {
    setBusy(key)
    try { const r = await fn(); setData('parcel' in r ? r.parcel : r); if (ok) toast(ok); announceChange() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }
  const check = async () => {
    setBusy('check')
    try {
      const r = await api.post<{ parcel: Parcel; changed: boolean; error?: string }>(`${V1}/parcels/${p.id}/check`)
      setData(r.parcel)
      if (r.error) toast(r.error, 'err'); else toast(r.changed ? 'New news' : 'No news since the last check')
      if (r.changed) announceChange()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }
  const who = p.direction === 'out' ? (p.contact_name ? `To ${p.contact_name}` : 'Sent by you') : p.shop
  const sub = [who, p.carrier_name, p.order_number && `Order ${p.order_number}`].filter(Boolean).join(' · ')
  const when = active ? arrivesText(p) : deliveredText(p)
  return (
    <div className="stack loose">
      <div className="detail-head">
        <span className={`detail-ico st-${p.status}${p.stale && active ? ' st-stale' : ''}`}><Icon name={STATUS[p.status]?.icon || 'package'} size={22} /></span>
        <div className="grow detail-title">
          <h1><span className="ellipsis">{p.title}</span>{p.direction === 'out' && <span className="badge">sent</span>}{p.archived && <span className="badge">archived</span>}</h1>
          {sub && <p className="muted">{sub}</p>}
        </div>
        <div className="row toolbar">
          {p.can_check && <button type="button" className="btn" onClick={check} disabled={busy === 'check'}><Icon name="refresh" size={15} className={busy === 'check' ? 'spin' : ''} /><span className="hide-xs">Check now</span></button>}
          {active && <button type="button" className="btn primary" onClick={() => run('here', () => api.post(`${V1}/parcels/${p.id}/delivered`), 'Marked delivered')} disabled={!!busy}><Icon name="check" size={15} />It’s here</button>}
          <button type="button" className="btn" onClick={() => setEdit(true)}><Icon name="edit" size={15} /><span className="hide-xs">Edit</span></button>
          <MenuButton label="More" align="right" width={210} items={[
            { label: 'Add a step', icon: 'plus', onSelect: () => setStep(true) },
            { label: p.archived ? 'Back to the list' : 'Archive', icon: 'archive', onSelect: () => run('arch', () => api.put(`${V1}/parcels/${p.id}`, { archived: !p.archived }), p.archived ? 'Back in the list' : 'Archived') },
            'sep',
            { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(true) },
          ]}><Icon name="more" size={18} /></MenuButton>
        </div>
      </div>

      <Card className="status-card">
        <Stepper p={p} />
        <div className="status-main">
          <div className="grow">
            <div className="status-big">{p.stale && active ? 'No news for a while' : p.status_label}</div>
            {p.status_text && <p className="status-text">{p.status_text}{p.location && <span className="muted"> · {p.location}</span>}</p>}
            <p className="muted small">{[when, p.status_at && `last news ${ago(p.status_at)}`].filter(Boolean).join(' · ') || 'No news yet.'}</p>
          </div>
        </div>
        {p.tracking_number && <div className="track-line">
          <span className="track-no mono" title="Tracking number">{spacedNumber(p.tracking_number)}</span>
          <button type="button" className="btn ghost sm" onClick={async () => toast(await copyText(p.tracking_number) ? 'Copied' : 'Could not copy')}><Icon name="copy" size={14} />Copy</button>
          {p.tracking_url && <a className="btn ghost sm" href={p.tracking_url} target="_blank" rel="noreferrer"><Icon name="external" size={14} />Open on {p.carrier_name && p.tracking_url.indexOf('17track') < 0 ? `the ${p.carrier_name} site` : '17TRACK'}</a>}
        </div>}
        {p.check_error ? <div className="callout warn mt"><Icon name="alert" size={15} /><span>{p.check_error}</span></div>
          : active && <p className="muted small check-line">{p.can_check ? `${followText(p.check_with, true)}${p.checked_at ? ` Last checked ${ago(p.checked_at)}.` : ''}` : followText(p.check_with, !!p.tracking_number)}{!p.can_check && p.tracking_number && <> <Link to="/settings/tracking">Tracking settings</Link></>}</p>}
      </Card>

      <div className="home-grid">
        <Card title="Journey" sub={p.events?.length ? `${p.events.length} ${p.events.length === 1 ? 'step' : 'steps'}, newest first` : undefined} actions={<button type="button" className="btn ghost sm" onClick={() => setStep(true)}><Icon name="plus" size={14} />Add a step</button>}>
          <Timeline p={p} onChanged={reload} />
        </Card>
        <div className="stack">
          <Facts p={p} />
          {(p.return_by || (p.direction === 'in' && p.status === 'delivered')) && <ReturnCard p={p} onSaved={setData} onEdit={() => setEdit(true)} />}
          <LinkedApps p={p} onSaved={setData} />
          <Papers p={p} onChanged={reload} />
          {p.notes && <Card title="Notes"><p className="notes">{p.notes}</p></Card>}
        </div>
      </div>
      {edit && <ParcelModal parcel={p} onClose={() => setEdit(false)} onSaved={x => { setEdit(false); setData(x) }} />}
      {step && <StepModal parcel={p} onClose={() => setStep(false)} onSaved={x => { setStep(false); setData(x) }} />}
      {del && <Confirm title={`Delete “${p.title}”?`} confirmLabel="Delete" message="Its journey and papers go too. Things it made in other apps (a task, an item on your shelves, a transaction) stay there." onCancel={() => setDel(false)}
        onConfirm={async () => { setDel(false); try { await api.del(`${V1}/parcels/${p.id}`); announceChange(); toast('Deleted'); navigate('/parcels') } catch (e) { toast(errMsg(e), 'err') } }} />}
    </div>
  )
}

function Timeline({ p, onChanged }: { p: Parcel; onChanged: () => void }) {
  const { toast } = useToast()
  const evs = p.events || []
  if (evs.length === 0) return <p className="muted">{p.tracking_number ? 'No steps yet. They appear here as the carrier, your email or you report them.' : 'No steps yet. Add them yourself, or mark it delivered when it arrives.'}</p>
  let lastDay = ''
  return (
    <ol className="timeline">
      {evs.map((e: ParcelEvent, i) => {
        const day = localDay(e.at)
        const head = day !== lastDay ? fmtDate(day) : ''
        lastDay = day
        return (
          <React.Fragment key={e.id}>
            {head && <li className="tl-day">{head}</li>}
            <li className={`tl-item st-${e.status} ${i === 0 ? 'tl-latest' : ''}`}>
              <span className="tl-dot" />
              <div className="grow">
                <div className="tl-text">{e.description}</div>
                <div className="muted small tl-meta">
                  <span>{fmtISOTime(e.at)}</span>
                  {e.location && <span>· {e.location}</span>}
                  <span className={`src-chip src-${e.source}`}>{sourceName(e.source)}</span>
                  {e.status !== 'in_transit' && <span>· {STATUS[e.status]?.label}</span>}
                </div>
              </div>
              {e.source === 'manual' && <button type="button" className="btn ghost icon-only sm" aria-label="Remove this step" title="Remove this step"
                onClick={async () => { try { await api.del(`${V1}/events/${e.id}`); onChanged(); toast('Step removed') } catch (err) { toast(errMsg(err), 'err') } }}><Icon name="x" size={14} /></button>}
            </li>
          </React.Fragment>
        )
      })}
    </ol>
  )
}

function Facts({ p }: { p: Parcel }) {
  const rows: [string, React.ReactNode][] = []
  if (p.shop && p.direction === 'in') rows.push(['Shop', p.shop])
  if (p.direction === 'out') rows.push(['Sent to', p.contact_name || '—'])
  if (p.carrier_name) rows.push(['Carrier', p.carrier_name])
  if (p.order_number) rows.push(['Order number', <span key="o" className="mono">{p.order_number}</span>])
  if (p.ordered_on) rows.push([p.direction === 'out' ? 'Sent on' : 'Ordered on', fmtDate(p.ordered_on)])
  if (p.price != null) rows.push(['Price', money(p.price, p.currency)])
  if (p.extra_cost != null) rows.push(['Customs, postage', money(p.extra_cost, p.currency)])
  if (p.expected_on) rows.push(['Expected', `${fmtDate(p.expected_on)}${p.expected_until ? ` – ${fmtDate(p.expected_until)}` : ''}${p.expected_manual ? ' (your date)' : ''}`])
  if (p.delivered_at) rows.push(['Delivered', fmtDateTime(p.delivered_at)])
  if (p.days != null) rows.push([p.status === 'delivered' ? 'Took' : 'On the way for', p.days === 1 ? '1 day' : `${p.days} days`])
  rows.push(['Added', `${fmtDate(localDay(p.created_at))} · ${ADDED_BY[p.source] || `by ${sourceName(p.source)}`}`])
  if (p.provider) rows.push(['Followed by', PROVIDER[p.provider] || p.provider])
  return <Card title="Details"><dl className="facts">{rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}</dl></Card>
}

function ReturnCard({ p, onSaved, onEdit }: { p: Parcel; onSaved: (p: Parcel) => void; onEdit: () => void }) {
  const { toast } = useToast()
  const set = async (state: ReturnState) => { try { onSaved(await api.put<Parcel>(`${V1}/parcels/${p.id}`, { return_state: state })); announceChange(); toast(state === 'returning' ? 'Marked to send back' : state === 'returned' ? 'Marked sent back' : 'Keeping it') } catch (e) { toast(errMsg(e), 'err') } }
  if (!p.return_by) return (
    <Card title="Send back">
      <p className="muted small">Might it go back? Add the last day to return it and you get a reminder before it passes.</p>
      <button type="button" className="btn sm mt" onClick={onEdit}><Icon name="calendar" size={14} />Set the return deadline</button>
    </Card>
  )
  const open = p.return_state !== 'returned' && p.return_state !== 'keeping'
  return (
    <Card title="Send back">
      <p className={open ? 'warn-text return-left' : 'muted return-left'}>{returnText(p)}</p>
      <p className="muted small">Last day: {fmtDate(p.return_by)}</p>
      <div className="mt"><Segmented size="sm" value={p.return_state || 'undecided'} onChange={v => set(v === 'undecided' ? '' : v as ReturnState)} label="Return"
        options={[{ value: 'undecided', label: 'Not sure' }, { value: 'keeping', label: 'Keep it' }, { value: 'returning', label: 'Send it back' }, { value: 'returned', label: 'Sent back' }]} /></div>
    </Card>
  )
}

/** One line in "Other Pocket apps": what the parcel is there, or the button to make it so. */
function AppLine({ icon, app, ready, children }: { icon: string; app: AppSlug; ready: boolean; children: React.ReactNode }) {
  const { settings } = useApp()
  const off = settings?.[`${app}_mode`] === 'off'
  return (
    <div className={`app-line ${ready ? '' : 'is-off'}`}>
      <span className="list-ico sm"><Icon name={icon} size={14} /></span>
      <div className="grow app-line-body">
        {children}
        {!ready && <span className="muted small app-line-why">{off ? `${APP_NAME[app]} is switched off. ` : `${APP_NAME[app]} is not connected${settings?.[`${app}_detail`] ? ` (${settings[`${app}_detail`]})` : ''}. `}<Link to="/settings/apps">Connected apps</Link></span>}
      </div>
    </div>
  )
}

function LinkedApps({ p, onSaved }: { p: Parcel; onSaved: (p: Parcel) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [pick, setPick] = useState<'' | 'wish' | 'money' | 'contact'>('')
  const [busy, setBusy] = useState('')
  if (!settings) return null
  const ok = (a: AppSlug) => !!settings[`${a}_configured`]
  const coreOn = settings.core_configured && settings.core_registered !== false
  const anyOff = (['wishlist', 'money', 'inventory', 'tasks', 'mail'] as AppSlug[]).some(a => !ok(a))
  const post = async (key: string, path: string, body: unknown, msg: string) => {
    setBusy(key)
    try {
      const r = await api.post<Parcel | { parcel: Parcel; warning?: string }>(`${V1}/parcels/${p.id}/${path}`, body)
      onSaved('parcel' in r ? r.parcel : r); announceChange()
      if ('warning' in r && r.warning) toast(r.warning, 'err'); else toast(msg)
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }
  const put = async (body: Record<string, unknown>, msg: string) => { try { onSaved(await api.put<Parcel>(`${V1}/parcels/${p.id}`, body)); announceChange(); toast(msg) } catch (e) { toast(errMsg(e), 'err') } }
  const ext = (url: string | undefined, label: React.ReactNode) => url ? <a href={url} target="_blank" rel="noreferrer" className="app-link">{label}<Icon name="external" size={12} /></a> : <span>{label}</span>
  const delivered = p.status === 'delivered'
  return (
    <Card title="Other Pocket apps">
      <div className="app-lines">
        {p.direction === 'in' && <AppLine icon="gift" app="wishlist" ready={ok('wishlist') || !!p.wish_id}>
          {p.wish_id ? <span className="app-line-main">{ext(p.wish_url, <>Wish: <b>{p.wish_title || 'a wish'}</b></>)}<button type="button" className="link-btn small" onClick={() => post('wish', 'wish', { wish_id: 0 }, 'No longer linked to the wish')}>Unlink</button></span>
            : <span className="app-line-main"><span className="muted">Is this something from your wishlist?</span><button type="button" className="btn sm" disabled={!ok('wishlist')} onClick={() => setPick('wish')}>Choose the wish</button></span>}
        </AppLine>}
        <AppLine icon="wallet" app="money" ready={ok('money') || !!p.money_tx_id}>
          {p.money_tx_id ? <span className="app-line-main">{ext(p.money_url, <>Paid: <b>{p.money_tx_label || 'the payment'}</b></>)}<button type="button" className="link-btn small" onClick={() => post('money', 'money', { tx_id: 0 }, 'Payment unlinked')}>Unlink</button></span>
            : <span className="app-line-main"><span className="muted">The payment for it in Pocket Money</span><button type="button" className="btn sm" disabled={!ok('money')} onClick={() => setPick('money')}>Find the payment</button></span>}
          {p.extra_cost != null && p.extra_cost > 0 && (p.extra_tx_id ? <span className="app-line-main small">{ext(p.extra_url, <>Customs and postage booked ({money(p.extra_cost, p.currency)})</>)}</span>
            : <span className="app-line-main small"><span className="muted">Customs and postage: {money(p.extra_cost, p.currency)}</span><button type="button" className="btn sm" disabled={!ok('money') || busy === 'extra'} onClick={() => post('extra', 'money', { book_extra: true }, 'Booked in Pocket Money')}>Book it</button></span>)}
        </AppLine>
        {p.direction === 'in' && <AppLine icon="archive" app="inventory" ready={ok('inventory') || !!p.inventory_item_id}>
          {p.inventory_item_id ? <span className="app-line-main">{ext(p.item_url, <>On your shelves in Pocket Inventory</>)}</span>
            : <span className="app-line-main"><span className="muted">{delivered ? 'Keep it in your inventory' : 'Once it arrives: put it on your shelves'}</span><button type="button" className="btn sm" disabled={!ok('inventory') || !delivered || busy === 'inv'} onClick={() => post('inv', 'inventory', {}, 'Added to Pocket Inventory')}>Put on my shelves</button></span>}
        </AppLine>}
        <AppLine icon="check-square" app="tasks" ready={ok('tasks') || !!p.task_id || !!p.return_task_id}>
          {p.direction === 'in' && active(p) && (p.task_id ? <span className="app-line-main">{ext(p.task_url, <>Pick-up task in Pocket Tasks</>)}</span>
            : <span className="app-line-main"><span className="muted">{p.status === 'pickup' ? 'Waiting at a pick-up point' : 'A task to pick it up'}</span><button type="button" className="btn sm" disabled={!ok('tasks') || busy === 'task'} onClick={() => post('task', 'task', { kind: 'pickup' }, 'Added to Pocket Tasks')}>Add a pick-up task</button></span>)}
          {p.return_by && (p.return_task_id ? <span className="app-line-main">{ext(p.return_task_url, <>Send-back task in Pocket Tasks</>)}</span>
            : <span className="app-line-main"><span className="muted">A task to send it back</span><button type="button" className="btn sm" disabled={!ok('tasks') || busy === 'rtask'} onClick={() => post('rtask', 'task', { kind: 'return' }, 'Added to Pocket Tasks')}>Add a send-back task</button></span>)}
          {!(p.direction === 'in' && active(p)) && !p.return_by && <span className="app-line-main muted">Tasks to pick it up or send it back appear here when they apply.</span>}
        </AppLine>
        {p.direction === 'out' && <AppLine icon="user" app="contacts" ready={ok('contacts') || !!p.contact_id}>
          {p.contact_id ? <span className="app-line-main">{ext(p.contact_url, <>For <b>{p.contact_name}</b></>)}<button type="button" className="link-btn small" onClick={() => put({ contact_id: 0 }, 'Contact unlinked')}>Unlink</button></span>
            : <span className="app-line-main"><span className="muted">Who gets it, from your contacts</span><button type="button" className="btn sm" disabled={!ok('contacts')} onClick={() => setPick('contact')}>Choose a contact</button></span>}
        </AppLine>}
        <AppLine icon="mail" app="mail" ready={ok('mail') || !!p.mail_url}>
          {p.mail_url ? <span className="app-line-main">{ext(p.mail_url, <>The shipping email in Pocket Mail</>)}</span>
            : <span className="app-line-main muted">{p.source === 'mail' ? 'Found in your email' : 'Shipping emails about it update the journey by themselves.'}</span>}
        </AppLine>
        {settings.calendar_sync && ok('calendar') && p.expected_on && active(p) && <div className="app-line"><span className="list-ico sm"><Icon name="calendar" size={14} /></span><span className="grow muted small">On your calendar on {fmtDate(p.expected_on)}{p.return_by ? `, and the return deadline on ${fmtDate(p.return_by)}` : ''}.</span></div>}
      </div>
      {anyOff && !coreOn && <CoreHint className="mt" />}
      {pick === 'wish' && <PickModal<Wish> title="Which wish is this?" icon="gift" path={q => linkURL('wishlist', { q })} initialQ="" empty="No open wishes match."
        note={settings.wishlist_bought && <p className="muted small">Pocket Wishlist marks the wish bought.</p>}
        render={w => ({ label: w.title, sub: [w.store, w.status].filter(Boolean).join(' · '), side: w.price ? money(w.price, w.currency) : '' })}
        onPick={async w => { setPick(''); await post('wish', 'wish', { wish_id: w.id }, `Linked to “${w.title}”`) }} onClose={() => setPick('')} />}
      {pick === 'money' && <PickModal<MoneyTx> title="Find the payment" icon="wallet" path={q => linkURL('money/transactions', { q, near: q ? '' : p.ordered_on || localDay(p.created_at) })} initialQ={p.shop}
        empty="No expense matches. Try another word, or clear the search to see the days around the order."
        render={t => ({ label: t.payee || t.description || 'Expense', sub: [fmtDate(t.date), t.payee && t.description].filter(Boolean).join(' · '), side: money(Math.abs(t.amount), t.currency || p.currency) })}
        onPick={async t => { setPick(''); await post('money', 'money', { tx_id: t.id }, 'Payment linked') }} onClose={() => setPick('')} />}
      {pick === 'contact' && <PickModal<ContactItem> title="Who gets it?" icon="user" path={q => linkURL('contacts', { q })} initialQ={p.contact_name} placeholder="Type a name…" empty="Type a name to find a contact."
        render={c => ({ label: c.fn || c.org, sub: [c.org !== c.fn ? c.org : '', (c.phones || [])[0]].filter(Boolean).join(' · ') })}
        onPick={async c => { setPick(''); await put({ contact_id: c.id, contact_name: c.fn || c.org }, `For ${c.fn || c.org}`) }} onClose={() => setPick('')} />}
    </Card>
  )
}

const active = (p: Parcel) => isActive(p)

function Papers({ p, onChanged }: { p: Parcel; onChanged: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [pick, setPick] = useState(false)
  const [link, setLink] = useState<{ url: string; title: string } | null>(null)
  const docsOk = !!settings?.documents_configured
  const atts = p.attachments || []
  const add = async (body: Record<string, unknown>) => { try { await api.post<Attachment>(`${V1}/attachments`, { parcel_id: p.id, ...body }); onChanged(); toast('Added') } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <Card title="Papers" sub="The invoice, customs papers, the return label">
      {atts.length > 0 && <div className="papers">{atts.map(a => (
        <div key={a.id} className="paper">
          <Icon name={a.kind === 'document' ? 'file' : 'link'} size={14} className="muted" />
          {a.url ? <a href={a.url} target="_blank" rel="noreferrer" className="grow ellipsis">{a.title || a.url}</a> : <span className="grow ellipsis">{a.title}</span>}
          <button type="button" className="btn ghost icon-only sm" aria-label="Remove" title="Remove" onClick={async () => { try { await api.del(`${V1}/attachments/${a.id}`); onChanged() } catch (e) { toast(errMsg(e), 'err') } }}><Icon name="x" size={14} /></button>
        </div>))}</div>}
      {link ? (
        <form className="stack" onSubmit={e => { e.preventDefault(); add({ kind: 'url', url: link.url, title: link.title }); setLink(null) }}>
          <Field label="Link"><input autoFocus value={link.url} onChange={e => setLink({ ...link, url: e.target.value })} placeholder="https://…" /></Field>
          <Field label="Name (optional)"><input value={link.title} onChange={e => setLink({ ...link, title: e.target.value })} placeholder="Return label" /></Field>
          <div className="form-actions"><button type="button" className="btn sm" onClick={() => setLink(null)}>Cancel</button><button className="btn primary sm">Add link</button></div>
        </form>
      ) : (
        <div className="row wrap">
          <button type="button" className="btn sm" disabled={!docsOk} onClick={() => setPick(true)} title={docsOk ? '' : 'Connect Pocket Documents in Settings → Connected apps'}><Icon name="file" size={14} />From Pocket Documents</button>
          <button type="button" className="btn sm" onClick={() => setLink({ url: '', title: '' })}><Icon name="link" size={14} />Add a link</button>
        </div>
      )}
      {!docsOk && !link && <p className="muted small mt">{settings?.documents_detail ? `Pocket Documents: ${settings.documents_detail}` : 'Connect Pocket Documents to keep papers from your archive here.'}</p>}
      {pick && <PickModal<DocumentItem> title="Choose a document" icon="file" path={q => linkURL('documents', { q })} initialQ={p.shop || p.title} empty="No document matches."
        render={d => ({ label: d.title || d.original_name, sub: [d.correspondent_name, d.document_type_name, d.created && fmtDate(d.created)].filter(Boolean).join(' · ') })}
        onPick={async d => { setPick(false); await add({ kind: 'document', ref: d.id, title: d.title || d.original_name }) }} onClose={() => setPick(false)} />}
    </Card>
  )
}

