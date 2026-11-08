import { BaseEdge, EdgeLabelRenderer, type EdgeProps } from '@xyflow/react'
import { memo } from 'react'
import type { SEdge } from './scenes'

/**
 * A loop back edge leaves the bottom of its card, curves onto a rail with
 * clearance beside the loop, then curves up into the bottom of the loop.
 */
function LoopEdgeInner({ id, label, sourceX, sourceY, targetX, targetY, data }: EdgeProps<SEdge>) {
  const r = 24
  const railX = typeof data?.railX === 'number' ? data.railX : targetX
  const left = railX < sourceX
  const dir = left ? -1 : 1
  const up = targetY < sourceY ? -1 : 1
  const startY = sourceY + 22
  const overY = targetY - up * 26
  const direct = Math.abs(railX - targetX) < 1
  const d = [
    `M ${sourceX} ${sourceY}`,
    `C ${sourceX} ${startY} ${sourceX} ${startY} ${sourceX + dir * r} ${startY}`,
    `H ${railX - dir * r}`,
    `Q ${railX} ${startY} ${railX} ${startY + up * r}`,
    `V ${direct ? targetY : overY}`,
    ...(direct
      ? []
      : [
          `Q ${railX} ${overY} ${railX - dir * r} ${overY}`,
          `H ${targetX - dir * r}`,
          `Q ${targetX} ${overY} ${targetX} ${targetY}`,
        ]),
  ].join(' ')

  const text = typeof label === 'string' ? label : ''
  const lx = railX + (left ? 12 : -12)
  const ly = (sourceY + targetY) / 2

  return (
    <>
      <BaseEdge id={id} path={d} style={{ stroke: '#58e0b7', strokeWidth: 1.2, opacity: 0.85 }} />
      {text && (
        <EdgeLabelRenderer>
          <div
            style={{ transform: `translate(-50%,-50%) translate(${lx}px,${ly}px)` }}
            className="pointer-events-none absolute rounded-[3px] border border-acc/25 bg-canvas/95 px-1 text-[8.5px] tracking-[0.06em] text-acc"
          >
            {text}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  )
}

export const LoopEdge = memo(LoopEdgeInner)
