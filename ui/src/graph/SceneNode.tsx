import { Handle, Position, type NodeProps } from '@xyflow/react'
import { motion, useReducedMotion } from 'motion/react'
import { memo } from 'react'
import type { SData, SNode } from './scenes'
import { statusBadgeTone, statusTone, type StatusTone } from '../lib/status'
import { CardMenu } from './NodeMenu'

const returnTone: Record<StatusTone, { idle: string; done: string; cur: string; text: string; hover: string }> = {
  ok: {
    idle: 'border-acc/45 bg-panel/50',
    done: 'border-acc/60 bg-acc/[0.06]',
    cur: 'border-acc/90 bg-acc/[0.12] shadow-[0_0_18px_-4px_rgba(88,224,183,0.55)]',
    text: 'text-acc',
    hover: 'hover:border-acc/80 hover:bg-acc/[0.08]',
  },
  info: {
    idle: 'border-info/45 bg-panel/50',
    done: 'border-info/60 bg-info/[0.06]',
    cur: 'border-info/90 bg-info/[0.12] shadow-[0_0_18px_-4px_rgba(159,198,255,0.55)]',
    text: 'text-info',
    hover: 'hover:border-info/80 hover:bg-info/[0.08]',
  },
  warn: {
    idle: 'border-warn/45 bg-panel/50',
    done: 'border-warn/55 bg-warn/[0.06]',
    cur: 'border-warn/85 bg-warn/[0.12] shadow-[0_0_18px_-4px_rgba(224,163,62,0.55)]',
    text: 'text-warn',
    hover: 'hover:border-warn/80 hover:bg-warn/[0.08]',
  },
  bad: {
    idle: 'border-bad/45 bg-panel/50',
    done: 'border-bad/60 bg-bad/[0.06]',
    cur: 'border-bad/90 bg-bad/[0.12] shadow-[0_0_18px_-4px_rgba(224,106,106,0.55)]',
    text: 'text-bad',
    hover: 'hover:border-bad/80 hover:bg-bad/[0.08]',
  },
  mut: {
    idle: 'border-line2 bg-panel/50',
    done: 'border-line2 bg-panel2',
    cur: 'border-line2 bg-panel2 shadow-[0_0_14px_-4px_rgba(125,156,153,0.45)]',
    text: 'text-mut',
    hover: 'hover:border-line2 hover:bg-panel2',
  },
}

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
  const kind = t.split(/[:× ]/)[0]
  const tone =
    t === 'db'
      ? 'text-warn border-warn/40'
      : t === 'cache'
        ? 'text-info border-info/40'
        : t === 'error'
          ? 'text-bad border-bad/40'
          : t === 'nil'
            ? 'text-acc border-acc/40'
            : t === 'void'
              ? 'text-mut border-line'
              : t === 'service'
                ? 'text-acc border-acc/40'
                : t === 'auth'
                  ? 'text-info border-info/40'
                  : kind === 'ctx'
          ? 'text-info border-info/40'
          : kind === 'query'
            ? 'text-acc border-acc/40'
            : kind === 'path' || kind === 'cookie' || kind === 'form'
              ? 'text-warn border-warn/40'
              : 'text-mut border-line'
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
  const isReturn = d.kind === 'return'
  const isInput = d.kind === 'input'
  const clickable = Boolean(d.href)
  const isLabel = d.kind === 'label'

  if (isLabel) {
    return (
      <div className="px-0.5 text-[9.5px] uppercase tracking-[0.18em] text-faint">{d.title}</div>
    )
  }

  if (isInput) {
    const access = d.access as string | undefined
    const tone =
      access === 'set'
        ? 'border-acc/45 text-acc'
        : access === 'read'
          ? 'border-info/45 text-info'
          : 'border-warn/45 text-warn'
    const w = (d.w as number) ?? 380
    const wide = w >= 200
    return (
      <CardMenu data={d}>
        <motion.div
          initial={reduce ? false : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.2, delay: Math.min((d.index ?? 0) * 0.018, 0.4), ease: [0.22, 0.7, 0.3, 1] }}
          style={{ width: w, height: (d.h as number) ?? 44 }}
          role="button"
          tabIndex={0}
          aria-label={`${d.title} input group`}
          className={`relative cursor-pointer rounded-[4px] border border-dashed bg-panel ${wide ? 'flex items-center justify-between gap-3 px-3' : 'px-2 py-1'} ${tone}`}
          title={`${d.title} · ${access ?? ''}${d.sub ? ' · ' + d.sub : ''}`}
          onContextMenu={(e) => e.stopPropagation()}
        >
          <Handle type="target" position={Position.Left} id="l" />
          <Handle type="source" position={Position.Right} id="r" />
          <Handle type="target" position={Position.Top} id="t" />
          <Handle type="source" position={Position.Bottom} id="b" />
          <div className="min-w-0 truncate text-[10.5px] leading-tight">{d.title}</div>
          <div className={`truncate text-[8.5px] uppercase tracking-[0.08em] opacity-80 ${wide ? 'shrink-0' : 'mt-0.5'}`}>{d.sub}</div>
        </motion.div>
      </CardMenu>
    )
  }

  if (isReturn) {
    const tone = returnTone[statusTone(d.badge as string | undefined)]
    return (
      <CardMenu data={d}>
        <motion.div
          initial={reduce ? false : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.2, delay: Math.min((d.index ?? 0) * 0.018, 0.4), ease: [0.22, 0.7, 0.3, 1] }}
          style={{ width: (d.w as number) ?? 32, height: (d.h as number) ?? 32 }}
          className={`relative ${clickable ? 'cursor-pointer' : ''}`}
          title={d.title}
          onContextMenu={(e) => e.stopPropagation()}
        >
          <Handle type="target" position={Position.Top} id="t" />
          <Handle type="source" position={Position.Bottom} id="b" />
          <Handle type="target" position={Position.Left} id="l" />
          <Handle type="source" position={Position.Right} id="r" />
          <Handle type="target" position={Position.Right} id="ri" style={{ top: '50%' }} />
          <Handle type="source" position={Position.Left} id="lo" style={{ top: '50%' }} />
          <div
            className={`flex size-full items-center justify-center rounded-full border border-dashed transition-colors ${tone.hover} ${tone.idle}`}
          >
            <span className={`text-[10px] font-semibold leading-none tabular-nums ${tone.text}`}>{String(d.badge || '↩')}</span>
          </div>
        </motion.div>
      </CardMenu>
    )
  }

  return (
    <CardMenu data={d}>
      <motion.div
        initial={reduce ? false : { opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.24, delay: Math.min((d.index ?? 0) * 0.018, 0.5), ease: [0.22, 0.7, 0.3, 1] }}
        style={{ width: (d.w as number) ?? 380, height: (d.h as number) ?? 84 }}
        title={d.full}
        onContextMenu={(e) => e.stopPropagation()}
        className={[
        'relative flex flex-col justify-center overflow-hidden rounded-[6px] border px-2.5 py-2 text-ink',
        isBanner
          ? 'border-dashed border-acc/35 bg-transparent text-acc/90'
          : isExit
            ? 'border-dashed border-warn/45 bg-warn/[0.04]'
            : isRoot
              ? 'border-line2 bg-panel2'
              : 'border-line bg-panel',
        clickable ? 'cursor-pointer transition-colors hover:border-acc/60 hover:bg-panel2' : '',
      ].join(' ')}
    >
      <Handle type="target" position={Position.Top} id="t" />
      <Handle type="source" position={Position.Bottom} id="b" />
      <Handle type="target" position={Position.Bottom} id="bi" style={{ left: '75%' }} />
      <Handle type="target" position={Position.Left} id="l" />
      <Handle type="source" position={Position.Right} id="r" />
      <Handle type="target" position={Position.Right} id="ri" style={{ top: "50%" }} />
      <Handle type="source" position={Position.Left} id="lo" style={{ top: "50%" }} />

      <span className="absolute -left-[19px] top-[13px] size-[7px] rounded-full border border-line2 bg-canvas" />

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
        {clickable && (
          <span
            data-open
            role="link"
            aria-label={`open ${d.title}`}
            className="shrink-0 cursor-pointer rounded-[3px] border border-transparent px-1 py-0.5 text-[10px] text-faint transition-colors hover:border-line hover:bg-panel2 hover:text-acc"
            title="open this function"
          >
            open ›
          </span>
        )}
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

      {isExit && (
        <div className="mt-1 flex items-center gap-1.5 self-start">
          <span className="text-[10px] text-mut">returns here</span>
          {d.rk && <Tag t={d.rk} />}
          {d.badge && (!d.rk || d.badge !== 'exits') && (
            <span
              className={`rounded-[3px] border px-1 text-[10px] leading-[14px] ${statusBadgeTone[statusTone(d.badge as string | undefined)]}`}
            >
              {d.badge}
            </span>
          )}
        </div>
      )}
      </motion.div>
    </CardMenu>
  )
}

export const SceneNode = memo(SceneNodeInner)
