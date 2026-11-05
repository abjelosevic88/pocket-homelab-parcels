// Parcels: on the way, delivered or archived; search, shop and direction filters.
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useOnChange } from '../lib/data'
import type { Parcel } from '../lib/types'
import { Card, Empty, Loading, Segmented, useAsync, useDebounce } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ParcelRow } from '../components/Rows'
import { openAdd } from '../components/Layout'

type View = 'active' | 'delivered' | 'archived' | 'problems' | 'returns' | 'all'
const VIEWS: { value: View; label: string }[] = [{ value: 'active', label: 'On the way' }, { value: 'delivered', label: 'Delivered' }, { value: 'archived', label: 'Archive' }]
const EXTRA: Partial<Record<View, string>> = { problems: 'Problems', returns: 'Send back', all: 'Everything' }

export default function Parcels() {
  const [sp, setSp] = useSearchParams()
  const view = (sp.get('view') as View) || 'active'
  const shop = sp.get('shop') || ''
  const dir = sp.get('direction') || ''
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q.trim(), 250)
  const setParam = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n, { replace: true }) }
  const { data, reload, error } = useAsync(() => {
    const p = new URLSearchParams({ view })
    if (dq) p.set('q', dq)
    if (shop) p.set('shop', shop)
    if (dir) p.set('direction', dir)
    return api.get<{ parcels: Parcel[]; total: number }>(`${V1}/parcels?${p}`)
  }, [view, dq, shop, dir])
  const { data: shops } = useAsync(() => api.get<{ shops: string[] }>(`${V1}/shops`).then(r => r.shops), [])
  useOnChange(reload)
  const viewOptions = EXTRA[view] ? [...VIEWS, { value: view, label: EXTRA[view]! }] : VIEWS
  return (
    <div className="stack loose">
      <div className="page-header">
        <div className="grow"><h1>Parcels</h1><div className="sub">{data ? `${data.total} ${view === 'active' ? 'on the way' : view === 'delivered' ? 'delivered, not yet archived' : view === 'archived' ? 'in the archive' : 'parcels'}` : ' '}</div></div>
        <div className="actions hide-xs"><button type="button" className="btn primary" onClick={() => openAdd()}><Icon name="plus" size={15} />Add a parcel</button></div>
      </div>
      <div className="scroll-x"><Segmented value={view} onChange={v => setParam('view', v === 'active' ? '' : v)} options={viewOptions} label="Which parcels" /></div>
      <div className="row wrap filters">
        <div className="search-in input-ico"><Icon name="search" size={15} /><input value={q} onChange={e => { setQ(e.target.value); setParam('q', e.target.value.trim()) }} placeholder="Name, tracking number, shop, order…" aria-label="Search parcels" /></div>
        <Select ariaLabel="Shop" value={shop} onChange={v => setParam('shop', v)} placeholder="All shops" clearLabel="All shops" options={(shops || []).map(s => ({ value: s, label: s }))} />
        <Segmented size="sm" value={dir || 'all'} onChange={v => setParam('direction', v === 'all' ? '' : v)} options={[{ value: 'all', label: 'All' }, { value: 'in', label: 'Coming' }, { value: 'out', label: 'Sent' }]} label="Direction" />
      </div>
      {error ? <div className="callout danger"><Icon name="alert" size={15} />{error}</div> : !data ? <Loading /> : data.parcels.length === 0 ? (
        <Card><Empty title={dq || shop || dir ? 'Nothing matches' : view === 'active' ? 'Nothing on the way' : view === 'delivered' ? 'Nothing delivered lately' : 'Nothing here yet'} icon="package">
          {view === 'active' && !dq && <button type="button" className="btn primary mt" onClick={() => openAdd()}><Icon name="plus" size={15} />Add a parcel</button>}
          {view === 'delivered' && <span>Delivered parcels move to the Archive after a while (Settings → General).</span>}
        </Empty></Card>
      ) : (
        <Card flush><div className="list">{data.parcels.map(p => <ParcelRow key={p.id} p={p} progress={view === 'active'} showReturn={view === 'returns'} />)}</div></Card>
      )}
    </div>
  )
}
