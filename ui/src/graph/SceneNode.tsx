import { Handle, Position, type NodeProps } from '@xyflow/react'
import { motion, useReducedMotion } from 'motion/react'
import { memo } from 'react'
import type { SData, SNode } from './scenes'

const kindBadge: Record<string, string> = {
  root: 'server',
  group: 'group',
  route: 'route',
  stage: 'stage',
  call: 'call',
  request: 'in',
  response: 'out',
  exit: 'exit',
  loop: 'loop',
  note: 'control',
}

const methodTone: Record<string, string> = {
  GET: 'text-info',
  POST: 'text-acc',
  PUT: 'text-warn',
  PATCH: 'text-warn',
  DELETE: 'text-bad',
  ANY: 'text-mut',
}

function Tag({ t }: { t: string }) {
  const label = t === 'db' ? 'database' : t === 'cache' ? 'cache' : t === 'net' ? 'outbound' : t
  const tone =
    t === 'db' ? 'text-warn border-warn/40' : t === 'cache' ? 'text-info border-info/40' : 'text-mut border-line'
  return (
    <span className={`rounded-[3px] border px-1 py-px text-[10px] uppercase tracking-[0.1em] ${tone}`}>
      {label}
    </span>
  )
}

function SceneNodeInner({ data }: NodeProps<SNode>) {
  const d = data as SData
  const reduce = useReducedMotion()
  const isBanner = d.kind === 'request' || d.kind === 'response'
  const isExit = d.kind === 'exit'
  const isRoot = d.kind === 'root'
  const isRoute = d.kind === 'route'
  const isStage = d.kind === 'stage'
  const clickable = Boolean(d.href)
  const isLabel = d.kind === 'label'

  const statusRing =
    d.status === 'cur'
      ? 'ring-1 ring-acc/70 shadow-[0_0_22px_-4px_rgba(88,224,183,0.45)]'
      : d.status === 'done'
        ? 'ring-1 ring-acc/25'
        : ''

  if (isLabel) {
    return (
      <div className="px-0.5 text-[9.5px] uppercase tracking-[0.18em] text-faint">{d.title}</div>
    )
  }

  return (
    <motion.div
      initial={reduce ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.24, delay: Math.min((d.index ?? 0) * 0.018, 0.5), ease: [0.22, 0.7, 0.3, 1] }}
      style={{ width: (d.w as number) ?? 380, height: (d.h as number) ?? 84 }}
      className={[
        'relative flex flex-col justify-center overflow-hidden rounded-[6px] border px-2.5 py-2 text-ink',
        isBanner
          ? 'border-dashed border-acc/35 bg-transparent text-acc/90'
          : isExit
            ? 'border-dashed border-warn/45 bg-warn/[0.04]'
            : isRoot
              ? 'border-line2 bg-panel2'
              : 'border-line bg-panel',
        statusRing,
        clickable ? 'cursor-pointer transition-colors hover:border-acc/60 hover:bg-panel2' : '',
      ].join(' ')}
    >
      <Handle type="target" position={Position.Top} id="t" />
      <Handle type="source" position={Position.Bottom} id="b" />
      <Handle type="target" position={Position.Left} id="l" />
      <Handle type="source" position={Position.Right} id="r" />
      <Handle type="target" position={Position.Right} id="ri" style={{ top: "50%" }} />
      <Handle type="source" position={Position.Left} id="lo" style={{ top: "50%" }} />

      <span
        className={[
          'absolute -left-[19px] top-[13px] size-[7px] rounded-full border',
          d.status === 'done' ? 'border-acc bg-acc' : d.status === 'cur' ? 'border-acc bg-acc shadow-[0_0_8px_rgba(88,224,183,.8)]' : 'border-line2 bg-canvas',
        ].join(' ')}
      />

      <div className="flex items-center gap-1.5">
        {d.index !== undefined && d.index > 0 && !isBanner && (
          <span className="rounded-[3px] border border-line px-1 text-[10px] text-faint">{d.index}</span>
        )}
        {isRoute && d.badge && (
          <span className={`text-[10px] font-semibold tracking-[0.08em] ${methodTone[d.badge] ?? 'text-mut'}`}>
            {d.badge}
          </span>
        )}
        <span className={`min-w-0 flex-1 truncate ${isBanner ? 'text-[11px] tracking-[0.06em]' : 'text-[12px]'}`}>
          {d.kind === 'loop' && <span className="mr-1 text-warn">↺</span>}
          {d.title}
        </span>
        {d.badge && !isRoute && !isExit && (
          <span className="shrink-0 rounded-[3px] border border-line px-1 text-[9.5px] uppercase tracking-[0.1em] text-mut">
            {d.badge}
          </span>
        )}
        {clickable && <span className="shrink-0 text-[10px] text-faint">open ›</span>}
      </div>

      {(d.sub || (d.tags && d.tags.length > 0) || isStage) && (
        <div className="mt-1 flex flex-wrap items-center gap-1.5">
          {d.sub && (
            <span className="min-w-0 flex-1 truncate text-[10.5px] text-mut" title={d.sub}>
              {d.sub}
            </span>
          )}
          {(d.tags ?? []).map((t) => (
            <Tag key={t} t={t} />
          ))}
          {d.ms && <span className="ml-auto shrink-0 tabular-nums text-[10.5px] text-info">{d.ms}</span>}
          {isStage && !d.badge && <span className="text-[9.5px] text-faint">{kindBadge[d.kind]}</span>}
        </div>
      )}

      {typeof d.share === 'number' && d.share > 0 && (
        <span className="pointer-events-none absolute inset-x-[7px] bottom-[5px] h-[2px] rounded-full bg-line">
          <span
            className="block h-full rounded-full bg-acc/70"
            style={{ width: `${Math.min(100, Math.max(2, Math.round(d.share * 100)))}%` }}
          />
        </span>
      )}

      {isExit && d.badge && (
        <div className="mt-1 self-start rounded-[3px] border border-warn/45 px-1 text-[10px] text-warn">{d.badge}</div>
      )}
    </motion.div>
  )
}

export const SceneNode = memo(SceneNodeInner)
