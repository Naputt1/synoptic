import { BaseEdge, EdgeLabelRenderer, type EdgeProps, getSmoothStepPath } from '@xyflow/react'
import { memo } from 'react'

import { statusColor, statusLineTone, statusTone } from '../lib/status'

/** A return line leaves the card and ends in its status circle. */
function ReturnEdgeInner({
  id,
  label,
  data,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
}: EdgeProps) {
  const straight = Math.abs(sourceY - targetY) < 1
  // return circles are 3px narrower on the right than their handle centre
  const tx = targetX - 3
  const [path, labelX, labelY] = straight
    ? ([`M ${sourceX} ${sourceY} H ${tx}`, (sourceX + tx) / 2, sourceY] as const)
    : getSmoothStepPath({
        sourceX,
        sourceY,
        targetX: tx,
        targetY,
        sourcePosition,
        targetPosition,
        borderRadius: 10,
        offset: 10,
      })
  const text = typeof label === 'string' ? label : ''
  const code = typeof data?.code === 'string' ? data.code : undefined
  const tone = statusTone(code)

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        style={{
          stroke: statusColor[tone],
          strokeDasharray: '4 3',
          strokeWidth: 1.3,
          opacity: 0.85,
        }}
      />
      {text && (
        <EdgeLabelRenderer>
          <div
            style={{ transform: `translate(-50%,-50%) translate(${labelX}px,${labelY}px)` }}
            className={`bg-canvas/95 pointer-events-none absolute rounded-[3px] border px-1 text-[8.5px] tracking-[0.08em] ${statusLineTone[tone]}`}
          >
            {text}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  )
}

export const ReturnEdge = memo(ReturnEdgeInner)
