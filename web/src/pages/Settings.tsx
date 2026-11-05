import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange } from '../lib/data'
import type { APIToken, AppSlug, CalendarItem, ConnMode, MoneyAccount, MoneyCategory, Picked, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, plural, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'

type TabId = 'general' | 'tracking' | 'notifications' | 'email' | 'apps' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'package', sub: 'Your currency, dates, when delivered parcels leave the list, and the app title.' },
  { id: 'tracking', label: 'Tracking', icon: 'route', sub: 'What follows your parcels by itself: free Cainiao tracking for AliExpress parcels, and 17TRACK for every other carrier.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'A push when a parcel is out for delivery, ready to pick up or delivered, and a daily reminder for returns.' },
  { id: 'email', label: 'Email', icon: 'mail', sub: 'Tracking numbers found in your Pocket Mail, added for you or offered first.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Pocket Mail, Money, Wishlist, Inventory, Calendar, Tasks, Contacts and Documents.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'Who reads shipping emails the patterns miss: Pocket AI through Core, or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for Homepage widgets, Siri Shortcuts and scripts.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Download the database, export or import your parcels.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span className="spacer" />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'general' && <General />}
            {tab === 'tracking' && <Tracking />}
            {tab === 'notifications' && <Notifications />}
            {tab === 'email' && <EmailTab />}
            {tab === 'apps' && <Integrations />}
            {tab === 'ai' && <AITab />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved'); announceChange() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

const HOURS = Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: `${String(h).padStart(2, '0')}:00` }))
const ARCHIVE = [{ value: '0', label: 'Never' }, { value: '3', label: 'After 3 days' }, { value: '7', label: 'After a week' }, { value: '14', label: 'After two weeks' }, { value: '30', label: 'After a month' }]
const STALE = [{ value: '0', label: 'Never' }, { value: '5', label: 'After 5 days' }, { value: '7', label: 'After a week' }, { value: '10', label: 'After 10 days' }, { value: '14', label: 'After two weeks' }, { value: '21', label: 'After three weeks' }]

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  const opt = (list: { value: string; label: string }[], v: number) => list.some(o => o.value === String(v)) ? list : [...list, { value: String(v), label: `After ${v} days` }]
  return (
    <div className="stack loose">
      <Section title="Parcels" icon="package">
        <SettingRow title="Delivered parcels leave the list" help="They move to the Archive; nothing is deleted. A parcel you are sending back stays until it is sent.">
          <Select ariaLabel="Archive after" value={String(f.auto_archive_days)} onChange={v => setF({ ...f, auto_archive_days: Number(v) })} options={opt(ARCHIVE, f.auto_archive_days)} />
        </SettingRow>
        <SettingRow title="Call a parcel stuck" help="When nothing new was heard for this long, it shows under Problems and in the daily reminder.">
          <Select ariaLabel="Stuck after" value={String(f.stale_days)} onChange={v => setF({ ...f, stale_days: Number(v) })} options={opt(STALE, f.stale_days)} />
        </SettingRow>
        <SettingRow title="Currency for new parcels" help="Each parcel keeps its own currency."><input value={f.currency} onChange={e => setF({ ...f, currency: e.target.value.toUpperCase() })} maxLength={3} className="set-time" /></SettingRow>
      </Section>
      <Section title="Dates" icon="calendar">
        <SettingRow title="Week starts on"><Segmented size="sm" value={f.week_start} onChange={v => setF({ ...f, week_start: v })} options={[{ value: 1, label: 'Monday' }, { value: 6, label: 'Saturday' }, { value: 7, label: 'Sunday' }]} /></SettingRow>
        <SettingRow title="Date format"><Select ariaLabel="Date format" value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select ariaLabel="Time format" value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        {isAdmin && <SettingRow title="App title" help="Shown in the sidebar for everyone on this box."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></SettingRow>}
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Tracking() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>Without either of these, parcels still work: shipping emails from Pocket Mail bring the news, you can add steps yourself, and every parcel links to its carrier's own tracking page.</div></div>
      <Section title="AliExpress and Cainiao — free" icon="globe" actions={<span className={`badge ${f.track_cainiao ? 'ok' : ''}`}>{f.track_cainiao ? 'on' : 'off'}</span>}
        sub="Parcels with a Cainiao number (LP…) and most China Post, Hongkong Post and Singapore Post numbers are followed through Cainiao's public tracking. No account, no key. Pocket Parcels asks only about your own numbers.">
        <SettingRow title="Follow these parcels by themselves"><Switch checked={f.track_cainiao} onChange={v => save({ track_cainiao: v })} label="Cainiao tracking" /></SettingRow>
        <div className="row wrap"><button type="button" className="btn sm" onClick={() => run('cainiao', '', '')}>Test Cainiao</button>{status('cainiao')}</div>
      </Section>
      <Section title="17TRACK — every other carrier" icon="route" actions={<span className={`badge ${f.track17_configured ? 'ok' : ''}`}>{f.track17_configured ? 'key saved' : 'no key'}</span>}
        sub={<>17TRACK knows over 2,000 carriers (DHL, UPS, GLS, DPD, national posts, local couriers). Get a key at <a href="https://api.17track.net" target="_blank" rel="noreferrer">api.17track.net</a>: it starts with a free allowance, and each new tracking number uses one. With a key, 17TRACK follows every parcel, AliExpress ones too.</>}>
        <Field label="API key" help="Stored on the server for you only; shown masked."><input value={f.track17_key} onChange={e => setF({ ...f, track17_key: e.target.value })} autoComplete="off" placeholder="Paste your 17TRACK key" className="mono-input" /></Field>
        <div className="row wrap mt">
          <button type="button" className="btn primary" onClick={() => save({ track17_key: f.track17_key })} disabled={busy}>Save key</button>
          <button type="button" className="btn" onClick={() => run('17track', '', f.track17_key)} disabled={!f.track17_key}>Test and show what is left</button>
          {f.track17_configured && <button type="button" className="btn ghost danger-text" onClick={() => save({ track17_key: '' })}>Remove key</button>}
          {status('17track')}
        </div>
      </Section>
    </div>
  )
}

function Notifications() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const now = async () => { try { const r = await api.post<{ sent: boolean; body: string }>(`${V1}/notify/check`); toast(r.sent ? 'Sent' : r.body ? `Nothing sent (no push URL): ${r.body.split('\n')[0]}` : 'No returns or stuck parcels to tell you about today') } catch (e) { toast((e as Error).message, 'err') } }
  const sw = (k: keyof SettingsT, title: string, help: string) => <SettingRow title={title} help={help}><Switch checked={!!f[k]} onChange={v => setF({ ...f, [k]: v } as SettingsT)} label={title} /></SettingRow>
  return (
    <div className="stack loose">
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Where messages go: Pocket Alerts or an Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://alerts:8000/notify/parcels" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
      </Section>
      <Section title="Right away" icon="truck" sub="One push when a parcel reaches one of these, never twice for the same thing.">
        {sw('notify_out', 'Out for delivery', 'It is on the van today.')}
        {sw('notify_pickup', 'Ready to pick up', 'It waits at a post office, locker or pick-up point.')}
        {sw('notify_delivered', 'Delivered', 'It arrived.')}
        {sw('notify_problems', 'Problems', 'A failed delivery, a customs hold, sent back, or tracking that ended.')}
        {sw('notify_moves', 'Every step', 'Each time it moves (left the warehouse, arrived in the country…). Can be a lot.')}
      </Section>
      <Section title="Once a day" icon="clock" sub="Everything in one message, so it never piles up.">
        <SettingRow title="At" help="Server time."><Select ariaLabel="Hour" value={String(f.notify_hour)} onChange={v => setF({ ...f, notify_hour: Number(v) })} options={HOURS} /></SettingRow>
        {sw('notify_returns', 'Return deadlines', 'Three days before, the day before and on the last day to send something back.')}
        {sw('notify_stale', 'Stuck parcels', `Parcels with no news for ${f.stale_days || 10} days or more (change it under General), at most once a week each.`)}
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        <button type="button" className="btn" onClick={now}><Icon name="refresh" size={15} />Send today's reminders now</button>
        {status('notify')}
      </div>
    </div>
  )
}

function EmailTab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const [scanning, setScanning] = useState(false)
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const scan = async () => {
    setScanning(true)
    try {
      const r = await api.post<{ ok: boolean; error?: string; result: { read: number; found: number; added: number; updated: number } }>(`${V1}/mail/scan`)
      toast(r.ok ? (r.result.read ? `Read ${plural(r.result.read, 'shipping email')}: ${r.result.found} new, ${r.result.added} added, ${r.result.updated} updates` : 'No new shipping emails') : r.error || 'failed', r.ok ? 'ok' : 'err')
      announceChange()
    } catch (e) { toast((e as Error).message, 'err') } finally { setScanning(false) }
  }
  return (
    <div className="stack loose">
      {!f.mail_configured && <div className="callout info"><Icon name="mail" size={15} /><div>Pocket Mail is not connected{f.mail_detail ? ` (${f.mail_detail})` : ''}. Connect it under Connected apps. {!coreOn && <CoreHint />}</div></div>}
      <Section title="Shipping emails" icon="mail" sub="Every half hour Pocket Parcels reads your new receipts and shipping updates in Pocket Mail (never anything else), finds tracking numbers, and turns “delivered” and “ready for pick-up” emails into steps of the parcels you follow.">
        <SettingRow title="Look through new email"><Switch checked={f.mail_scan} disabled={!f.mail_configured} onChange={v => setF({ ...f, mail_scan: v })} label="Look through email" /></SettingRow>
        <SettingRow title="Add found parcels at once" help="Off: they wait under Found in email for you to add or ignore. On: they are followed right away.">
          <Switch checked={f.mail_auto_add} disabled={!f.mail_configured} onChange={v => setF({ ...f, mail_auto_add: v })} label="Add at once" /></SettingRow>
        <SettingRow title="The first look goes back" help="How far back the first look reaches; after that only new email is read.">
          <Select ariaLabel="Days back" value={String(f.mail_days)} disabled={!f.mail_configured} onChange={v => setF({ ...f, mail_days: Number(v) })}
            options={[7, 14, 30, 60, 90].map(n => ({ value: String(n), label: `${n} days` })).concat([7, 14, 30, 60, 90].includes(f.mail_days) ? [] : [{ value: String(f.mail_days), label: `${f.mail_days} days` }])} />
        </SettingRow>
        <p className="muted small">An email the patterns cannot read goes to your AI (Settings → AI) when one is connected; a number it gives is kept only if it appears in the email.</p>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={scan} disabled={!f.mail_configured || scanning}><Icon name="refresh" size={15} />{scanning ? 'Looking…' : 'Look through my email now'}</button>
      </div>
    </div>
  )
}

const APPS: { slug: AppSlug; name: string; icon: string; title: string; sub: string; token: string; port: string }[] = [
  { slug: 'mail', name: 'Pocket Mail', icon: 'mail', title: 'Pocket Mail — parcels from your email', sub: 'Tracking numbers in shipping emails become parcels, and “delivered” or “ready for pick-up” emails update them. Only receipts and shipping updates are read.', token: 'Pocket Mail → Settings → API tokens', port: '8208' },
  { slug: 'money', name: 'Pocket Money', icon: 'wallet', title: 'Pocket Money — what it cost', sub: 'Link an order to its payment, and book customs or postage paid on top as an expense, as you.', token: 'Pocket Money → Settings → API tokens', port: '8178' },
  { slug: 'wishlist', name: 'Pocket Wishlist', icon: 'gift', title: 'Pocket Wishlist — the wish you ordered', sub: 'Say which wish a parcel is; Wishlist marks it bought.', token: 'Pocket Wishlist → Settings → API tokens', port: '8198' },
  { slug: 'inventory', name: 'Pocket Inventory', icon: 'archive', title: 'Pocket Inventory — onto the shelves', sub: 'What arrived goes into your inventory with its price and the day it came.', token: 'Pocket Inventory → Settings → API tokens', port: '8170' },
  { slug: 'calendar', name: 'Pocket Calendar', icon: 'calendar', title: 'Pocket Calendar — delivery days', sub: 'Expected delivery days and return deadlines as all-day events that move when the date changes. Your own events are never touched.', token: 'Pocket Calendar → Settings → API tokens', port: '8190' },
  { slug: 'tasks', name: 'Pocket Tasks', icon: 'check-square', title: 'Pocket Tasks — pick it up, send it back', sub: 'A task when a parcel waits at a pick-up point, and one to send a parcel back before its deadline.', token: 'Pocket Tasks → Settings → API tokens', port: '8184' },
  { slug: 'contacts', name: 'Pocket Contacts', icon: 'users', title: 'Pocket Contacts — who gets it', sub: 'Pick who you send a parcel to from your contacts.', token: 'Pocket Contacts → Settings → API tokens', port: '8191' },
  { slug: 'documents', name: 'Pocket Documents', icon: 'file', title: 'Pocket Documents — the papers', sub: 'Keep the invoice, customs papers or the return label from your archive with a parcel.', token: 'Pocket Documents → Settings → API tokens', port: '8183' },
]

function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { toast } = useToast()
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const eff = (m: ConnMode): ConnMode => !coreOn && m === 'core' ? 'off' : m
  const set = (k: string, v: string) => setF({ ...f, [k]: v } as SettingsT)
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These connections are yours. Through <b>Pocket Core</b> Parcels acts as you in your own Pocket apps with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter.{!coreOn && <CoreHint />}</div></div>
      {APPS.map(a => {
        const mode = f[`${a.slug}_mode`]
        const via = f[`${a.slug}_via`]
        const on = eff(mode) !== 'off'
        return (
          <Section key={a.slug} title={a.title} icon={a.icon} actions={connBadge(eff(mode), via)} sub={a.sub}>
            <Connection app={a.name} mode={mode} via={via} detail={f[`${a.slug}_detail`]} core={coreOn} hasToken={!!f[`${a.slug}_token`]}
              onMode={m => save({ [`${a.slug}_mode`]: m } as Partial<SettingsT>)} onDisconnect={() => save({ [`${a.slug}_url`]: '', [`${a.slug}_token`]: '' } as Partial<SettingsT>)}
              test={<><button type="button" className="btn sm" onClick={() => run(a.slug, f[`${a.slug}_url`], f[`${a.slug}_token`])}>Test connection</button>{status(a.slug)}</>}
              direct={<div className="two-col tight">
                <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={f[`${a.slug}_url`]} onChange={e => set(`${a.slug}_url`, e.target.value)} placeholder={`http://host.docker.internal:${a.port}`} /></Field>
                <Field label="API token" help={a.token}><input value={f[`${a.slug}_token`]} onChange={e => set(`${a.slug}_token`, e.target.value)} autoComplete="off" /></Field>
              </div>} />
            {a.slug === 'mail' && on && <div className="mt">
              <SettingRow title="Look through new email" help="Settings → Email has the rest."><Switch checked={f.mail_scan} disabled={!via} onChange={v => save({ mail_scan: v })} label="Look through email" /></SettingRow>
            </div>}
            {a.slug === 'money' && on && via && <MoneyOptions f={f} setF={setF} save={save} />}
            {a.slug === 'wishlist' && on && <div className="mt">
              <SettingRow title="Mark the wish bought" help="When you link a parcel to a wish, Wishlist marks it bought (and does what you set up there: Money, Inventory, Split).">
                <Switch checked={f.wishlist_bought} disabled={!via} onChange={v => save({ wishlist_bought: v })} label="Mark bought" /></SettingRow>
            </div>}
            {a.slug === 'inventory' && on && <div className="mt">
              <SettingRow title="Add every delivered parcel" help="Off: use “Put on my shelves” on a parcel when you want it there.">
                <Switch checked={f.inventory_auto} disabled={!via} onChange={v => save({ inventory_auto: v })} label="Add delivered parcels" /></SettingRow>
              <SettingRow title="Where they go" help="A location in Pocket Inventory; empty = none.">
                <input value={f.inventory_location} disabled={!via} onChange={e => setF({ ...f, inventory_location: e.target.value })} onBlur={() => save({ inventory_location: f.inventory_location })} placeholder="Hallway" /></SettingRow>
            </div>}
            {a.slug === 'calendar' && on && <div className="mt">
              <SettingRow title="Show deliveries on my calendar" help="An all-day event on each expected delivery day; delivered parcels are taken off.">
                <Switch checked={f.calendar_sync} disabled={!via} onChange={v => save({ calendar_sync: v })} label="Calendar" /></SettingRow>
              {via && <CalendarOptions f={f} save={save} onSync={async () => { const r = await api.post<{ ok: boolean; error?: string; counts: Record<string, number> }>(`${V1}/links/calendar/sync`); toast(r.ok ? `Calendar: ${r.counts.created} added, ${r.counts.moved} moved, ${r.counts.removed} removed` : r.error || 'failed', r.ok ? 'ok' : 'err') }} />}
            </div>}
            {a.slug === 'tasks' && on && <div className="mt">
              <SettingRow title="Pick-up task" help="When a parcel waits at a pick-up point, add “Pick up …” to your tasks for today.">
                <Switch checked={f.tasks_pickup} disabled={!via} onChange={v => save({ tasks_pickup: v })} label="Pick-up task" /></SettingRow>
              <SettingRow title="Send-back task" help="When you decide to send a parcel back, add “Send back …” due the day before its deadline.">
                <Switch checked={f.tasks_returns} disabled={!via} onChange={v => save({ tasks_returns: v })} label="Send-back task" /></SettingRow>
            </div>}
          </Section>
        )
      })}
      <Section title="Other Pocket apps" icon="layers" sub="Parcels offers your parcels to the rest of the box through Pocket Core: Pocket AI answers “what arrives today?”, Pocket Day writes deliveries into your journal, another app can add a parcel from a tracking number. Switched off until you allow them in Pocket Core → Links.">
        <dl className="set-dl">
          <div><dt>parcels:read</dt><dd className="muted">Your parcels, where each one is and when it should arrive, their journeys and returns.</dd></div>
          <div><dt>parcels:write</dt><dd className="muted">Add a parcel from a tracking number and add news about it; only what that app added can be changed by it.</dd></div>
        </dl>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function MoneyOptions({ f, setF, save }: { f: SettingsT; setF: (s: SettingsT) => void; save: (p?: Partial<SettingsT>) => void }) {
  const { data: accts } = useAsync(() => api.get<Picked<MoneyAccount>>(`${V1}/links/money/accounts`), [])
  const { data: cats } = useAsync(() => api.get<Picked<MoneyCategory>>(`${V1}/links/money/categories?account_id=${f.money_account_id || 0}`), [f.money_account_id])
  return (
    <div className="mt">
      <SettingRow title="Account for customs and postage" help="Where “Book it” puts the extra cost.">
        <Select ariaLabel="Account" value={f.money_account_id ? String(f.money_account_id) : ''} onChange={v => { setF({ ...f, money_account_id: Number(v) || 0 }); save({ money_account_id: Number(v) || 0 }) }} clearLabel="Not chosen" placeholder="Choose an account"
          options={(accts?.items || []).map(a => ({ value: String(a.id), label: a.name, hint: a.currency }))} />
      </SettingRow>
      <SettingRow title="Category" help="Optional.">
        <Select ariaLabel="Category" value={f.money_category_id ? String(f.money_category_id) : ''} onChange={v => { setF({ ...f, money_category_id: Number(v) || 0 }); save({ money_category_id: Number(v) || 0 }) }} clearLabel="None" placeholder="None"
          options={(cats?.items || []).map(c => ({ value: String(c.id), label: c.full_name || c.name }))} />
      </SettingRow>
      {accts && !accts.connected && <p className="bad small">{accts.detail}</p>}
    </div>
  )
}

function CalendarOptions({ f, save, onSync }: { f: SettingsT; save: (p?: Partial<SettingsT>) => void; onSync: () => void }) {
  const { data: cals } = useAsync(() => api.get<Picked<CalendarItem>>(`${V1}/links/calendar/calendars`), [])
  if (!f.calendar_sync) return null
  return (
    <>
      <SettingRow title="Return deadlines too" help="The last day to send something back, until you keep it or send it.">
        <Switch checked={f.calendar_returns} onChange={v => save({ calendar_returns: v })} label="Return deadlines on the calendar" /></SettingRow>
      <SettingRow title="Calendar" help="Otherwise your default calendar.">
        <Select ariaLabel="Calendar" value={f.calendar_id ? String(f.calendar_id) : ''} onChange={v => save({ calendar_id: Number(v) || 0 })} clearLabel="My default calendar" placeholder="My default calendar"
          options={(cals?.items || []).map(c => ({ value: String(c.id), label: c.name }))} />
      </SettingRow>
      <div className="row"><button type="button" className="btn sm" onClick={onSync}><Icon name="refresh" size={14} />Update the calendar now</button></div>
    </>
  )
}

function AITab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string }>(`${V1}/ai/status`), [])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="When a shipping email from Pocket Mail has no tracking number the patterns recognise, its subject, sender and text go to the AI, which names the number, shop and item. A number it gives is kept only if it appears in the email. Questions about your parcels go to Pocket AI through the Ask button at the top.">
        {ai && (ai.ready ? <p className="muted small row ai-ready"><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} is ready.</p>
          : <div className="callout info"><Icon name="info" size={15} /><div>No AI connected{ai.detail ? ` (${ai.detail})` : ''}. Emails are still read with patterns.{!coreOn && <CoreHint />}</div></div>)}
      </Section>
      <Section title="Provider" icon="sparkles">
        <div className="grid">
          <Field group label="Who reads" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(saved === 'direct')
  const mode: ConnMode = !core && saved === 'core' ? 'off' : saved
  const ok = via !== '' && mode !== 'off'
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: Parcels does not talk to ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !core },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and follows their own parcels, with their own connections. Admins manage accounts; they do not see anyone else’s parcels.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their parcels." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/summary</code>) or add a parcel from a Shortcut. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>GET /summary</code></dt><dd className="muted">What arrives today, waits for pick-up, has a problem, and returns due — for Homepage or a morning Shortcut.</dd></div>
          <div><dt><code>POST /parcels</code></dt><dd className="muted"><code>{'{"tracking_number":"LP00123456789012","title":"Phone case"}'}</code> — follow a parcel (a share sheet Shortcut).</dd></div>
          <div><dt><code>POST /parcels/{'{id}'}/delivered</code></dt><dd className="muted">Mark a parcel delivered.</dd></div>
          <div><dt><code>GET /day/YYYY-MM-DD</code></dt><dd className="muted">One day of parcels — what Pocket Day reads.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const importJSON = async (file: File) => {
    setBusy(true)
    try { const r = await api.post<Record<string, number>>(`${V1}/import`, JSON.parse(await file.text())); toast('Imported: ' + (Object.entries(r).map(([k, v]) => `${v} ${k}`).join(', ') || 'nothing new')); window.dispatchEvent(new CustomEvent('ppc:changed')) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-parcels.db</code> in the data directory.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
      <Section title="Export & import" icon="archive" sub="Everything of yours as one JSON file (parcels, their journeys and papers), or your parcels as a CSV. Importing the same file twice adds nothing; links to other apps stay with the account the file came from.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a><a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} />Parcels as CSV</a><label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Import JSON…'}<input type="file" accept="application/json,.json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label></div>
      </Section>
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; registered?: boolean; url?: string; error?: string }
  const tiles: [string, number | undefined, string][] = [['Parcels', counts.parcels, 'package'], ['Steps', counts.parcel_events, 'route'], ['From email', counts.mail_finds, 'mail'], ['Papers', counts.attachments, 'file']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Pocket Core', core.configured ? (core.registered ? <span key="c" className="ok">connected · {core.url}</span> : <span key="c" className="bad">{core.error || 'not registered yet'}</span>) : <span key="c" className="muted">not configured</span>],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Parcels</h3>
          <p className="muted small">Follow every parcel in one place: where it is, when it arrives, a push when it is out for delivery or ready to pick up, and the last day to send something back. One Go binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-parcels" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Mail, Money, Wishlist, Inventory, Calendar, Tasks, Pocket AI and more. Tracking comes from 17TRACK (your own key) and Cainiao; carriers' pages are linked as they are.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
