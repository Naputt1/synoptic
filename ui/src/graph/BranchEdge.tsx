import { BaseEdge, EdgeLabelRenderer, type EdgeProps } from '@xyflow/react'
import { memo } from 'react'

/**
 * A branch edge leaves the condition on its right, runs to the branch column,
 * then drops into the top of its target. Entering at the top keeps the target's
 * left side free for its return line.
 */
function BranchEdgeInner({ id, label, sourceX, sourceY, targetX, targetY }: EdgeProps) {
  const r = 8
  const ty = targetY + 2
  const down = ty >= sourceY ? 1 : -1
  const d =
    Math.abs(sourceY - ty) < 1
      ? `M ${sourceX} ${sourceY} H ${targetX}`
      : [
          `M ${sourceX} ${sourceY}`,
          `H ${targetX - r}`,
          `Q ${targetX} ${sourceY} ${targetX} ${sourceY + down * r}`,
          `V ${ty}`,
        ].join(' ')
  const text = typeof label === 'string' ? label : ''
  const lx = targetX - 26
  const ly = sourceY - 7

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

export const BranchEdge = memo(BranchEdgeInner)
