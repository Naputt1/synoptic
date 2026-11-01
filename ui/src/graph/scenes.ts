import type { Edge, Node } from '@xyflow/react'
import type { Data, FuncFlow, Route, RouteTiming, Step } from '../lib/types'
import { fnHref, tagLabel, msOf, routeHref, segments, stageHref, touchTags } from '../lib/data'

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
  | 'note'
  | 'label'

export type Status = 'idle' | 'cur' | 'done'

export type SData = {
  kind: NodeKind
  title: string
  sub?: string
  badge?: string
  index?: number
  status?: Status
  tags?: string[]
  href?: string
  /** trace overlay: formatted duration for this node */
  ms?: string
  /** trace overlay: 0..1 share of the route's summed time */
  share?: number
} & Record<string, unknown>

export type SNode = Node<SData, 'scene'>
export type SEdge = Edge<{ live?: boolean }, string>

export type Scene = { nodes: SNode[]; edges: SEdge[]; steps: number; key: string }

const node = (
  id: string,
  x: number,
  y: number,
  data: SData,
  width: number,
  height: number,
): SNode => ({ id, type: 'scene', position: { x, y }, data: { ...data, w: width, h: height } })

const edge = (from: string, to: string, live = false): SEdge => ({
  id: `${from}->${to}`,
  source: from,
  target: to,
  type: 'flow',
  data: { live },
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
export function buildRouterScene(d: Data, focus: string): Scene {
  const segs = segments(focus)
  const depth = segs.length
  const nodes: SNode[] = []
  const edges: SEdge[] = []

  const colPath = new Map<number, string>()

  const under = (path: string) =>
    !focus || path === focus || path.startsWith(focus + '/') || focus.startsWith(path + '/')

  const emitRoutes = (level: number, cap = 22) => {
    const all = (routesByDepth.get(level) ?? [])
      .filter((i) => {
        const p = d.routes[i].p
        return under(p === '/' ? '' : p.slice(1))
      })
      .sort((a, b) => d.routes[a].p.localeCompare(d.routes[b].p))
    const shown = all.slice(0, Math.max(0, cap))
    for (const ri of shown) {
      const r = d.routes[ri]
      const id = 'r:' + ri
      nodes.push(
        node(id, level * COL, nextY(level), {
          kind: 'route',
          title: r.p,
          badge: r.m,
          sub: `${r.ch.length} ${r.ch.length === 1 ? 'step' : 'steps'}`,
          href: routeHref(ri),
        }, 176, 58),
      )
    }
    if (all.length > shown.length && cap > 0) {
      const id = 'more:' + level
      nodes.push(
        node(id, level * COL, nextY(level), {
          kind: 'note',
          title: `+${all.length - shown.length} more routes`,
          sub: 'search to reach them',
        }, 176, 58),
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
    node(rootId, 0, nextY(0), {
      kind: 'root',
      title: d.repo,
      sub: d.mod,
      badge: `${d.stats.routes} routes`,
      status: depth === 0 ? 'cur' : 'done',
      href: '/',
    }, 176, 58),
  )

  const groupsByDepth = new Map<number, Set<string>>()
  const routesByDepth = new Map<number, number[]>()
  const parentOf = new Map<string, string>()

  d.routes.forEach((r, i) => {
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
    const wanted = 'g:' + segs.slice(0, level + 1).join('/')
    const set = [...(groupsByDepth.get(level) ?? [])].filter(under).sort()
    for (const path of set) {
      const id = 'g:' + path
      const name = path.split('/').pop() ?? path
      const count = d.routes.filter((r) => r.p === '/' + path + '/' || r.p.startsWith('/' + path + '/')).length
      const isWanted = id === wanted
      if (!colPath.has(level + 1)) colPath.set(level + 1, path)
      nodes.push(
        node(id, (level + 1) * COL, nextY(level + 1), {
          kind: 'group',
          title: name,
          sub: '/' + path,
          badge: String(count),
          status: isWanted ? 'cur' : 'idle',
          href: '/g/' + path,
        }, 176, 58),
      )
    }
    emitRoutes(level + 1, 15 - set.length)
  }

  const openDepth = depth
  const set = [...(groupsByDepth.get(openDepth) ?? [])].filter(under).sort()
  for (const path of set) {
    const id = 'g:' + path
    const name = path.split('/').pop() ?? path
    const count = d.routes.filter((r) => r.p.startsWith('/' + path + '/')).length
    if (!colPath.has(openDepth + 1)) colPath.set(openDepth + 1, path)
    nodes.push(
      node(id, (openDepth + 1) * COL, nextY(openDepth + 1), {
        kind: 'group',
        title: name,
        sub: '/' + path,
        badge: String(count),
        href: '/g/' + path,
      }, 210, 58),
    )
  }
  emitRoutes(openDepth + 1, 15 - set.length)

  const laneLevel = openDepth + 2
  const laneRoutes = d.routes.filter((r) => !focus || r.p === '/' + focus || r.p.startsWith('/' + focus + '/'))
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
        node('lane:' + t, laneLevel * COL, nextY(laneLevel), {
          kind: 'group',
          title: tagLabel[t] ?? t,
          sub: `${hit[t]} ${hit[t] === 1 ? 'route' : 'routes'}`,
          tags: [t],
        }, 176, 58),
      )
    }
  }

  // the focused chain must never have a hole, even if a column was filtered out
  for (let k = 0; k < depth; k++) {
    const path = segs.slice(0, k + 1).join('/')
    const id = 'g:' + path
    if (!nodes.some((n) => n.id === id)) {
      nodes.push(
        node(id, (k + 1) * COL, nextY(k + 1), {
          kind: 'group',
          title: path.split('/').pop() ?? path,
          sub: '/' + path,
          href: '/g/' + path,
        }, 176, 58),
      )
    }
  }

  for (let k = 0; k < depth; k++) {
    const from = k === 0 ? rootId : 'g:' + segs.slice(0, k).join('/')
    const to = 'g:' + segs.slice(0, k + 1).join('/')
    if (nodes.some((n) => n.id === from) && nodes.some((n) => n.id === to)) edges.push(cross(from, to))
  }

  const columns = [...new Set(nodes.map((n) => Math.round(n.position.x / COL)))].filter((c) => c <= openDepth + 1).sort((a, b) => a - b)
  const headers: SNode[] = columns.map((c) => {
    const base = segs.slice(0, Math.min(c, depth)).join('/')
    const label = c === 0 ? 'server' : c > depth ? (base ? '/' + base + '/*' : '/*') : '/' + base
    return node('h:' + c, c * COL, -42, { kind: 'label', title: label, w: 176, h: 18 }, 176, 18)
  })
  nodes.unshift(...headers)

  return { nodes, edges, steps: 0, key: 'router:' + focus }
}

/** Level 2: one request walking its middleware chain. */
export function buildPipelineScene(
  r: Route,
  ri: number,
  step: number,
  timing?: RouteTiming,
  flows?: Record<string, FuncFlow>,
): Scene {
  const nodes: SNode[] = []
  const edges: SEdge[] = []
  const x = 150
  let y = 0
  const gap = 84
  const status = (i: number): Status =>
    step < 0 ? 'idle' : i < step ? 'done' : i === step ? 'cur' : 'idle'

  nodes.push(
    node('in', x, y, {
      kind: 'request',
      title: 'request in',
      sub: `${r.m} ${r.p}`,
      index: 0,
      status: status(0),
    }, 380, 66),
  )
  y += gap

  const early: { from: string; label: string }[] = []
  const lane = new Map<string, { y: number; from: string }>()

  r.ch.forEach((st, i) => {
    const id = 's' + i
    const t = timing?.st.find((x) => x.i === i)
    nodes.push(
      node(id, x, y, {
        kind: 'stage',
        title: st.n,
        sub: st.f ? `${st.f}:${st.l}` : undefined,
        badge: st.d ? undefined : st.r ?? st.k,
        tags: touchTags(st),
        index: i + 1,
        status: status(i + 1),
        href: stageHref(ri, i),
        ms: t ? msOf(t.m) : undefined,
        share: t?.sh,
      }, 380, 76),
    )
    edges.push(edge(i === 0 ? 'in' : 's' + (i - 1), id, status(i + 1) === 'cur'))

    // early returns converge on the response node instead of sitting beside the spine
    const exits = ((st.fk ? flows?.[st.fk]?.flow : undefined) ?? []).filter((e) => e.k === 'exit')
    if (exits.length) {
      const codes = [...new Set(exits.map((e) => e.cd).filter(Boolean))] as string[]
      early.push({ from: id, label: codes.join(' · ') || 'exits' })
    }
    const tags = new Set(touchTags(st).filter((t) => t === 'db' || t === 'cache' || t === 'net'))
    for (const step of (st.fk ? flows?.[st.fk]?.flow : undefined) ?? [])
      if (step.g === 'db' || step.g === 'cache' || step.g === 'net') tags.add(step.g)
    for (const t of tags) if (!lane.has(t)) lane.set(t, { y, from: id })
    y += gap
  })

  const last = r.ch.length
  nodes.push(
    node('out', x, y, {
      kind: 'response',
      title: 'response out',
      sub: timing ? `per request, from ${timing.spans} spans` : undefined,
      index: last + 1,
      status: status(last + 1),
      ms: timing ? msOf(timing.total) : undefined,
    }, 380, 60),
  )
  if (last > 0) edges.push(edge('s' + (last - 1), 'out', status(last + 1) === 'cur'))

  for (const e of early) {
    edges.push({
      id: e.from + '->out',
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

  for (const [tag, at] of lane) {
    const id = 'l:' + tag
    nodes.push(
      node(id, 520, at.y, {
        kind: 'note',
        title: tagLabel[tag] ?? tag,
        sub: 'reached from this stage',
        tags: [tag],
      }, 200, 54),
    )
    edges.push({ ...cross(at.from, id), type: 'straight', style: { stroke: '#2b535b', strokeWidth: 1 } })
  }

  return { nodes, edges, steps: last + 2, key: 'pipe:' + ri }
}

function flowNodeId(prefix: string, i: number) {
  return `${prefix}:${i}`
}

/** Level 3: the code path of one function — a spine of calls, exits to the right. */
export function buildFlowScene(flow: FuncFlow, key: string, step: number, cont?: { label: string; href: string }): Scene {
  const nodes: SNode[] = []
  const edges: SEdge[] = []
  const steps = flow.flow ?? []
  const x = 0
  const gap = 88
  let y = 0
  let lastId: string | null = null
  let n = 0
  const earlies: { from: string; label: string }[] = []

  const status = (i: number): Status =>
    step < 0 ? 'idle' : i < step ? 'done' : i === step ? 'cur' : 'idle'

  const walk = (list: Step[], indent: number) => {
    for (const st of list) {
      n += 1
      const id = flowNodeId(key, n)
      const isExit = st.k === 'exit'
      const isIf = st.k === 'if' || st.k === 'switch' || (st.k === 'note' && !!st.b)
      const kind: NodeKind = isExit
        ? 'exit'
        : st.k === 'loop'
          ? 'loop'
          : st.k === 'ret'
            ? 'response'
            : st.k === 'note' || st.k === 'switch' || st.k === 'if'
              ? 'note'
              : 'call'

      if (isExit && lastId) {
        nodes.push(
          node(id, x - 420, y - gap, {
            kind: 'exit',
            title: st.t,
            sub: 'returns here',
            badge: st.cd ? `${st.cd} out` : 'exits',
            index: n,
            status: status(n),
          }, 380, 78),
        )
        edges.push({
          id: `${lastId}->${id}`,
          source: lastId,
          target: id,
          sourceHandle: 'lo',
          targetHandle: 'ri',
          type: 'straight',
          style: { stroke: '#e0a33e', strokeDasharray: '4 3', strokeWidth: 1.2, opacity: 0.8 },
        })
        earlies.push({ from: id, label: st.cd ? `${st.cd} out` : 'exits' })
        y += gap
        continue
      }

      nodes.push(
        node(id, x + indent, y, {
          kind,
          title: st.t + (st.c && st.c > 1 ? ` ×${st.c}` : ''),
          sub: st.s,
          badge: st.g ? st.g : undefined,
          index: n,
          status: status(n),
          line: st.l,
          href: st.fk ? fnHref(st.fk) : undefined,
        }, 380 - indent * 2, 78),
      )
      if (lastId) edges.push(edge(lastId, id, status(n) === 'cur'))
      lastId = id
      y += gap

      nodes.push(
        node(id, x + indent, y, {
          kind,
          title: st.t + (st.c && st.c > 1 ? ` ×${st.c}` : ''),
          sub: st.s,
          badge: st.g ? st.g : undefined,
          index: n,
          status: status(n),
          href: st.fk ? fnHref(st.fk) : undefined,
        }, 380 - indent * 2, 78),
      )
      if (lastId) edges.push(edge(lastId, id, status(n) === 'cur'))
      lastId = id
      y += gap

      // the false path is the default: the spine keeps going. the true branch
      // runs in its own lane so it cannot be mistaken for the main flow.
      if (isIf) {
        const laneX = x + 430
        let ly = y - gap
        const tru = st.b ?? []
        tru.forEach((_, bi) => {
          const br = tru[bi]
          nodes.push(
            node(`${id}:t${bi}`, laneX, ly, {
              kind: br.k === 'ret' ? 'response' : br.k === 'call' ? 'call' : 'note',
              title: br.t,
              sub: br.s,
              badge: br.cd ? `${br.cd} out` : 'true',
              line: br.l,
            }, 380, 70),
          )
          ly += gap
        })
        if (tru.length) {
          edges.push({
            id: `${id}->t0`,
            source: id,
            target: `${id}:t0`,
            sourceHandle: 'r',
            targetHandle: 'l',
            type: 'straight',
            label: 'true',
            style: { stroke: '#58e0b7', strokeWidth: 1.2, opacity: 0.7 },
          })
        }
      }
      if (st.e?.length) walk(st.e, indent)
    }
  }
  walk(steps, 0)

  if (earlies.length) {
    const outId = 'flowout'
    nodes.push(
      node(outId, x - 420, y, {
        kind: 'response',
        title: 'response out',
        sub: earlies.length === 1 ? '1 early return above' : `${earlies.length} early returns above`,
      }, 380, 60),
    )
    earlies.forEach((e, i) => {
      edges.push({
        id: `${e.from}->${outId}-${i}`,
        source: e.from,
        target: outId,
        sourceHandle: 'lo',
        targetHandle: 'l',
        type: 'exitRail',
        label: e.label,
        style: { stroke: '#e0a33e', strokeDasharray: '4 3', strokeWidth: 1.4 },
      })
    })
  }
  if (cont) {
    nodes.push(
      node('nextstage', x, y, {
        kind: 'note',
        title: `next · ${cont.label}`,
        sub: 'continue to the next filter on this request',
        badge: 'jump',
        href: cont.href,
      }, 380, 60),
    )
    if (earlies.length)
      edges.push({ id: 'flowout->next', source: 'flowout', target: 'nextstage', type: 'straight', style: { stroke: '#2b535b', strokeWidth: 1 } })
    y += 60
  }
  nodes.unshift(node('lh', x, -42, { kind: 'label', title: 'calls', w: 380, h: 18 }, 380, 18))

  return { nodes, edges, steps: Math.max(n, 0), key: 'flow:' + key }
}
