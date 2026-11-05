// One series of columns (parcels per month):
// thin columns with a rounded top on one baseline, a recessive grid, the
// value on hover. Axis labels are HTML, so nothing stretches.
import { useState } from 'react'

export interface Bar { key: string; label: string; value: number; tip: string; muted?: boolean }

export function Bars({ bars, height = 150, fmt, ariaLabel }: { bars: Bar[]; height?: number; fmt: (v: number) => string; ariaLabel: string }) {
  const [hover, setHover] = useState<number | null>(null)
  const max = Math.max(...bars.map(b => b.value), 0)
  const top = niceMax(max)
  const ticks = [top, top / 2, 0]
  const n = bars.length
  return (
    <div className="bars-chart" role="img" aria-label={ariaLabel}>
      <div className="bc-plot" style={{ height }}>
        <div className="bc-grid" aria-hidden="true">{ticks.map(t => <span key={t}><i>{t > 0 ? fmt(t) : ''}</i></span>)}</div>
        <div className="bc-cols" onMouseLeave={() => setHover(null)}>
          {bars.map((b, i) => (
            <div key={b.key} className={`bc-col ${hover === i ? 'hl' : ''}`} onMouseEnter={() => setHover(i)} onFocus={() => setHover(i)} onBlur={() => setHover(null)} tabIndex={0} aria-label={b.tip}>
              <span className={`bc-bar ${b.muted ? 'muted' : ''}`} style={{ height: top > 0 ? `${(b.value / top) * 100}%` : 0 }} />
            </div>
          ))}
        </div>
        {hover !== null && bars[hover] && (
          <div className="bc-tip" style={{ left: `${((hover + 0.5) / n) * 100}%` }}>{bars[hover].tip}</div>
        )}
      </div>
      <div className="bc-labels" aria-hidden="true">{bars.map((b, i) => <span key={b.key} className={i % labelStep(n) === 0 ? '' : 'skip'}>{b.label}</span>)}</div>
    </div>
  )
}

function labelStep(n: number) { return n > 18 ? 3 : n > 12 ? 2 : 1 }

function niceMax(v: number): number {
  if (v <= 0) return 0
  const p = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10]) if (m * p >= v) return m * p
  return 10 * p
}

/** "2026-03" → "Mar". */
export function monthLabel(ym: string, withYear = false) {
  const [y, m] = ym.split('-').map(Number)
  return new Date(y, m - 1, 1).toLocaleDateString(undefined, withYear ? { month: 'short', year: 'numeric' } : { month: 'short' })
}
