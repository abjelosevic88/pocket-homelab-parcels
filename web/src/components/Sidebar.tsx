// Left column: the add button, sections with counts, the parcels on their way — the Pocket Calendar sidebar.
import { Link, NavLink } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { useData } from '../lib/data'
import { STATUS } from '../lib/parcels'
import { Icon } from './icons'

export function Sidebar({ onAdd, onNavigate }: { onAdd: () => void; onNavigate: () => void }) {
  const { status, settings } = useApp()
  const { overview: o } = useData()
  const title = settings?.title || status?.title || 'Pocket Parcels'
  const live = o ? [...o.needs_you, ...o.problems, ...o.on_the_way] : []
  const nav: { to: string; label: string; icon: string; end?: boolean; count?: number; warn?: boolean }[] = [
    { to: '/', label: 'Overview', icon: 'home', end: true, count: o?.counts.needs_you, warn: true },
    { to: '/parcels', label: 'Parcels', icon: 'package', count: o?.counts.active },
    { to: '/email', label: 'Found in email', icon: 'mail', count: o?.finds, warn: true },
    { to: '/returns', label: 'Send back', icon: 'undo', count: o?.counts.returns },
    { to: '/stats', label: 'Stats', icon: 'chart' },
  ]
  return (
    <aside className="sidebar" aria-label="Pocket Parcels">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Parcels' ? title : <>Pocket <b>Parcels</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" onClick={onAdd} title="Add a parcel (N)">
        <Icon name="plus" size={15} strokeWidth={2.2} />Add a parcel
      </button>

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className={`sb-count ${n.warn ? 'warn' : ''}`}>{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      <section className="sb-sec">
        <h4 className="sb-h"><Link to="/parcels" className="sb-h-btn" onClick={onNavigate}>On the way</Link></h4>
        {o && live.length === 0 && <button type="button" className="sb-row sb-empty" onClick={onAdd}><Icon name="plus" size={14} />Add a parcel to follow</button>}
        {live.slice(0, 10).map(p => (
          <NavLink key={p.id} to={`/parcels/${p.id}`} onClick={onNavigate} className={({ isActive }) => `sb-row sb-parcel ${isActive ? 'active' : ''}`} title={`${p.title} — ${p.status_label}`}>
            <span className={`sb-st st-${p.status}${p.stale ? ' st-stale' : ''}`}><Icon name={STATUS[p.status]?.icon || 'package'} size={13} /></span>
            <span className="grow ellipsis">{p.title}</span>
          </NavLink>
        ))}
        {live.length > 10 && <Link to="/parcels" className="sb-row sb-more" onClick={onNavigate}>{live.length - 10} more…</Link>}
      </section>

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('ppc:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
