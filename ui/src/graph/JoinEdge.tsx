import { BaseEdge, EdgeLabelRenderer, type EdgeProps } from '@xyflow/react'
import { memo } from 'react'

/**
 * A join edge carries a branch that falls through back to the next statement
 * after the if/else. It drops on the branch side, runs left above the target,
 * then enters the target from the top.
 */
function JoinEdgeInner({ id, label, sourceX, sourceY, targetX, targetY }: EdgeProps) {
  const r = 8
  const ty = targetY + 2
  const midY = ty - 20
  const left = targetX < sourceX
  const d = [
    `M ${sourceX} ${sourceY}`,
    `V ${midY - r}`,
    `Q ${sourceX} ${midY} ${sourceX + (left ? -r : r)} ${midY}`,
    `H ${targetX + (left ? r : -r)}`,
    `Q ${targetX} ${midY} ${targetX} ${midY + r}`,
    `V ${ty}`,
  ].join(' ')
  const text = typeof label === 'string' ? label : ''
  const lx = (sourceX + targetX) / 2
  const ly = midY - 7

  return (
    <>
      <BaseEdge id={id} path={d} style={{ stroke: '#58e0b7', strokeWidth: 1.1, opacity: 0.8 }} />
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

export const JoinEdge = memo(JoinEdgeInner)
