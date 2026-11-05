// Stats: parcels per month, how long they take, from which shops and with which carriers, and what was spent.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useOnChange } from '../lib/data'
import { money } from '../lib/parcels'
import type { Stats as StatsT, Tally } from '../lib/types'
import { Card, Empty, Loading, Segmented, useAsync } from '../components/ui'
import { Icon } from '../components/icons'
import { Bars, monthLabel } from '../components/Bars'

const days = (v: number) => v ? `${v.toFixed(v < 10 ? 1 : 0)} days` : '—'

export default function Stats() {
  const [months, setMonths] = useState(12)
  const { data: s, reload } = useAsync(() => api.get<StatsT>(`${V1}/stats?months=${months}`), [months])
  useOnChange(reload)
  if (!s) return <Loading />
  const total = s.months.reduce((n, m) => n + m.parcels, 0)
  const spent = Object.entries(s.spent).filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1])
  const extra = Object.entries(s.extra).filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1])
  return (
    <div className="stack loose">
      <div className="page-header">
        <div className="grow"><h1>Stats</h1><div className="sub">Parcels ordered or sent in the last {months} months.</div></div>
        <div className="actions"><Segmented size="sm" value={months} onChange={setMonths} options={[{ value: 3, label: '3 months' }, { value: 12, label: 'Year' }, { value: 24, label: '2 years' }]} label="Period" /></div>
      </div>
      {total === 0 ? <Card><Empty title="Nothing to count yet" icon="chart">Stats appear once parcels arrive.</Empty></Card> : <>
        <div className="tiles">
          <div className="tile static"><div className="tile-head"><span className="tile-ico"><Icon name="package" size={14} /></span>Parcels</div><div className="tile-val">{total}</div><div className="muted small">{s.delivered} delivered</div></div>
          <div className="tile static"><div className="tile-head"><span className="tile-ico"><Icon name="clock" size={14} /></span>Usually takes</div><div className="tile-val">{days(s.avg_days)}</div><div className="muted small">from order to door</div></div>
          <div className="tile static"><div className="tile-head"><span className="tile-ico"><Icon name="wallet" size={14} /></span>Spent on orders</div><div className="tile-val">{spent.length ? money(spent[0][1], spent[0][0], true) : '—'}</div>
            <div className="muted small">{spent.length > 1 ? spent.slice(1).map(([c, v]) => money(v, c, true)).join(' + ') : 'where you entered a price'}</div></div>
          <div className="tile static"><div className="tile-head"><span className="tile-ico"><Icon name="receipt" size={14} /></span>Customs and postage</div><div className="tile-val">{extra.length ? money(extra[0][1], extra[0][0], true) : '—'}</div>
            <div className="muted small">{extra.length > 1 ? extra.slice(1).map(([c, v]) => money(v, c, true)).join(' + ') : 'paid on top'}</div></div>
        </div>
        <Card title="Parcels per month">
          <Bars ariaLabel="Parcels per month" fmt={v => String(Math.round(v))} bars={s.months.map(m => ({ key: m.month, label: monthLabel(m.month), value: m.parcels,
            tip: `${monthLabel(m.month, true)}: ${m.parcels} ${m.parcels === 1 ? 'parcel' : 'parcels'}${m.delivered ? `, took ${days(m.avg_days)} on average` : ''}` }))} />
        </Card>
        <div className="home-grid">
          <Card title="Shops" sub="Most parcels first, with how long they usually take"><TallyBars rows={s.shops} link={n => `/parcels?view=all&shop=${encodeURIComponent(n)}`} /></Card>
          <Card title="Carriers" sub="Who brought them"><TallyBars rows={s.carriers} /></Card>
        </div>
      </>}
    </div>
  )
}

function TallyBars({ rows, link }: { rows: Tally[]; link?: (name: string) => string }) {
  if (rows.length === 0) return <p className="muted">Nothing yet.</p>
  const max = Math.max(...rows.map(r => r.parcels), 1)
  return (
    <div className="kind-bars">
      {rows.map(r => {
        const inner = <>
          <span className="kb-label ellipsis">{r.name}</span>
          <span className="kb-track"><span style={{ width: `${(r.parcels / max) * 100}%` }} /></span>
          <span className="kb-val tnum">{r.parcels}<span className="muted"> · {days(r.avg_days)}</span></span>
        </>
        return link ? <Link key={r.name} to={link(r.name)} className="kind-bar">{inner}</Link> : <div key={r.name} className="kind-bar static">{inner}</div>
      })}
    </div>
  )
}
