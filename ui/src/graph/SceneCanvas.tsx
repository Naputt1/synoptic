import {
  Background,
  BackgroundVariant,
  Controls,
  type NodeMouseHandler,
  ReactFlow,
  ReactFlowProvider,
  useNodesInitialized,
  useNodesState,
  useReactFlow,
} from '@xyflow/react'
import {
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  type Simulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from 'd3-force'
import { ArrowLeft, Link2, LocateFixed, MousePointerClick, Search } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuGroup,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuTrigger,
} from '@/components/ui/context-menu'

import { copyValue, openSearch } from '../lib/actions'
import { goto } from '../lib/nav'
import { BranchEdge } from './BranchEdge'
import { DepEdge } from './DepEdge'
import { ExitEdge } from './ExitEdge'
import { FlowEdge } from './FlowEdge'
import { JoinEdge } from './JoinEdge'
import { LoopEdge } from './LoopEdge'
import { ReturnEdge } from './ReturnEdge'
import { SceneNode } from './SceneNode'
import type { Scene, SData, SNode } from './scenes'

const nodeTypes = { scene: SceneNode }
const edgeTypes = {
  flow: FlowEdge,
  dep: DepEdge,
  exitRail: ExitEdge,
  return: ReturnEdge,
  branch: BranchEdge,
  join: JoinEdge,
  loop: LoopEdge,
}

type Box = { x: number; y: number; w: number; h: number }

type LiveNode = SimulationNodeDatum & {
  id: string
  r: number
  service: boolean
  linked: boolean
}
type LiveLink = SimulationLinkDatum<LiveNode> & { thirdParty: boolean }
type Live = {
  sim: Simulation<LiveNode, LiveLink>
  byId: Map<string, LiveNode>
  dragId: string | null
}

const endpointOf = (v: string | number | LiveNode) =>
  typeof v === 'string' ? v : typeof v === 'number' ? String(v) : v.id

/** Bounding box of a scene, from the layout we produced (no DOM measuring). */
function sceneBox(nodes: SNode[]): Box {
  let x0 = Infinity
  let y0 = Infinity
  let x1 = -Infinity
  let y1 = -Infinity
  for (const n of nodes) {
    const d = n.data as SData
    const w = (d.w as number) ?? 380
    const h = (d.h as number) ?? 84
    x0 = Math.min(x0, n.position.x)
    y0 = Math.min(y0, n.position.y)
    x1 = Math.max(x1, n.position.x + w)
    y1 = Math.max(y1, n.position.y + h)
  }
  if (!Number.isFinite(x0)) return { x: 0, y: 0, w: 1, h: 1 }
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 }
}

const cardSize = (data: SData) => ({
  w: (data.w as number) ?? 200,
  h: (data.h as number) ?? 84,
})

function Inner({
  scene,
  focusId,
  locate,
  onExpand,
}: {
  scene: Scene
  focusId?: string
  locate?: number
  onExpand?: (id: string) => void
}) {
  const isDeps = scene.key.startsWith('deps:')
  const [nodes, setNodes, onNodesChange] = useNodesState<SNode>(scene.nodes)
  const { setViewport, fitView, getViewport } = useReactFlow()
  const nodesInitialized = useNodesInitialized()
  const wrap = useRef<HTMLDivElement>(null)
  const sceneRef = useRef(scene)
  sceneRef.current = scene
  const focusRef = useRef(focusId)
  focusRef.current = focusId
  const fitted = useRef({ key: '', w: 0, h: 0 })
  const [hot, setHot] = useState<string | null>(null)
  const live = useRef<Live | null>(null)
  const nodesRef = useRef(nodes)
  nodesRef.current = nodes

  // Reveal a package next to the detail sheet. If it is already visible, leave
  // the viewport alone so opening the sheet does not yank the graph left.
  const focusOn = useCallback(
    (n: SNode, duration = 220) => {
      const el = wrap.current
      if (!el) return
      const { x, y, zoom } = getViewport()
      const sheet = focusRef.current ? Math.min(760, el.clientWidth * 0.92) : 0
      const visible = el.clientWidth - sheet
      if (visible < 260) return
      const margin = 72
      const { w, h } = cardSize(n.data)
      const cx = (n.position.x + w / 2) * zoom + x
      const cy = (n.position.y + h / 2) * zoom + y
      const maxX = visible - margin
      const maxY = el.clientHeight - margin
      const dx = cx > maxX ? maxX - cx : cx < margin ? margin - cx : 0
      const dy = cy > maxY ? maxY - cy : cy < margin ? margin - cy : 0
      if (Math.abs(dx) < 1 && Math.abs(dy) < 1) return
      setViewport({ x: x + dx, y: y + dy, zoom }, { duration })
    },
    [setViewport, getViewport],
  )

  useEffect(() => {
    setNodes(scene.nodes)
    setHot(null)
  }, [scene, setNodes])

  // Keep a d3 simulation behind the deps graph. It sits idle until a dot is
  // dragged, then reheats so the rest of the graph reacts around the pointer.
  useEffect(() => {
    if (!isDeps || !scene.nodes.length) return
    const linked = new Set<string>()
    for (const e of scene.edges) {
      linked.add(String(e.source))
      linked.add(String(e.target))
    }
    const simNodes: LiveNode[] = scene.nodes.map((n) => {
      const { w, h } = cardSize(n.data)
      const service = n.data.kind === 'service'
      const title = String(n.data.title ?? '')
      return {
        id: n.id,
        x: n.position.x + w / 2,
        y: n.position.y + h / 2,
        r: 18 + Math.min(72, title.length * 2.6) + (service ? 8 : 0),
        service,
        linked: linked.has(n.id),
      }
    })
    const byId = new Map(simNodes.map((n) => [n.id, n]))
    const centerX = simNodes.reduce((sum, n) => sum + (n.x ?? 0), 0) / simNodes.length
    const centerY = simNodes.reduce((sum, n) => sum + (n.y ?? 0), 0) / simNodes.length
    const links: LiveLink[] = scene.edges.map((e) => ({
      source: String(e.source),
      target: String(e.target),
      thirdParty: String(e.source).startsWith('x:') || String(e.target).startsWith('x:'),
    }))
    const sim = forceSimulation<LiveNode>(simNodes)
      .force(
        'link',
        forceLink<LiveNode, LiveLink>(links)
          .id((n) => n.id)
          .distance((l) => {
            if (l.thirdParty) return 62
            const s = byId.get(endpointOf(l.source))
            const t = byId.get(endpointOf(l.target))
            return s?.service || t?.service ? 108 : 82
          })
          .strength((l) => (l.thirdParty ? 0.35 : 0.84)),
      )
      .force(
        'charge',
        forceManyBody<LiveNode>()
          .strength((n) => (n.linked ? -360 : -90))
          .distanceMax(1400),
      )
      .force('collide', forceCollide<LiveNode>((n) => n.r).iterations(3))
      .force(
        'x',
        forceX<LiveNode>(centerX).strength((n) => (n.linked ? 0.06 : 0.14)),
      )
      .force(
        'y',
        forceY<LiveNode>(centerY).strength((n) => (n.linked ? 0.06 : 0.14)),
      )
      .alpha(0)
      .alphaDecay(0.06)
      .stop()
    live.current = { sim, byId, dragId: null }
    sim.on('tick', () => {
      const current = live.current
      if (!current || current.sim !== sim) return
      const dragId = current.dragId
      setNodes((prev) => {
        let changed = false
        const next = prev.map((n) => {
          if (n.id === dragId) return n
          const s = byId.get(n.id)
          if (!s) return n
          const { w, h } = cardSize(n.data)
          const x = (s.x ?? 0) - w / 2
          const y = (s.y ?? 0) - h / 2
          if (Math.abs(n.position.x - x) < 0.1 && Math.abs(n.position.y - y) < 0.1) return n
          changed = true
          return { ...n, position: { x, y } }
        })
        return changed ? next : prev
      })
    })
    return () => {
      sim.stop()
      if (live.current?.sim === sim) live.current = null
    }
  }, [isDeps, scene, setNodes])

  // Fit once per scene (and again if the canvas is resized), never on a step change.
  useEffect(() => {
    if (!nodesInitialized) return
    const el = wrap.current
    if (!el) return
    let retry = 0

    const fit = () => {
      const w = el.clientWidth
      const h = el.clientHeight
      if (w < 80 || h < 80) return
      const box = sceneBox(sceneRef.current.nodes)
      const key = `${sceneRef.current.key}:${sceneRef.current.nodes.length}:${Math.round(box.x)}:${Math.round(box.y)}:${Math.round(box.w)}:${Math.round(box.h)}`
      const last = fitted.current
      // The detail sheet locks body scroll and can change the canvas width by a
      // scrollbar. Do not re-fit for that tiny shift: it fights the focus pan.
      if (last.key === key && Math.abs(last.w - w) < 24 && Math.abs(last.h - h) < 24) return
      fitted.current = { key, w, h }
      if (sceneRef.current.key.startsWith('deps:')) {
        // Dots are small enough to fit the whole graph.
        fitView({ padding: 0.15, duration: 320, minZoom: 0.7, maxZoom: 1.25 })
        window.clearTimeout(retry)
        retry = window.setTimeout(
          () => fitView({ padding: 0.15, duration: 0, minZoom: 0.7, maxZoom: 1.25 }),
          80,
        )
        return
      }
      fitView({ padding: 0.14, duration: 320, minZoom: 0.22, maxZoom: 1.15 })
      // nodes can still be unmeasured on the first frame; settle after they are
      window.clearTimeout(retry)
      retry = window.setTimeout(
        () => fitView({ padding: 0.14, duration: 0, minZoom: 0.22, maxZoom: 1.15 }),
        80,
      )
    }

    fit()
    const ro = new ResizeObserver(fit)
    ro.observe(el)
    return () => {
      ro.disconnect()
      window.clearTimeout(retry)
    }
  }, [scene.key, nodesInitialized, setViewport, fitView])

  // Only the sheet's locate button moves the viewport; clicking a package never
  // does. That keeps the graph still while the sheet opens and closes.
  useEffect(() => {
    if (!isDeps || !locate || !focusId) return
    const n = nodesRef.current.find((item) => item.id === focusId)
    if (n) focusOn(n, 220)
  }, [locate, focusId, isDeps, focusOn])

  const onNodeClick = useCallback<NodeMouseHandler<SNode>>(
    (e, n) => {
      if (isDeps && e.shiftKey && onExpand) {
        onExpand(n.id)
        return
      }
      const input = n.data.input as string | undefined
      if (n.data.kind === 'input' && input) {
        setHot((current) => (current === input ? null : input))
        const inputLine = n.data.line as number | undefined
        if (inputLine) {
          window.dispatchEvent(
            new CustomEvent('synoptic:source', {
              detail: {
                line: inputLine,
                lines: n.data.lines as number[] | undefined,
                startLine: n.data.startLine as number | undefined,
              },
            }),
          )
        }
        return
      }
      const href = n.data.href as string | undefined
      // jump cards (c.Next and chain handoffs) move on wherever they are clicked
      if (href && n.data.jump) {
        goto(href)
        return
      }
      // only the "open ›" affordance walks into the callee; the card itself
      // shows the line where the call is made
      if (href && (e.target as Element).closest?.('[data-open]')) {
        goto(href)
        return
      }
      const line = n.data.line as number | undefined
      if (line) {
        window.dispatchEvent(
          new CustomEvent('synoptic:source', {
            detail: {
              line,
              lines: n.data.lines as number[] | undefined,
              startLine: n.data.startLine as number | undefined,
            },
          }),
        )
        return
      }
      if (href) goto(href)
    },
    [isDeps, onExpand],
  )

  const onNodeEnter = useCallback<NodeMouseHandler<SNode>>(
    (_e, n) => {
      if (isDeps) setHot(n.id)
    },
    [isDeps],
  )
  const onNodeLeave = useCallback(() => {
    if (isDeps) setHot(null)
  }, [isDeps])

  const onPaneClick = useCallback(() => setHot(null), [])

  const pin = useCallback((n: SNode, active: boolean) => {
    const current = live.current
    if (!current || !current.byId.has(n.id)) return
    const node = current.byId.get(n.id)!
    const { w, h } = cardSize(n.data)
    node.fx = n.position.x + w / 2
    node.fy = n.position.y + h / 2
    // Reheat once when the drag begins, but do not hold the graph at a high
    // alpha while the pointer is still: that keeps every other dot drifting.
    if (active) current.sim.alpha(0.15).alphaTarget(0).restart()
  }, [])

  const endDrag = useCallback(() => {
    const current = live.current
    if (!current?.dragId) return
    const node = current.byId.get(current.dragId)
    if (node) {
      // Let go of the node after the drop. Pinning it forever lets one drag
      // strand a package far away, with its links stretched across the view.
      node.fx = null
      node.fy = null
    }
    current.dragId = null
    current.sim.alpha(0.2).alphaTarget(0).restart()
  }, [])

  const onNodeDragStart = useCallback(
    (_e: unknown, n: SNode) => {
      const current = live.current
      if (!current) return
      current.dragId = n.id
      setHot(null)
      pin(n, true)
    },
    [pin],
  )

  const onNodeDrag = useCallback((_e: unknown, n: SNode) => {
    const current = live.current
    if (!current || current.dragId !== n.id) return
    const node = current.byId.get(n.id)
    if (!node) return
    const { w, h } = cardSize(n.data)
    const fx = n.position.x + w / 2
    const fy = n.position.y + h / 2
    const moved = Math.abs((node.fx ?? 0) - fx) + Math.abs((node.fy ?? 0) - fy) > 1
    node.fx = fx
    node.fy = fy
    // React to pointer movement, then cool again when the pointer stops. A
    // stationary hold must not keep the whole graph simmering.
    if (moved) current.sim.alpha(Math.max(current.sim.alpha(), 0.1)).restart()
  }, [])

  const onNodeDragStop = useCallback(() => {
    endDrag()
  }, [endDrag])

  // Once the node is released, the dragged node is no longer pinned to the
  // cursor, so its links and the centring forces pull the graph back together.
  // This also catches drops that happen outside the canvas or the window.
  useEffect(() => {
    if (!isDeps) return
    const end = () => endDrag()
    window.addEventListener('pointerup', end)
    window.addEventListener('pointercancel', end)
    return () => {
      window.removeEventListener('pointerup', end)
      window.removeEventListener('pointercancel', end)
    }
  }, [isDeps, endDrag])

  const neighbors = useMemo(() => {
    const set = new Set<string>()
    if (!isDeps || !hot) return set
    for (const e of scene.edges) {
      if (e.source === hot) set.add(e.target)
      if (e.target === hot) set.add(e.source)
    }
    return set
  }, [isDeps, hot, scene.edges])

  const displayNodes = useMemo(() => {
    if (!isDeps) return nodes
    let changed = false
    const next = nodes.map((n) => {
      const dim = hot ? n.id !== hot && !neighbors.has(n.id) : false
      const selected = n.id === focusId
      if (n.data.dim === dim && n.data.selected === selected) return n
      changed = true
      return { ...n, data: { ...n.data, dim, selected } }
    })
    return changed ? next : nodes
  }, [nodes, isDeps, hot, neighbors, focusId])

  // Edge dimming follows the pointer only. Keeping the sheet selection out of
  // it means opening and closing the sheet does not re-style every import.
  const active = hot
  const displayEdges = useMemo(() => {
    if (!isDeps || !active) return scene.edges
    return scene.edges.map((e) => {
      const on = e.source === active || e.target === active
      const tone = e.source === active ? '#58e0b7' : '#9fc6ff'
      const marker =
        typeof e.markerEnd === 'object' && e.markerEnd
          ? { ...e.markerEnd, color: on ? tone : '#2b535b' }
          : e.markerEnd
      return {
        ...e,
        style: {
          ...e.style,
          opacity: on ? 1 : 0.04,
          stroke: on ? tone : '#2b535b',
          strokeWidth: on ? 2.2 : 1,
        },
        markerEnd: marker,
        zIndex: on ? 1000 : 0,
      }
    })
  }, [scene.edges, active, isDeps])

  return (
    <ContextMenu>
      <ContextMenuTrigger
        render={
          <div ref={wrap} className="absolute inset-0" data-hot={hot ?? undefined}>
            {hot && !isDeps && (
              <style>{`
              [data-hot="${hot}"] .react-flow__edge:not(.edge-input-${hot}) { opacity: 0.18; }
              [data-hot="${hot}"] .edge-input-${hot} { opacity: 1 !important; }
              [data-hot="${hot}"] .edge-input-${hot} path { stroke: #58e0b7 !important; stroke-width: 2.2 !important; opacity: 1 !important; }
            `}</style>
            )}
            <ReactFlow
              nodes={displayNodes}
              edges={displayEdges}
              nodeTypes={nodeTypes}
              edgeTypes={edgeTypes}
              onNodesChange={onNodesChange}
              onNodeClick={onNodeClick}
              onNodeMouseEnter={onNodeEnter}
              onNodeMouseLeave={onNodeLeave}
              autoPanOnNodeDrag={false}
              onNodeDragStart={onNodeDragStart}
              onNodeDrag={onNodeDrag}
              onNodeDragStop={onNodeDragStop}
              onPaneClick={onPaneClick}
              nodesDraggable={isDeps}
              nodesConnectable={false}
              elementsSelectable={false}
              panOnScroll
              zoomOnDoubleClick={false}
              minZoom={0.15}
              maxZoom={1.8}
              proOptions={{ hideAttribution: true }}
              className="grid-bg"
            >
              <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="#24444b" />
              <Controls showInteractive={false} position="bottom-right" />
            </ReactFlow>
          </div>
        }
      />
      <ContextMenuContent className="w-[214px]">
        <ContextMenuGroup>
          <ContextMenuLabel>canvas</ContextMenuLabel>
        </ContextMenuGroup>
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => fitView({ padding: 0.14, duration: 320, minZoom: 0.22, maxZoom: 1.15 })}
        >
          <LocateFixed /> Fit to view
        </ContextMenuItem>
        <ContextMenuItem onClick={openSearch}>
          <Search /> Search routes
          <ContextMenuShortcut>⌘K</ContextMenuShortcut>
        </ContextMenuItem>
        {hot && (
          <ContextMenuItem onClick={() => setHot(null)}>
            <MousePointerClick /> Clear highlight
          </ContextMenuItem>
        )}
        <ContextMenuSeparator />
        <ContextMenuItem onClick={() => void copyValue(window.location.href, 'page link')}>
          <Link2 /> Copy page link
        </ContextMenuItem>
        <ContextMenuItem onClick={() => window.history.back()}>
          <ArrowLeft /> Back
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}

export function SceneCanvas({
  scene,
  focusId,
  locate,
  onExpand,
}: {
  scene: Scene
  focusId?: string
  locate?: number
  onExpand?: (id: string) => void
}) {
  return (
    <ReactFlowProvider>
      <Inner scene={scene} focusId={focusId} locate={locate} onExpand={onExpand} />
    </ReactFlowProvider>
  )
}
