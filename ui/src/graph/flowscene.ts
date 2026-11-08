import { fnHref } from '../lib/data'
import { codesLabel, codesOf } from '../lib/status'
import type { FuncFlow, Input, Step } from '../lib/types'
import type { Scene, SEdge, SNode } from './scenes'

const GAP = 88
const SPINE_W = 380
const CARD_H = 74
const COL_W = 520
const RET_W = 32
const RET_DY = (CARD_H - RET_W) / 2
const RAIL_X = -520
const INPUT_H = 44
const INPUT_GAP = 56

type NodeKind = 'call' | 'exit' | 'note' | 'loop' | 'response' | 'return' | 'input' | 'label'

function card(
  id: string,
  x: number,
  y: number,
  data: Record<string, unknown>,
  w = SPINE_W,
  h = CARD_H,
): SNode {
  return { id, type: 'scene', position: { x, y }, data: { ...data, w, h } } as unknown as SNode
}

function kindOf(s: Step): NodeKind {
  switch (s.k) {
    case 'loop':
      return 'loop'
    case 'ret':
      return 'return'
    case 'exit':
      return 'exit'
    case 'if':
    case 'switch':
    case 'note':
      return 'note'
    default:
      return 'call'
  }
}

function titleOf(s: Step) {
  return (s.t ?? '') + (s.c && s.c > 1 ? ` ×${s.c}` : '')
}

/**
 * Level 3 scene. Every statement gets its own row, like a line of code:
 *
 *   returns   calls / spine        true branch
 *   (rail)    (default path)       (nested path)
 *
 * Depth indents a condition's true body into the next column. Its else body and
 * the following statement continue below, in the same column. Because no two
 * cards share a row, every return can run left to the rail without crossing a
 * card.
 */
export function buildFlowScene(
  flow: FuncFlow,
  key: string,
  cont?: { label: string; href: string },
  prev?: { label: string; href: string },
  inputs?: Input[],
  origin?: { ri: number; si: number },
): Scene {
  const nodes: SNode[] = []
  const edges: SEdge[] = []
  const startLine = flow.l ?? 1

  let n = 0
  let stepNo = 0
  const nextId = () => `n${++n}`
  const nextStep = () => stepNo++

  // one row per statement, like lines of code
  let rowY = 0
  let nextLinked = false

  const pos = new Map<string, { x: number; y: number; w: number; h: number; kind: string }>()
  // status code carried by each return circle, so its line can take the same tone
  const codeOf = new Map<string, string>()

  const place = (id: string, x: number, y: number, w: number, h: number, kind: string) =>
    pos.set(id, { x, y, w, h, kind })

  const flowEdge = (from: string, to: string, label?: string, hot?: string) => {
    const a = pos.get(from)
    const b = pos.get(to)
    if (!a || !b) return
    const text = label === 'false' ? undefined : label
    const across = b.x > a.x + a.w + 8
    const edge: Record<string, unknown> = {
      id: `${from}->${to}`,
      source: from,
      target: to,
      sourceHandle: across ? 'r' : 'b',
      targetHandle: 't',
      type: across ? 'branch' : 'flow',
    }
    if (hot) {
      edge.className = hot
        .split(/\s+/)
        .map((h) => `edge-input-${h}`)
        .join(' ')
      edge.data = { input: hot }
    }
    if (text) edge.label = text
    edges.push(edge as SEdge)
  }

  const returnEdge = (from: string, to: string, label?: string, handle: 'lo' | 'b' = 'lo') => {
    edges.push({
      id: `${from}->${to}`,
      source: from,
      target: to,
      sourceHandle: handle,
      targetHandle: 'ri',
      type: 'return',
      label: label === 'false' ? undefined : label,
      data: { code: codeOf.get(to) ?? '' },
    } as SEdge)
  }

  const joinEdge = (from: string, to: string, label?: string) => {
    edges.push({
      id: `${from}->${to}`,
      source: from,
      target: to,
      sourceHandle: 'b',
      targetHandle: 't',
      type: 'join',
      label: label === 'false' ? undefined : label,
    } as SEdge)
  }

  const loopLabeled = new Set<string>()
  const loopEdge = (from: string, to: string, label?: string) => {
    const a = pos.get(from)
    const b = pos.get(to)
    if (!a || !b) return
    const show = label && !loopLabeled.has(to)
    if (show) loopLabeled.add(to)
    edges.push({
      id: `${from}->${to}`,
      source: from,
      target: to,
      sourceHandle: 'b',
      targetHandle: 'bi',
      type: 'loop',
      label: show ? label : undefined,
      data: { railX: b.x + b.w * 0.75 },
    } as SEdge)
  }

  const addCard = (
    s: Step,
    col: number,
    kind: NodeKind,
    badge?: string,
  ): { id: string; idx: number } => {
    const id = nextId()
    const idx = nextStep()
    const y = rowY
    rowY += GAP
    const jump = cont && s.k === 'call' && s.t.trim() === 'c.Next()' ? cont : undefined
    if (jump) nextLinked = true
    nodes.push(
      card(id, col, y, {
        kind,
        title: jump ? s.t : titleOf(s),
        sub: jump ? `next · ${jump.label}` : s.s,
        badge: jump
          ? 'jump'
          : badge !== undefined
            ? badge
            : kind === 'exit'
              ? codesLabel(s)
                ? `${codesLabel(s)} out`
                : 'exits'
              : kind === 'call'
                ? codesLabel(s) || s.g
                : s.g
                  ? s.g
                  : undefined,
        tags:
          kind === 'exit'
            ? s.g
              ? [s.g]
              : undefined
            : kind === 'call' && codesLabel(s) && s.g
              ? [s.g]
              : undefined,
        rk: kind === 'exit' ? s.rk : undefined,
        index: idx,
        jump: Boolean(jump),
        href: jump
          ? jump.href
          : s.rpc
            ? `/rpc/${s.rpc}`
            : s.fk
              ? fnHref(s.fk, origin, s.ik && s.sk ? { ik: s.ik, sk: s.sk } : undefined)
              : undefined,
        line: s.l,
        lines: s.ls && s.ls.length > 1 ? s.ls : s.sl && s.sl !== s.l ? [s.l, s.sl] : undefined,
        startLine,
        file: s.f ?? flow.f,
        source: true,
      }),
    )
    place(id, col, y, SPINE_W, CARD_H, kind)
    return { id, idx }
  }

  const addReturn = (s: Step, y: number, idx: number): string => {
    const id = nextId()
    const label = codesLabel(s)
    const retSub = s.rk
      ? label
        ? `${label} · ${s.rk} return`
        : `${s.rk} return`
      : label || 'return'
    // one code stays a circle; several widen it into a pill that still fits the text
    const w = Math.max(RET_W, 14 + label.length * 6.2)
    nodes.push(
      card(
        id,
        RAIL_X,
        y + RET_DY,
        {
          kind: 'return',
          title: s.t || 'return',
          badge: label,
          sub: retSub,
          index: idx,
          href: s.fk ? fnHref(s.fk) : undefined,
          line: s.l,
          lines: s.sl && s.sl !== s.l ? [s.l, s.sl] : undefined,
          startLine,
          file: s.f ?? flow.f,
          source: true,
        },
        w,
        RET_W,
      ),
    )
    place(id, RAIL_X, y + RET_DY, w, RET_W, 'return')
    codeOf.set(id, label)
    return id
  }

  const maxLine = (list: Step[]): number => {
    let max = 0
    for (const s of list) {
      max = Math.max(max, s.l ?? 0)
      if (s.b?.length) max = Math.max(max, maxLine(s.b))
      if (s.e?.length) max = Math.max(max, maxLine(s.e))
    }
    return max
  }
  const columnForLine = (list: Step[], line: number, depth: number): number => {
    for (const s of list) {
      if (s.k !== 'if' || !s.b?.length) continue
      const bodyMax = maxLine(s.b)
      if (line > (s.l ?? 0) && line <= bodyMax) return columnForLine(s.b, line, depth + 1)
      if (s.e?.length) {
        const elseMax = maxLine(s.e)
        if (line > bodyMax && line <= elseMax) return columnForLine(s.e, line, depth)
      }
    }
    return depth
  }

  const addInputs = (list: Input[], last: string | null): string | null => {
    type Group = {
      key: string
      access: string
      kind: string
      via: string
      file: string
      names: string[]
      line: number
      last: number
      lines: number[]
    }
    const cards = nodes.filter((n) => {
      const k = (n.data as { kind?: string }).kind
      return k === 'call' || k === 'note' || k === 'loop' || k === 'exit'
    })
    const titleOfNode = (n: SNode) => String((n.data as { title?: string }).title ?? '')
    const lineOfNode = (n: SNode) => Number((n.data as { line?: number }).line ?? 0)
    const linesOfNode = (n: SNode) => {
      const lines = (n.data as { lines?: number[] }).lines
      return lines && lines.length ? lines : [lineOfNode(n)]
    }
    const tagNode = (n: SNode, tag: string) => {
      const data = n.data as { tags?: string[] }
      const tags = data.tags ?? (data.tags = [])
      if (!tags.includes(tag)) tags.push(tag)
    }
    // Keep adjacent accesses together (for example five c.Set calls in a row),
    // but do not drag an input across the flow to an unrelated earlier use.
    const groups: Group[] = []
    for (const x of list) {
      if (x.a !== 'read' && x.a !== 'set' && x.a !== 'read+set' && x.a !== 'dynamic') continue
      const via = x.a === 'set' ? (x.si ?? '') : x.a === 'read' ? (x.ri ?? '') : x.si || x.ri || ''
      const ats = x.a === 'set' ? (x.sas && x.sas.length ? x.sas : x.sa ? [x.sa] : []) : [x.ra]
      let pending = ats.map((at) => Number((at ?? '').split(':').pop())).filter((line) => line > 0)
      const file =
        ats.map((at) => (at ?? '').slice(0, (at ?? '').lastIndexOf(':'))).find(Boolean) ?? ''
      // a direct c.Set/SetCookie has its own call card: tag that line instead
      // of drawing the same write twice. a key can be written in more than one
      // place (branch and tail), so tag every write we found.
      if (x.a === 'set' && !via && pending.length) {
        const unmatched: number[] = []
        for (const line of pending) {
          const write = cards.find(
            (n) =>
              linesOfNode(n).includes(line) &&
              /\.(Set|SetCookie|Add|Header)\(/.test(titleOfNode(n)),
          )
          if (write) tagNode(write, `${x.k}:${x.n}`)
          else unmatched.push(line)
        }
        pending = unmatched
        if (!pending.length) continue
      }
      if (!pending.length) pending = [0]
      const key = [x.a, x.k, via].join('|')
      for (const line of pending) {
        let g: Group | undefined
        for (let i = groups.length - 1; i >= 0; i--) {
          const q = groups[i]
          if (q.key !== key) continue
          if (line === 0 || q.line === 0 || line - q.last <= 4) g = q
          break
        }
        if (!g) {
          g = {
            key,
            access: x.a,
            kind: x.k,
            via,
            file,
            names: [],
            line: Number.isFinite(line) ? line : 0,
            last: 0,
            lines: [],
          }
          groups.push(g)
        }
        if (!g.file) g.file = file
        if (!g.names.includes(x.n)) g.names.push(x.n)
        if (Number.isFinite(line) && line > 0) {
          if (!g.line || line < g.line) g.line = line
          if (line > g.last) g.last = line
          if (!g.lines.includes(line)) g.lines.push(line)
        }
      }
    }

    const bottomOf = () => {
      let bottom = 0
      for (const n of nodes) {
        const h = (n.data as { h?: number }).h ?? CARD_H
        bottom = Math.max(bottom, n.position.y + h)
      }
      return bottom
    }
    const shiftBelow = (y: number, dy: number) => {
      for (const n of nodes) if (n.position.y >= y) n.position.y += dy
      for (const p of pos.values()) if (p.y >= y) p.y += dy
    }

    let gid = 0
    let tail = last
    const ordered = [...groups].sort((a, b) => (a.line || 1e9) - (b.line || 1e9))
    for (const g of ordered) {
      gid++
      const hot = String(gid)
      const leaf = g.via.slice(g.via.lastIndexOf('.') + 1)
      const anchor = g.via
        ? (cards.find((n) => titleOfNode(n).includes(g.via + '(')) ??
          cards.find((n) => titleOfNode(n).includes(leaf + '(')))
        : undefined
      // the access happens inside the helper, so the call card gets a tag
      // instead of a second box beside the flow
      if (g.via && anchor) {
        const tag = g.names.length === 1 ? `${g.kind}:${g.names[0]}` : `${g.kind}×${g.names.length}`
        const data = anchor.data as { tags?: string[] }
        const tags = data.tags ?? (data.tags = [])
        if (!tags.includes(tag)) tags.push(tag)
        continue
      }
      // a helper with no visible call card belongs to another function or
      // middleware: keep it in the Inputs panel, not as a floating box
      if (g.via) continue
      // a line from a different file is not a position in this flow
      if (g.file && flow.f && g.file !== flow.f) continue
      const x = columnForLine(flow.flow ?? [], g.line, 0) * COL_W
      const same = cards.filter((n) => Math.round(n.position.x) === x)
      const next = !anchor
        ? same
            .filter((n) => lineOfNode(n) > 0 && g.line > 0 && lineOfNode(n) >= g.line)
            .sort((a, b) => a.position.y - b.position.y)[0]
        : undefined
      const prev = same
        .filter((n) => !next || n.position.y < next.position.y)
        .sort((a, b) => a.position.y - b.position.y)
        .pop()

      const y = anchor
        ? anchor.position.y + CARD_H + 12
        : next
          ? next.position.y
          : prev
            ? prev.position.y + CARD_H + 12
            : bottomOf() + 16

      shiftBelow(y, INPUT_GAP)
      const id = nextId()
      nodes.push(
        card(
          id,
          x,
          y,
          {
            kind: 'input',
            title:
              g.names.length === 1 ? `${g.kind}:${g.names[0]}` : `${g.kind} ×${g.names.length}`,
            sub: `${g.access} · ${g.via || flow.n || 'this function'}`,
            access: g.access,
            input: hot,
            line: g.line,
            lines: g.lines,
            startLine,
            file: g.file || flow.f,
            source: true,
          },
          SPINE_W,
          INPUT_H,
        ),
      )
      place(id, x, y, SPINE_W, INPUT_H, 'input')

      // keep the line going through the new row instead of behind it
      const crossing = edges.findIndex((e) => {
        if (e.type === 'branch' || e.type === 'join' || e.type === 'return') return false
        const a = pos.get(e.source)
        const b = pos.get(e.target)
        return Boolean(a && b && a.x === x && b.x === x && a.y < y && b.y > y)
      })
      if (crossing >= 0) {
        const old = edges[crossing]
        const oldHot = String((old.data as { input?: string } | undefined)?.input ?? '')
        const both = oldHot && oldHot !== hot ? `${oldHot} ${hot}` : hot
        edges.splice(crossing, 1)
        flowEdge(old.source, id, undefined, both)
        flowEdge(id, old.target, old.label as string | undefined, both)
      } else {
        if (prev) flowEdge(prev.id, id, undefined, hot)
        if (next) flowEdge(id, next.id, undefined, hot)
        if (!prev && !next && x === 0 && tail) flowEdge(tail, id, undefined, hot)
      }
      if (x === 0 && !next) tail = id
    }
    return tail
  }

  const renderList = (
    list: Step[],
    depth: number,
    from: string | null,
    fromLabel?: string,
    anchorY?: number,
    loopTarget?: string,
  ): { tail: string | null; falls: boolean } => {
    const col = depth * COL_W
    let prev = from
    let label = fromLabel
    let join = false
    let falls = true

    const connect = (to: string) => {
      if (!prev) {
        label = undefined
        return
      }
      if (join) {
        joinEdge(prev, to, label)
        join = false
      } else if (pos.get(to)?.kind === 'return') {
        returnEdge(prev, to, label, 'b')
      } else {
        flowEdge(prev, to, label)
      }
      label = undefined
    }

    for (let i = 0; i < list.length; i++) {
      const s = list[i]
      if (s.k === 'ret') {
        // a response ends the request: the circle carries the status
        if (codesOf(s).length) {
          // a lone response in a true body sits on the condition's row, so the
          // line leaves the condition's side and cannot be confused with else
          const anchored = anchorY !== undefined && list.length === 1
          const id = addReturn(s, anchored ? anchorY : rowY, nextStep())
          if (!anchored) rowY += GAP
          if (label === 'false') label = undefined
          if (anchored) {
            if (prev) returnEdge(prev, id, label, 'lo')
            label = undefined
          } else {
            connect(id)
          }
          prev = id
          falls = false
          break
        }

        // a bare return has no status; keep the code line and its circle
        const y = rowY
        const { id, idx } = addCard(s, col, 'exit', '')
        connect(id)
        const rid = addReturn(s, y, idx)
        returnEdge(id, rid)
        prev = id
        falls = false
        break
      }

      if (s.k === 'exit') {
        const y = rowY
        const { id, idx } = addCard(s, col, 'exit')
        connect(id)

        // the failing call leaves here; the false path stays on the column
        const rid = addReturn(s, y, idx)
        returnEdge(id, rid, 'true')

        prev = id
        label = 'false'
        continue
      }

      if (s.k === 'continue') {
        const { id } = addCard(s, col, 'note')
        connect(id)
        if (loopTarget) loopEdge(id, loopTarget, 'loop')
        falls = false
        prev = id
        label = undefined
        continue
      }

      if (s.k === 'if') {
        const cardY = rowY
        const { id } = addCard(s, col, 'note')
        connect(id)

        const tb = s.b?.length
          ? renderList(s.b, depth + 1, id, 'true', cardY, loopTarget)
          : { tail: id, falls: true }
        const eb = s.e?.length
          ? renderList(s.e, depth, id, 'else', undefined, loopTarget)
          : { tail: id, falls: true }

        if (s.e?.length) {
          falls = tb.falls || eb.falls
          if (tb.falls && !eb.falls) {
            // the true side can fall through; join it back past the else
            prev = tb.tail ?? id
            const at = prev ? pos.get(prev) : undefined
            join = !!at && at.x > 0
          } else if (!tb.falls && eb.falls) {
            prev = eb.tail ?? id
          } else {
            prev = id
          }
        } else {
          falls = true
          prev = id
        }
        label = undefined
        continue
      }

      if (s.k === 'loop') {
        const cardY = rowY
        const { id } = addCard(s, col, 'loop')
        connect(id)
        if (s.b?.length) {
          const body = renderList(s.b, depth + 1, id, 'body', cardY, id)
          if (body.tail && body.falls) loopEdge(body.tail, id, 'loop')
          // keep the loop-back rail's lane clear: the next step starts below
          // the whole body, not under the loop card.
          let bottom = cardY
          for (const n of nodes) {
            if (n.position.y < cardY) continue
            const h = (n.data as { h?: number }).h ?? CARD_H
            bottom = Math.max(bottom, n.position.y + h)
          }
          rowY = Math.max(rowY, bottom + GAP)
        }
        falls = true
        prev = id
        label = undefined
        continue
      }

      // consecutive context writes are one statement group, like Register's
      // c.Set block: one card, all source lines, one tag per key
      if (s.k === 'call' && s.s === 'context write') {
        let j = i
        while (j + 1 < list.length && list[j + 1].k === 'call' && list[j + 1].s === 'context write')
          j++
        if (j > i) {
          const run = list.slice(i, j + 1)
          const base = s.t.includes('(') ? s.t.slice(0, s.t.indexOf('(')) : s.t
          const grouped: Step = {
            ...s,
            t: base + '(…)',
            c: run.length,
            ls: run.map((x) => x.l).filter((x): x is number => typeof x === 'number'),
          }
          const { id } = addCard(grouped, col, kindOf(grouped))
          connect(id)
          prev = id
          i = j
          continue
        }
      }

      // call / loop / switch / note
      const { id } = addCard(s, col, kindOf(s))
      connect(id)
      prev = id
    }
    return { tail: prev, falls }
  }

  const last = renderList(flow.flow ?? [], 0, null).tail

  const tail = inputs?.length ? (addInputs(inputs, last) ?? last) : last

  // the next filter sits under the whole flow, on the spine
  if (cont && !nextLinked) {
    let bottom = 0
    for (const node of nodes) {
      const d = node.data as { h?: number }
      bottom = Math.max(bottom, node.position.y + ((d.h as number) ?? CARD_H))
    }
    const y = bottom + 28

    if (cont) {
      const id = nextId()
      nodes.push(
        card(
          id,
          0,
          y,
          {
            kind: 'note',
            title: `next · ${cont.label}`,
            sub: 'continue to the next filter on this request',
            badge: 'jump',
            href: cont.href,
          },
          SPINE_W,
          60,
        ),
      )
      place(id, 0, y, SPINE_W, 60, 'note')
      // the default path leaves the last spine node and continues to next
      const at = tail ? pos.get(tail) : undefined
      if (tail && at && at.x === 0) {
        edges.push({
          id: `${tail}->${id}`,
          source: tail,
          target: id,
          sourceHandle: 'b',
          targetHandle: 't',
          type: 'straight',
        } as SEdge)
      }
    }
  }

  if (nodes.length) {
    nodes.unshift(
      card('lh', 0, -42, { kind: 'label', title: 'calls', w: SPINE_W, h: 18 }, SPINE_W, 18),
    )
    if (nodes.some((x) => x.position.x <= RAIL_X)) {
      nodes.unshift(
        card('lh2', RAIL_X, -42, { kind: 'label', title: 'returns', w: 120, h: 18 }, 120, 18),
      )
    }
    if (nodes.some((x) => x.position.x >= COL_W)) {
      nodes.unshift(
        card(
          'lh3',
          COL_W,
          -42,
          { kind: 'label', title: 'true branch', w: SPINE_W, h: 18 },
          SPINE_W,
          18,
        ),
      )
    }
  }

  // the previous filter sits above the columns, at the top of the stack
  if (prev) {
    const id = nextId()
    const y = -118
    nodes.push(
      card(
        id,
        0,
        y,
        {
          kind: 'note',
          title: `prev · ${prev.label}`,
          sub: 'from the previous filter on this request',
          badge: 'jump',
          href: prev.href,
        },
        SPINE_W,
        60,
      ),
    )
    place(id, 0, y, SPINE_W, 60, 'note')
  }

  return { nodes, edges, key: 'flow:' + key }
}
