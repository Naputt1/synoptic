import { BaseEdge, type EdgeProps, getStraightPath, useInternalNode } from '@xyflow/react'

type Box = { x: number; y: number; w: number; h: number }

type Internal = {
  measured: { width?: number; height?: number }
  internals: { positionAbsolute: { x: number; y: number } }
}

const centerOf = (node: Internal): Box => {
  const w = node.measured.width ?? 14
  const h = node.measured.height ?? 14
  return {
    x: node.internals.positionAbsolute.x + w / 2,
    y: node.internals.positionAbsolute.y + h / 2,
    w,
    h,
  }
}

/** Where the line from one package dot to another leaves the dot. */
const border = (from: Box, to: Box) => {
  const dx = to.x - from.x
  const dy = to.y - from.y
  const len = Math.hypot(dx, dy)
  if (len === 0) return { x: from.x, y: from.y }
  const r = from.w / 2 + 1
  return { x: from.x + (dx / len) * r, y: from.y + (dy / len) * r }
}

/** A straight import arrow that floats between the two dots as they drag. */
export function DepEdge({ id, source, target, markerEnd, style }: EdgeProps) {
  const a = useInternalNode(source)
  const b = useInternalNode(target)
  if (!a || !b) return null
  const ca = centerOf(a)
  const cb = centerOf(b)
  const s = border(ca, cb)
  const t = border(cb, ca)
  const [path] = getStraightPath({
    sourceX: s.x,
    sourceY: s.y,
    targetX: t.x,
    targetY: t.y,
  })
  return <BaseEdge id={id} path={path} markerEnd={markerEnd} style={style} />
}
