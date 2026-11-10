import type { Edge, Node } from '@xyflow/react'

import { msOf, routeHref, segments, stageHref, tagLabel, touchTags } from '../lib/data'
import { codesOf } from '../lib/status'
import type { Data, FuncFlow, JErr, Route, RouteTiming } from '../lib/types'

export type NodeKind =
  | 'root'
  | 'group'
  | 'route'
  | 'request'
  | 'stage'
  | 'call'
  | 'exit'
  | 'loop'
  | 'response'
  | 'return'
  | 'input'
  | 'note'
  | 'label'

export type SData = {
  kind: NodeKind
  title: string
  /** full path kept as a hover tooltip when the card only shows its tail */
  full?: string
  sub?: string
  badge?: string
  index?: number
  tags?: string[]
  /** outcome of a return/exit path, shown beside "returns here" */
  rk?: 'nil' | 'error' | 'void'
  href?: string
  /** source file, line(s) and start line for the source sheet */
  file?: string
  line?: number
  lines?: number[]
  /** last line of the source construct, for trace focus */
  spanEnd?: number
  startLine?: number
  /** true when the current view can open the source sheet from a card */
  source?: boolean
  /** request context carried by route-shaped cards */
  path?: string
  method?: string
  curl?: string
  /** trace overlay: formatted duration for this node */
  ms?: string
  /** trace overlay: 0..1 share of the route's summed time */
  share?: number
  /** runtime messages this node can emit, for the hover preview */
  errs?: JErr[]
  /** true while a pasted message is being traced to this node */
  focused?: boolean
} & Record<string, unknown>

export type SNode = Node<SData, 'scene'>
export type SEdge = Edge<{ code?: string; railX?: number }, string>

export type Scene = { nodes: SNode[]; edges: SEdge[]; key: string }

const node = (
  id: string,
  x: number,
  y: number,
  data: SData,
  width: number,
  height: number,
): SNode => ({ id, type: 'scene', position: { x, y }, data: { ...data, w: width, h: height } })

const edge = (from: string, to: string): SEdge => ({
  id: `${from}->${to}`,
  source: from,
  target: to,
  type: 'flow',
  data: {},
  sourceHandle: 'b',
  targetHandle: 't',
})

const cross = (from: string, to: string): SEdge => ({
  id: `${from}->${to}`,
  source: from,
  target: to,
  type: 'default',
  sourceHandle: 'r',
  targetHandle: 'l',
  style: { stroke: '#2b535b', strokeWidth: 1.2 },
})

export const COL = 200

/** Level 1: the router, one column per path depth, focused on a group path. */
export function buildRouterScene(
  d: Data,
  focus: string,
  proto: 'all' | 'http' | 'ws' | 'sse' = 'all',
  svc = '',
): Scene {
  const segs = segments(focus)
  const depth = segs.length
  const nodes: SNode[] = []
  const edges: SEdge[] = []

  const colPath = new Map<number, string>()
  const active = (r: Route) =>
    (proto === 'all' ? true : proto === 'http' ? !r.pr : r.pr === proto) && (!svc || r.svc === svc)

  const under = (path: string) =>
    !focus || path === focus || path.startsWith(focus + '/') || focus.startsWith(path + '/')

  const emitRoutes = (level: number, cap = 22) => {
    const all = (routesByDepth.get(level) ?? [])
      .filter((i) => {
        const r = d.routes[i]
        const p = r.p
        return active(r) && under(p === '/' ? '' : p.slice(1))
      })
      .sort((a, b) => d.routes[a].p.localeCompare(d.routes[b].p))
    const shown = all.slice(0, Math.max(0, cap))
    for (const ri of shown) {
      const r = d.routes[ri]
      const id = 'r:' + ri
      const parts = segments(r.p)
      nodes.push(
        node(
          id,
          level * COL,
          nextY(level),
          {
            kind: 'route',
            title: '/' + (parts[parts.length - 1] ?? r.p),
            full: r.p,
            badge: r.pr ? r.pr.toUpperCase() : r.m,
            sub: r.pr
              ? `${r.m} · ${r.ch.length} ${r.ch.length === 1 ? 'step' : 'steps'}`
              : `${r.ch.length} ${r.ch.length === 1 ? 'step' : 'steps'}`,
            href: routeHref(ri),
            path: r.p,
            method: r.m,
            curl: r.doc?.curl,
            file: r.f,
            line: r.l,
          },
          176,
          58,
        ),
      )
    }
    if (all.length > shown.length && cap > 0) {
      const id = 'more:' + level
      nodes.push(
        node(
          id,
          level * COL,
          nextY(level),
          {
            kind: 'note',
            title: `+${all.length - shown.length} more routes`,
            sub: 'search to reach them',
          },
          176,
          58,
        ),
      )
    }
  }

  const rows = new Map<number, number>()
  const nextY = (level: number) => {
    const n = rows.get(level) ?? 0
    rows.set(level, n + 1)
    return 28 + n * 64
  }

  const rootId = 'root'
  nodes.push(
    node(
      rootId,
      0,
      nextY(0),
      {
        kind: 'root',
        title: d.repo,
        sub: d.mod,
        badge: `${d.routes.filter(active).length} ${proto === 'all' ? 'routes' : proto.toUpperCase()}`,
        href: '/',
      },
      176,
      58,
    ),
  )

  const groupsByDepth = new Map<number, Set<string>>()
  const routesByDepth = new Map<number, number[]>()
  const parentOf = new Map<string, string>()

  d.routes.forEach((r, i) => {
    if (!active(r)) return
    const s = segments(r.p)
    const chain = s.slice(0, Math.max(1, s.length - 1))
    if (!chain.length) {
      const list = routesByDepth.get(0) ?? []
      list.push(i)
      routesByDepth.set(0, list)
      return
    }
    chain.forEach((_name, k) => {
      const path = chain.slice(0, k + 1).join('/')
      const set = groupsByDepth.get(k) ?? new Set<string>()
      set.add(path)
      groupsByDepth.set(k, set)
      const parent = k === 0 ? rootId : 'g:' + chain.slice(0, k).join('/')
      parentOf.set('g:' + path, parent)
    })
    const list = routesByDepth.get(chain.length + 1) ?? []
    list.push(i)
    routesByDepth.set(chain.length + 1, list)
    parentOf.set('r:' + i, 'g:' + chain.join('/'))
  })

  for (let level = 0; level < depth; level++) {
    const set = [...(groupsByDepth.get(level) ?? [])].filter(under).sort()
    for (const path of set) {
      const id = 'g:' + path
      const name = path.split('/').pop() ?? path
      const count = d.routes.filter(
        (r) => active(r) && (r.p === '/' + path || r.p.startsWith('/' + path + '/')),
      ).length
      if (!colPath.has(level + 1)) colPath.set(level + 1, path)
      nodes.push(
        node(
          id,
          (level + 1) * COL,
          nextY(level + 1),
          {
            kind: 'group',
            title: name,
            sub: '/' + path,
            badge: String(count),
            href: '/g/' + path,
          },
          176,
          58,
        ),
      )
    }
    emitRoutes(level + 1, 15 - set.length)
  }

  const openDepth = depth
  const set = [...(groupsByDepth.get(openDepth) ?? [])].filter(under).sort()
  for (const path of set) {
    const id = 'g:' + path
    const name = path.split('/').pop() ?? path
    const count = d.routes.filter(
      (r) => active(r) && (r.p === '/' + path || r.p.startsWith('/' + path + '/')),
    ).length
    if (!colPath.has(openDepth + 1)) colPath.set(openDepth + 1, path)
    nodes.push(
      node(
        id,
        (openDepth + 1) * COL,
        nextY(openDepth + 1),
        {
          kind: 'group',
          title: name,
          sub: '/' + path,
          badge: String(count),
          href: '/g/' + path,
        },
        176,
        58,
      ),
    )
  }
  emitRoutes(openDepth + 1, 15 - set.length)

  const laneLevel = openDepth + 2
  const laneRoutes = d.routes.filter(
    (r) => active(r) && (!focus || r.p === '/' + focus || r.p.startsWith('/' + focus + '/')),
  )
  const hit: Record<string, number> = {}
  for (const r of laneRoutes) {
    const seen = new Set<string>()
    for (const st of r.ch) {
      for (const c of st.c ?? []) if (c.t) seen.add(c.t)
      const f = st.fk ? d.flows[st.fk] : undefined
      for (const step of f?.flow ?? []) if (step.g) seen.add(step.g)
    }
    for (const t of seen) if (t === 'db' || t === 'cache' || t === 'net') hit[t] = (hit[t] ?? 0) + 1
  }
  const laneTags = ['db', 'cache', 'net'].filter((t) => hit[t])
  if (laneTags.length) {
    nodes.push(node('h:lane', laneLevel * COL, -42, { kind: 'label', title: 'reaches' }, 176, 18))
    for (const t of laneTags) {
      nodes.push(
        node(
          'lane:' + t,
          laneLevel * COL,
          nextY(laneLevel),
          {
            kind: 'group',
            title: tagLabel[t] ?? t,
            sub: `${hit[t]} ${hit[t] === 1 ? 'route' : 'routes'}`,
            tags: [t],
          },
          176,
          58,
        ),
      )
    }
  }

  // the focused chain must never have a hole, even if a column was filtered out
  for (let k = 0; k < depth; k++) {
    const path = segs.slice(0, k + 1).join('/')
    const id = 'g:' + path
    if (!nodes.some((n) => n.id === id)) {
      nodes.push(
        node(
          id,
          (k + 1) * COL,
          nextY(k + 1),
          {
            kind: 'group',
            title: path.split('/').pop() ?? path,
            sub: '/' + path,
            href: '/g/' + path,
          },
          176,
          58,
        ),
      )
    }
  }

  for (let k = 0; k < depth; k++) {
    const from = k === 0 ? rootId : 'g:' + segs.slice(0, k).join('/')
    const to = 'g:' + segs.slice(0, k + 1).join('/')
    if (nodes.some((n) => n.id === from) && nodes.some((n) => n.id === to))
      edges.push(cross(from, to))
  }

  const columns = [...new Set(nodes.map((n) => Math.round(n.position.x / COL)))]
    .filter((c) => c <= openDepth + 1)
    .sort((a, b) => a - b)
  const headers: SNode[] = columns.map((c) => {
    const base = segs.slice(0, Math.min(c, depth)).join('/')
    const label = c === 0 ? 'server' : c > depth ? (base ? '/' + base + '/*' : '/*') : '/' + base
    return node('h:' + c, c * COL, -42, { kind: 'label', title: label, w: 176, h: 18 }, 176, 18)
  })
  nodes.unshift(...headers)

  return { nodes, edges, key: `router:${focus}:${proto}` }
}

/** Level 2: one request walking its middleware chain. */
export type ExtCall = { g: string; n: string; fk?: string; f?: string; l?: number; ev?: string }

/** Every external call reached by a route: explicit stream-handler calls plus
 *  db/cache/net steps in the baked flows. */
export function externalCalls(r: Route, flows?: Record<string, FuncFlow>): ExtCall[] {
  const out: ExtCall[] = []
  const seen = new Set<string>()
  const base = (g: string, n: string, f?: string, l?: number) => `${g}|${n}|${f ?? ''}|${l ?? 0}`
  const add = (g: string, n: string, fk?: string, f?: string, l?: number, ev?: string) => {
    if (!n) return
    const key = `${base(g, n, f, l)}|${ev ?? ''}`
    if (seen.has(key)) return
    seen.add(key)
    out.push({ g, n, fk, f, l, ev })
  }
  for (const x of r.st?.ext ?? []) add(x.g, x.n, x.fk, x.f, x.l, x.ev)
  for (const st of r.ch)
    for (const step of (st.fk ? flows?.[st.fk]?.flow : undefined) ?? [])
      if (step.g === 'db' || step.g === 'cache' || step.g === 'net') {
        if (
          out.some(
            (x) => base(x.g, x.n, x.f, x.l) === base(step.g ?? '', step.t ?? '', step.f, step.l),
          )
        )
          continue
        add(step.g ?? '', step.t ?? '', step.fk, step.f, step.l)
      }
  return out
}

export function buildPipelineScene(
  r: Route,
  ri: number,
  timing?: RouteTiming,
  flows?: Record<string, FuncFlow>,
): Scene {
  const nodes: SNode[] = []
  const edges: SEdge[] = []
  const x = 150
  let y = 0
  const gap = 84

  nodes.push(
    node(
      'in',
      x,
      y,
      {
        kind: 'request',
        title: r.pr === 'ws' ? 'websocket connect' : r.pr === 'sse' ? 'event stream' : 'request in',
        sub: `${r.m} ${r.p}`,
        index: 0,
        path: r.p,
        method: r.m,
        curl: r.doc?.curl,
        file: r.f,
        line: r.l,
      },
      380,
      66,
    ),
  )
  y += gap

  const early: { from: string; label: string }[] = []

  r.ch.forEach((st, i) => {
    const id = 's' + i
    const t = timing?.st.find((x) => x.i === i)
    nodes.push(
      node(
        id,
        x,
        y,
        {
          kind: 'stage',
          title: st.n,
          sub: st.f ? `${st.f}:${st.l}` : undefined,
          badge: st.d ? undefined : (st.r ?? st.k),
          tags: touchTags(st),
          index: i + 1,
          href: stageHref(ri, i),
          ms: t ? msOf(t.m) : undefined,
          share: t?.sh,
          file: st.f,
          line: st.l,
          path: r.p,
          method: r.m,
          curl: r.doc?.curl,
        },
        380,
        76,
      ),
    )
    edges.push(edge(i === 0 ? 'in' : 's' + (i - 1), id))

    // early returns converge on the response node instead of sitting beside the spine
    const exits = ((st.fk ? flows?.[st.fk]?.flow : undefined) ?? []).filter((e) => e.k === 'exit')
    if (exits.length) {
      const codes = [...new Set(exits.flatMap((e) => codesOf(e)))].sort()
      early.push({ from: id, label: codes.join(' · ') || 'exits' })
    }
    y += gap
  })

  const last = r.ch.length
  const lastFlow = r.ch[last - 1]?.fk ? flows?.[r.ch[last - 1].fk!] : undefined
  // the final status can come from a plain return or from a response helper
  // card such as utils.SendJSONResponse(w, http.StatusOK, ...)
  const retCodes = [
    ...new Set(
      (lastFlow?.flow ?? [])
        .filter((s) => (s.k === 'ret' || s.k === 'call') && codesOf(s).length)
        .flatMap((s) => codesOf(s)),
    ),
  ].sort()
  const earlyCodes = [...new Set(early.flatMap((e) => e.label.split(' · ')))].filter((c) =>
    /^\d{3}$/.test(c),
  )
  const responseSub = r.pr
    ? `${r.pr === 'ws' ? 'websocket' : 'event stream'} · ${r.st?.events?.length ?? 0} message types`
    : [
        retCodes.length ? `${retCodes.join(' · ')} out` : undefined,
        earlyCodes.length ? `early ${earlyCodes.join(' · ')}` : undefined,
        timing ? `per request, from ${timing.spans} spans` : undefined,
      ]
        .filter(Boolean)
        .join(' · ') || undefined
  nodes.push(
    node(
      'out',
      x,
      y,
      {
        kind: 'response',
        title: r.pr ? 'stream open' : 'response out',
        sub: responseSub,
        index: last + 1,
        ms: timing ? msOf(timing.total) : undefined,
      },
      380,
      60,
    ),
  )
  if (last > 0) edges.push(edge('s' + (last - 1), 'out'))

  for (const [i, e] of early.entries()) {
    edges.push({
      id: e.from + '->out:' + i,
      source: e.from,
      target: 'out',
      sourceHandle: 'lo',
      targetHandle: 'l',
      type: 'exitRail',
      label: e.label,
      labelStyle: { fill: '#e0a33e', fontSize: 10, fontFamily: 'ui-monospace, monospace' },
      labelBgStyle: { fill: '#0b1e23', fillOpacity: 0.95 },
      labelBgPadding: [6, 3] as [number, number],
      labelBgBorderRadius: 3,
      style: { stroke: '#e0a33e', strokeWidth: 1.1, strokeDasharray: '4 3', opacity: 0.75 },
    })
  }

  return { nodes, edges, key: 'pipe:' + ri }
}
