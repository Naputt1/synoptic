import { BaseEdge, EdgeLabelRenderer, getStraightPath, type EdgeProps } from '@xyflow/react'
import { memo } from 'react'
import type { SEdge } from './scenes'

function FlowEdgeInner({
  id,
  label,
  sourceX,
  sourceY,
  targetX,
  targetY,
  data,
}: EdgeProps<SEdge>) {
  // the measured top handle centre sits 2px above the visible border; extend
  // the line so it touches the node instead of stopping short
  const [path] = getStraightPath({ sourceX, sourceY, targetX, targetY: targetY + 2 })
  const live = Boolean(data?.live)
  const text = typeof label === 'string' ? label : ''
  const mx = (sourceX + targetX) / 2
  const my = (sourceY + targetY) / 2
  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        className={live ? 'edge-live' : undefined}
        style={{
          stroke: live ? 'var(--color-acc)' : 'var(--color-line2)',
          strokeWidth: live ? 1.4 : 1,
          opacity: live ? 1 : 0.72,
        }}
      />
      {live && (
        <circle r={3} fill="var(--color-acc)">
          <animateMotion dur="1.5s" repeatCount="indefinite" path={path} />
        </circle>
      )}
      {text && (
        <EdgeLabelRenderer>
          <div
            style={{ transform: `translate(0,-50%) translate(${mx + 12}px,${my}px)` }}
            className="pointer-events-none absolute rounded-[3px] border border-line2/70 bg-canvas/95 px-1 text-[8.5px] tracking-[0.06em] text-mut"
          >
            {text}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  )
}

export const FlowEdge = memo(FlowEdgeInner)
