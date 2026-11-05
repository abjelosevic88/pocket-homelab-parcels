// Pick something from another Pocket app: a wish, the payment in Money, a
// document, a contact. One searchable list; says plainly when the app is
// not connected.
import { type ReactNode, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Picked } from '../lib/types'
import { Loading, Modal, useDebounce } from './ui'
import { Icon } from './icons'

export function PickModal<T extends { id: number }>({ title, path, initialQ = '', render, onPick, onClose, placeholder = 'Search…', empty = 'Nothing found.', icon = 'link', note }: {
  title: string; path: (q: string) => string; initialQ?: string; render: (item: T) => { label: string; sub?: string; side?: string }
  onPick: (item: T) => void | Promise<void>; onClose: () => void; placeholder?: string; empty?: string; icon?: string; note?: ReactNode
}) {
  const [q, setQ] = useState(initialQ)
  const dq = useDebounce(q.trim(), 250)
  const [res, setRes] = useState<Picked<T> | null>(null)
  const [busy, setBusy] = useState(0)
  useEffect(() => {
    let live = true
    api.get<Picked<T>>(path(dq)).then(r => { if (live) setRes(r) }).catch(e => { if (live) setRes({ connected: false, detail: (e as Error).message, items: [] }) })
    return () => { live = false }
  }, [dq]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <Modal title={title} onClose={onClose}>
      <div className="stack">
        <div className="pick-search"><Icon name="search" size={15} /><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder={placeholder} aria-label="Search" /></div>
        {note}
        {!res ? <Loading /> : !res.connected ? (
          <div className="callout warn"><Icon name="alert" size={15} /><div>{res.detail || 'Not connected.'} <Link to="/settings/apps" onClick={onClose}>Settings → Connected apps</Link></div></div>
        ) : res.items.length === 0 ? <p className="muted small pick-empty">{empty}</p> : (
          <div className="pick-list" role="listbox">
            {res.items.map(it => {
              const r = render(it)
              return (
                <button key={it.id} type="button" className="pick-row" disabled={!!busy} onClick={async () => { setBusy(it.id); try { await onPick(it) } finally { setBusy(0) } }}>
                  <span className="list-ico sm"><Icon name={icon} size={14} /></span>
                  <span className="grow row-main"><span className="row-title"><span className="ellipsis">{r.label}</span></span>{r.sub && <span className="muted small ellipsis">{r.sub}</span>}</span>
                  {r.side && <span className="small tnum">{r.side}</span>}
                  {busy === it.id && <span className="spinner" />}
                </button>
              )
            })}
          </div>
        )}
      </div>
    </Modal>
  )
}

/** Plain URL text for the picker's query string. */
export const qs = (params: Record<string, string | number | undefined>) => {
  const u = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') u.set(k, String(v))
  const s = u.toString()
  return s ? `?${s}` : ''
}
export const linkURL = (path: string, params: Record<string, string | number | undefined> = {}) => `${V1}/links/${path}${qs(params)}`
