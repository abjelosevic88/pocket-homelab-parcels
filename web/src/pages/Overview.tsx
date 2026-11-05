// Overview: what needs you today, what is on the way, problems, what to send back, what arrived lately.
import { Link } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { useData } from '../lib/data'
import { plural } from '../lib/format'
import { Card, Loading } from '../components/ui'
import { Icon } from '../components/icons'
import { ParcelRow } from '../components/Rows'
import { openAdd } from '../components/Layout'

export default function Overview() {
  const { user, settings } = useApp()
  const { overview: o } = useData()
  if (!o) return <Loading />
  const first = (user?.name || '').split(' ')[0]
  const empty = o.counts.active === 0 && o.recent.length === 0 && o.returns.length === 0
  const mailOn = !!settings?.mail_configured
  if (empty) return (
    <div className="stack loose">
      {o.finds > 0 && <FindsBanner n={o.finds} />}
      <Card className="welcome">
        <div className="welcome-body">
          <img src="/favicon.svg" alt="" width={56} height={56} />
          <div className="grow">
            <h2>Follow your parcels here</h2>
            <p className="muted">Paste a tracking number from a shipping email or a shop, and Pocket Parcels shows where the parcel is, when it should arrive, and sends a push when it is out for delivery or ready to pick up. AliExpress parcels are followed for free; for every other carrier add a free 17TRACK key in Settings.</p>
            <p className="muted">With Pocket Mail connected, it finds tracking numbers in your email by itself.</p>
            <div className="row wrap mt">
              <button type="button" className="btn primary" onClick={() => openAdd()}><Icon name="plus" size={15} />Add a parcel</button>
              {!mailOn && <Link to="/settings/apps" className="btn"><Icon name="mail" size={15} />Connect Pocket Mail</Link>}
              <Link to="/settings/tracking" className="btn ghost"><Icon name="route" size={15} />Tracking settings</Link>
            </div>
          </div>
        </div>
      </Card>
    </div>
  )
  const n = o.counts.needs_you
  const line = [n ? `${plural(n, 'parcel needs', 'parcels need')} you today` : 'Nothing needs you today', o.counts.on_the_way ? `${o.counts.on_the_way} on the way` : '', o.counts.problems ? plural(o.counts.problems, 'problem') : ''].filter(Boolean).join(', ') + '.'
  return (
    <div className="stack loose">
      <div className="home-head">
        <div className="grow">
          <h1 className="home-title">{first && first !== 'Admin' ? `Hello, ${first}` : 'Your parcels'}</h1>
          <p className="muted">{line}</p>
        </div>
      </div>
      {o.finds > 0 && <FindsBanner n={o.finds} />}

      <div className="tiles">
        <Link to="/parcels" className="tile">
          <div className="tile-head"><span className="tile-ico"><Icon name="route" size={14} /></span>Following</div>
          <div className="tile-val">{o.counts.active}</div>
        </Link>
        <Link to="/" className={`tile ${n ? 'tile-hot' : ''}`} onClick={e => { e.preventDefault(); document.getElementById('needs-you')?.scrollIntoView({ behavior: 'smooth' }) }}>
          <div className="tile-head"><span className="tile-ico"><Icon name="truck" size={14} /></span>Today or to pick up</div>
          <div className="tile-val">{n}</div>
        </Link>
        <Link to="/parcels?view=problems" className={`tile ${o.counts.problems ? 'tile-bad' : ''}`}>
          <div className="tile-head"><span className="tile-ico"><Icon name="alert" size={14} /></span>Problems</div>
          <div className="tile-val">{o.counts.problems}</div>
        </Link>
        <Link to="/parcels?view=delivered" className="tile">
          <div className="tile-head"><span className="tile-ico"><Icon name="check-circle" size={14} /></span>Delivered this month</div>
          <div className="tile-val">{o.counts.delivered_month}</div>
        </Link>
      </div>

      <div className="home-grid">
        <div className="stack">
          <Card title="Needs you" flush actions={<button type="button" className="btn ghost sm" onClick={() => openAdd()}><Icon name="plus" size={14} />Add</button>}>
            <span id="needs-you" className="anchor-target" />
            {o.needs_you.length === 0 ? <p className="muted pad">Nothing arrives today and nothing waits at a pick-up point.</p>
              : <div className="list">{o.needs_you.map(p => <ParcelRow key={p.id} p={p} />)}</div>}
          </Card>
          {o.problems.length > 0 && <Card title="Problems" sub="A failed delivery, a customs hold, or no news for a while" flush>
            <div className="list">{o.problems.map(p => <ParcelRow key={p.id} p={p} progress />)}</div>
          </Card>}
          {o.returns.length > 0 && <Card title="Send back" flush actions={<Link to="/returns" className="btn ghost sm">All</Link>}>
            <div className="list">{o.returns.map(p => <ParcelRow key={p.id} p={p} showReturn />)}</div>
          </Card>}
        </div>
        <div className="stack">
          <Card title="On the way" flush actions={<Link to="/parcels" className="btn ghost sm">All parcels</Link>}>
            {o.on_the_way.length === 0 ? <p className="muted pad">Nothing else on the way.</p>
              : <div className="list">{o.on_the_way.map(p => <ParcelRow key={p.id} p={p} progress />)}</div>}
          </Card>
          <Card title="Arrived lately" flush actions={<Link to="/parcels?view=delivered" className="btn ghost sm">Delivered</Link>}>
            {o.recent.length === 0 ? <p className="muted pad">Nothing arrived in the last ten days.</p>
              : <div className="list">{o.recent.map(p => <ParcelRow key={p.id} p={p} />)}</div>}
          </Card>
        </div>
      </div>
      {!o.tracking.cainiao && !o.tracking.seventeen && <p className="muted small"><Icon name="info" size={13} /> Nothing follows your parcels automatically. <Link to="/settings/tracking">Switch on free AliExpress tracking or add a 17TRACK key</Link>.</p>}
    </div>
  )
}

function FindsBanner({ n }: { n: number }) {
  return (
    <Link to="/email" className="callout info finds-banner">
      <Icon name="mail" size={15} />
      <span className="grow"><b>{plural(n, 'parcel')} found in your email.</b> Add {n === 1 ? 'it' : 'them'} with one tap, or ignore.</span>
      <Icon name="chevron-right" size={15} />
    </Link>
  )
}
