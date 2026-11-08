import { BaseEdge, EdgeLabelRenderer, type EdgeProps, getStraightPath } from '@xyflow/react'
import { memo } from 'react'

import type { SEdge } from './scenes'

function FlowEdgeInner({ id, label, sourceX, sourceY, targetX, targetY }: EdgeProps<SEdge>) {
  // the measured top handle centre sits 2px above the visible border; extend
  // the line so it touches the node instead of stopping short
  const [path] = getStraightPath({ sourceX, sourceY, targetX, targetY: targetY + 2 })
  const text = typeof label === 'string' ? label : ''
  const mx = (sourceX + targetX) / 2
  const my = (sourceY + targetY) / 2
  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        style={{
          stroke: 'var(--color-line2)',
          strokeWidth: 1,
          opacity: 0.72,
        }}
      />
      {text && (
        <EdgeLabelRenderer>
          <div
            style={{ transform: `translate(0,-50%) translate(${mx + 12}px,${my}px)` }}
            className="border-line2/70 bg-canvas/95 text-mut pointer-events-none absolute rounded-[3px] border px-1 text-[8.5px] tracking-[0.06em]"
          >
            {text}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  )
}

export const FlowEdge = memo(FlowEdgeInner)
