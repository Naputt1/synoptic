import { createContext, useContext } from 'react'

import type { Data } from './types'

export const DataCtx = createContext<Data | null>(null)
export const useData = () => {
  const d = useContext(DataCtx)
  if (!d) throw new Error('data not loaded')
  return d
}

/** Selected service of a multi-entry module; '' means every service. */
export const SvcCtx = createContext<{ svc: string; setSvc: (v: string) => void }>({
  svc: '',
  setSvc: () => {},
})
export const useSvc = () => useContext(SvcCtx).svc
export const useSvcSwitch = () => useContext(SvcCtx)
