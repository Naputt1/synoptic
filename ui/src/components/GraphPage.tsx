import { SceneCanvas } from '@/graph/SceneCanvas'
import type { Scene } from '@/graph/scenes'

export function GraphPage({
  scene,
  card,
  children,
}: {
  scene: Scene
  card: React.ReactNode
  children?: React.ReactNode
}) {
  return (
    <div className="flex h-full flex-col">
      {card}
      <div className="relative min-h-0 flex-1">
        <SceneCanvas scene={scene} />
      </div>
      {children && (
        <div className="border-line bg-panel/30 max-h-[44vh] shrink-0 overflow-auto border-t">
          {children}
        </div>
      )}
    </div>
  )
}
