import type { Data, FuncFlow, Route, RouteTiming, Stage } from './types'

let cache: Data | null = null

export async function loadData(): Promise<Data> {
  if (cache) return cache
  const embedded = window.__SYNOPTIC__
  if (embedded) {
    cache = embedded
    return embedded
  }
  const node = document.getElementById('synoptic-data')
  if (node?.textContent) {
    cache = JSON.parse(node.textContent) as Data
    return cache
  }
  const res = await fetch('./synoptic.json')
  if (!res.ok) throw new Error('no data: generate the page with the synoptic tool')
  cache = (await res.json()) as Data
  return cache
}

export const segments = (path: string): string[] => path.split('/').filter(Boolean)

export const groupOf = (r: Route, depth = 2): string =>
  segments(r.p).slice(0, depth).join('/')

export function stageOf(r: Route, i: number): Stage | undefined {
  return r.ch[i]
}

export function flowOf(d: Data, key?: string): FuncFlow | undefined {
  if (!key) return undefined
  return d.flows[key]
}

export const routeHref = (ri: number) => `/r/${ri}`
export const stageHref = (ri: number, si: number) => `/r/${ri}/s/${si}`
export const fnHref = (key: string) => `/f?k=${encodeURIComponent(key)}`

export const methodHue: Record<string, string> = {
  GET: 'text-info',
  POST: 'text-acc',
  PUT: 'text-warn',
  PATCH: 'text-warn',
  DELETE: 'text-bad',
  ANY: 'text-mut',
}

export function touchTags(st: Stage): string[] {
  const tags = new Set<string>()
  for (const c of st.c ?? []) if (c.t) tags.add(c.t)
  if (st.p) tags.add(st.p)
  return [...tags]
}

export const tagLabel: Record<string, string> = {
  db: 'database',
  cache: 'cache',
  net: 'outbound',
}

/** The trace bucket for one route, when the page was built with -trace. */
export function timingOf(d: Data, r: Route): RouteTiming | undefined {
  return d.timing?.[r.m + ' ' + r.p]
}

export function msOf(v: number): string {
  if (v >= 100) return v.toFixed(0) + ' ms'
  if (v >= 10) return v.toFixed(1) + ' ms'
  return v.toFixed(2) + ' ms'
}
