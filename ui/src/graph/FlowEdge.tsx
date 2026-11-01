import { BaseEdge, getSmoothStepPath, type EdgeProps } from '@xyflow/react'
import { memo } from 'react'
import type { SEdge } from './scenes'

function FlowEdgeInner({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
}: EdgeProps<SEdge>) {
  const [path] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    borderRadius: 10,
  })
  const live = Boolean(data?.live)
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
    </>
  )
}

export const FlowEdge = memo(FlowEdgeInner)
