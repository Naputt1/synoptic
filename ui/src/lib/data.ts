import type { Data, ErrRef, FuncFlow, JErr, Route, RouteTiming, Stage } from './types'

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

export const groupOf = (r: Route, depth = 2): string => segments(r.p).slice(0, depth).join('/')

export function stageOf(r: Route, i: number): Stage | undefined {
  return r.ch[i]
}

export function flowOf(d: Data, key?: string): FuncFlow | undefined {
  if (!key) return undefined
  return d.flows[key]
}

export const routeHref = (ri: number) => `/r/${ri}`
export const stageHref = (ri: number, si: number) => `/r/${ri}/s/${si}`
export const fnHref = (
  key: string,
  origin?: { ri: number; si: number },
  ctx?: { ik?: string; sk?: string; hl?: number },
) =>
  `/f?k=${encodeURIComponent(key)}${origin ? `&ri=${origin.ri}&si=${origin.si}` : ''}` +
  `${ctx?.ik ? `&i=${encodeURIComponent(ctx.ik)}` : ''}` +
  `${ctx?.sk ? `&t=${encodeURIComponent(ctx.sk)}` : ''}${ctx?.hl ? `&hl=${ctx.hl}` : ''}`

/** Where a runtime message goes when you click it: its function and line, or its surface. */
export const errHref = (e: JErr, ref?: ErrRef): string | undefined => {
  if (ref?.k === 'rpc') return `/rpc/${ref.si ?? 0}/${ref.mi ?? 0}?hl=${e.l}`
  if (e.fn) {
    const origin =
      ref && Number.isInteger(ref.ri) && ref.si !== undefined
        ? { ri: ref.ri, si: ref.si }
        : undefined
    return fnHref(e.fn, origin, { hl: e.l })
  }
  if (ref) return stageHref(ref.ri, ref.si ?? 0)
  return undefined
}

export const methodHue: Record<string, string> = {
  GET: 'text-info',
  POST: 'text-acc',
  PUT: 'text-warn',
  PATCH: 'text-warn',
  DELETE: 'text-bad',
  ANY: 'text-mut',
}

export function touchTags(st: Stage, withInputs = true): string[] {
  const tags = new Set<string>()
  for (const c of st.c ?? []) if (c.t) tags.add(c.t)
  if (st.p) tags.add(st.p)
  if (!withInputs) return [...tags]

  const groups = new Map<string, { kind: string; names: string[] }>()
  for (const x of st.in ?? []) {
    if (x.a === 'unused' || x.a === 'undeclared') continue
    const group = groups.get(x.k) ?? { kind: x.k, names: [] }
    if (!group.names.includes(x.n)) group.names.push(x.n)
    groups.set(x.k, group)
  }
  for (const g of groups.values()) {
    tags.add(g.names.length === 1 ? `${g.kind}:${g.names[0]}` : `${g.kind}×${g.names.length}`)
  }
  return [...tags]
}

/** A readable frame label: explicit event, meaningful name, then type. */
export function frameName(m: { e?: string; n?: string; t?: string }): string {
  if (m.n && m.n.length > 2 && !m.n.includes('.')) return m.n
  const typ = (m.t ?? '').replace(/^.*[./]/, '').replace(/^Msg/, '')
  if (typ) return typ.replace(/([a-z0-9])([A-Z])/g, '$1_$2').toLowerCase()
  return m.e || m.n || 'frame'
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
