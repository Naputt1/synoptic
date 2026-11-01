import type { FuncFlow, Step } from '../lib/types'
import { fnHref } from '../lib/data'
import type { Scene, SEdge, SNode } from './scenes'

const GAP = 88
const SPINE_W = 380
const CARD_H = 74
const LANE_X = 470
const RAIL_X = -470

type Status = 'idle' | 'cur' | 'done'
type NodeKind = 'call' | 'exit' | 'note' | 'loop' | 'response' | 'label'

function card(id: string, x: number, y: number, data: Record<string, unknown>, w = SPINE_W, h = CARD_H): SNode {
  return { id, type: 'scene', position: { x, y }, data: { ...data, w, h } } as unknown as SNode
}

function kindOf(s: Step): NodeKind {
  switch (s.k) {
    case 'loop':
      return 'loop'
    case 'ret':
      return 'response'
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

function titleOf(s: Step, index?: number) {
  const base = s.t ?? ''
  return index ? base + (s.c && s.c > 1 ? ` ×${s.c}` : '') : base
}

/**
 * Level 3 scene. Three cursors, kept apart on purpose:
 *   spine  — the default path (false / fall-through), always straight down
 *   lane   — the true branch of a condition, one column to the right
 *   rail   — early returns, one column to the left, all landing on response out
 */
export function buildFlowScene(
  flow: FuncFlow,
  key: string,
  step: number,
  cont?: { label: string; href: string },
): Scene {
  const nodes: SNode[] = []
  const edges: SEdge[] = []
  const exits: { id: string; label: string }[] = []
  const startLine = flow.l ?? 1

  let n = 0
  let spineY = 0
  let lastSpine: string | null = null

  const status = (i: number): Status => (step < 0 ? 'idle' : i < step ? 'done' : i === step ? 'cur' : 'idle')

  const link = (from: string, to: string, live: boolean) =>
    edges.push({ id: `${from}->${to}`, source: from, target: to, type: 'flow', animated: live })

  const addSpine = (s: Step): string => {
    n += 1
    const id = `n${n}`
    nodes.push(
      card(id, 0, spineY, {
        kind: kindOf(s),
        title: titleOf(s, 1),
        sub: s.s,
        badge: s.g ? s.g : undefined,
        index: n,
        status: status(n),
        href: s.fk ? fnHref(s.fk) : undefined,
        line: s.l,
        startLine,
      }),
    )
    if (lastSpine) link(lastSpine, id, status(n) === 'cur')
    lastSpine = id
    spineY += GAP
    return id
  }

  let laneFloor = 0
  const laneReturns: string[] = []
  const addLane = (from: string, branch: Step[], laneX: number, top: number) => {
    let y = Math.max(top, laneFloor) + GAP / 2
    branch.forEach((b, i) => {
      const id = `${from}t${i}`
      nodes.push(
        card(id, laneX, y, {
          kind: kindOf(b),
          title: titleOf(b),
          sub: b.s,
          badge: b.cd ? `${b.cd} out` : 'true',
          status: status(n++),
          line: b.l,
          startLine,
        }),
      )
      if (i === 0)
        edges.push({
          id: `${from}->${id}`,
          source: from,
          target: id,
          sourceHandle: 'r',
          targetHandle: 'l',
          type: 'straight',
          label: 'true',
          style: { stroke: '#58e0b7', strokeWidth: 1.2, opacity: 0.75 },
        })
      if (b.k === 'exit' || b.k === 'ret') laneReturns.push(id)
      y += GAP
    })
    laneFloor = y
  }

  const walk = (list: Step[], laneX: number) => {
    for (const s of list) {
      if (s.k === 'exit') {
        const id = `x${exits.length + 1}`
        nodes.push(
          card(id, RAIL_X, Math.max(0, spineY - GAP), {
            kind: 'exit',
            title: s.t,
            sub: 'returns here',
            badge: s.cd ? `${s.cd} out` : 'exits',
            line: s.l,
            startLine,
          }),
        )
        exits.push({ id, label: s.cd ? `${s.cd} out` : 'exits' })
        if (lastSpine)
          edges.push({
            id: `${lastSpine}->${id}`,
            source: lastSpine,
            target: id,
            sourceHandle: 'lo',
            targetHandle: 'ri',
            type: 'straight',
            style: { stroke: '#e0a33e', strokeDasharray: '4 3', strokeWidth: 1.2, opacity: 0.8 },
          })
        continue
      }

      const id = addSpine(s)
      if (s.b?.length) addLane(id, s.b, laneX, spineY - GAP)
      // the false path is the default: the spine carries on, through the else
      // statements when the source has them, or straight to the next statement.
      if (s.e?.length) walk(s.e, laneX + 420)
    }
  }

  walk(flow.flow ?? [], LANE_X)

  if (exits.length || laneReturns.length) {
    nodes.push(
      card('flowout', RAIL_X, Math.max(spineY, laneFloor) + 16, {
        kind: 'response',
        title: 'response out',
        sub: exits.length === 1 ? '1 early return above' : `${exits.length} early returns above`,
      }, SPINE_W, 60),
    )
    exits.forEach((e, i) =>
      edges.push({
        id: `${e.id}->flowout${i}`,
        source: e.id,
        target: 'flowout',
        sourceHandle: 'lo',
        targetHandle: 'l',
        type: 'exitRail',
        label: e.label,
        style: { stroke: '#e0a33e', strokeDasharray: '4 3', strokeWidth: 1.4 },
      }),
    )
    laneReturns.forEach((id, i) =>
      edges.push({
        id: `${id}->flowout${i}`,
        source: id,
        target: 'flowout',
        sourceHandle: 'lo',
        targetHandle: 'l',
        type: 'smoothstep',
        label: 'returns',
        style: { stroke: '#e0a33e', strokeDasharray: '4 3', strokeWidth: 1.4 },
      }),
    )
    spineY += 60
  }

  if (cont) {
    nodes.push(
      card('nextstage', 0, Math.max(spineY, laneFloor) + (exits.length ? 96 : 16), {
        kind: 'note',
        title: `next · ${cont.label}`,
        sub: 'continue to the next filter on this request',
        badge: 'jump',
        href: cont.href,
      }, SPINE_W, 60),
    )
  }

  if (nodes.length) {
    nodes.unshift(card('lh', 0, -42, { kind: 'label', title: 'calls', w: SPINE_W, h: 18 }, SPINE_W, 18))
    if (exits.length) nodes.unshift(card('lh2', RAIL_X, -42, { kind: 'label', title: 'returns', w: SPINE_W, h: 18 }, SPINE_W, 18))
    if (nodes.some((x) => (x.position.x as number) >= LANE_X)) nodes.unshift(card('lh3', LANE_X, -42, { kind: 'label', title: 'true branch', w: SPINE_W, h: 18 }, SPINE_W, 18))
  }

  return { nodes, edges, steps: n, key: 'flow:' + key }
}
