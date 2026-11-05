// The overview (counts and the parcels on their way), loaded once for the
// sidebar and badges; any change in the app ("ppc:changed") and every two
// minutes while the page is visible reload it.
import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, V1 } from './api'
import type { Overview } from './types'

interface Data { overview: Overview | null; reload: () => void }
const Ctx = createContext<Data>({ overview: null, reload: () => {} })
export const useData = () => useContext(Ctx)

/** Something was added or changed: lists and pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('ppc:changed'))
export function useOnChange(cb: () => void) {
  const ref = React.useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('ppc:changed', h)
    return () => window.removeEventListener('ppc:changed', h)
  }, [])
}

export function DataProvider({ children }: { children: React.ReactNode }) {
  const [overview, setOverview] = useState<Overview | null>(null)
  const reload = useCallback(() => {
    api.get<Overview>(`${V1}/overview`).then(setOverview).catch(() => {})
  }, [])
  useEffect(() => { reload() }, [reload])
  useEffect(() => {
    const t = setInterval(() => { if (document.visibilityState === 'visible') reload() }, 120_000)
    return () => clearInterval(t)
  }, [reload])
  useOnChange(reload)
  return <Ctx.Provider value={{ overview, reload }}>{children}</Ctx.Provider>
}
