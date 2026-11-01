import { ExitEdge } from './ExitEdge'
import {
  Background,
  BackgroundVariant,
  Controls,
  ReactFlow,
  ReactFlowProvider,
  useNodesState,
  useReactFlow,
  type NodeMouseHandler,
} from '@xyflow/react'
import { useCallback, useEffect, useRef } from 'react'
import type { SData } from './scenes'
import { goto } from '../lib/nav'
import { FlowEdge } from './FlowEdge'
import { SceneNode } from './SceneNode'
import type { Scene, SNode } from './scenes'

const nodeTypes = { scene: SceneNode }
const edgeTypes = { flow: FlowEdge, exitRail: ExitEdge }

type Box = { x: number; y: number; w: number; h: number }

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

function Inner({ scene }: { scene: Scene }) {
  const [nodes, setNodes, onNodesChange] = useNodesState<SNode>(scene.nodes)
  const { setViewport, fitView } = useReactFlow()
  const wrap = useRef<HTMLDivElement>(null)
  const sceneRef = useRef(scene)
  sceneRef.current = scene
  const fitted = useRef({ key: '', w: 0, h: 0 })

  useEffect(() => {
    setNodes(scene.nodes)
  }, [scene, setNodes])

  // Fit once per scene (and again if the canvas is resized), never on a step change.
  useEffect(() => {
    const el = wrap.current
    if (!el) return

    const fit = () => {
      const w = el.clientWidth
      const h = el.clientHeight
      if (w < 80 || h < 80) return
      const box = sceneBox(sceneRef.current.nodes)
      const key = `${sceneRef.current.key}:${sceneRef.current.nodes.length}:${Math.round(box.x)}:${Math.round(box.y)}:${Math.round(box.w)}:${Math.round(box.h)}`
      const last = fitted.current
      if (last.key === key && last.w === w && last.h === h) return
      fitted.current = { key, w, h }
      fitView({ padding: 0.14, duration: 320, minZoom: 0.35, maxZoom: 1.15 })
      return

      const fitZoom = Math.min((w - 96) / box.w, (h - 120) / box.h)
      const floor = scene.key.startsWith('router:') ? 0.86 : 0.62
      const zoom = Math.max(floor, Math.min(1.35, fitZoom))
      const centredX = fitZoom < 0.62 ? 56 : (w - box.w * zoom) / 2
      const centredY = fitZoom < 0.62 ? 56 : box.h * zoom < h - 160 ? Math.max(48, (h - box.h * zoom) / 2) : 48
      setViewport(
        {
          x: centredX - box.x * zoom,
          y: centredY - box.y * zoom,
          zoom,
        },
        { duration: 320 },
      )
    }

    fit()
    const ro = new ResizeObserver(fit)
    ro.observe(el)
    return () => ro.disconnect()
  }, [scene.key, setViewport])

  const onNodeClick = useCallback<NodeMouseHandler<SNode>>((_e, n) => {
    const href = n.data.href as string | undefined
    if (href) {
      goto(href)
      return
    }
    const line = n.data.line as number | undefined
    if (line) window.dispatchEvent(new CustomEvent('synoptic:source', { detail: { line, startLine: n.data.startLine as number | undefined } }))
  }, [])

  return (
    <div ref={wrap} className="absolute inset-0">
      <ReactFlow
        nodes={nodes}
        edges={scene.edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onNodeClick={onNodeClick}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        panOnScroll
        zoomOnDoubleClick={false}
        minZoom={0.2}
        maxZoom={1.6}
        proOptions={{ hideAttribution: true }}
        className="grid-bg"
      >
        <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="#24444b" />
        <Controls showInteractive={false} position="bottom-right" />
      </ReactFlow>
    </div>
  )
}

export function SceneCanvas({ scene }: { scene: Scene }) {
  return (
    <ReactFlowProvider>
      <Inner scene={scene} />
    </ReactFlowProvider>
  )
}
