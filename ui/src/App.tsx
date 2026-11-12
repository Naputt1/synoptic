import {
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { useEffect, useState } from 'react'

import { Shell } from '@/components/Shell'
import { DataCtx } from '@/lib/context'
import { loadData } from '@/lib/data'
import type { Data } from '@/lib/types'
import { FlowView } from '@/views/Flow'
import { FnView } from '@/views/Fn'
import { PipelineView } from '@/views/Pipeline'
import { RouterView } from '@/views/Router'
import { RPCView } from '@/views/RPC'
import { RPCMethodView } from '@/views/RPCMethod'
import { TypeView } from '@/views/Type'

function Root() {
  const [data, setData] = useState<Data | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    loadData()
      .then(setData)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  if (error) {
    return (
      <div className="text-bad flex h-full items-center justify-center p-8 text-[12px]">
        {error} — run the generator with{' '}
        <code className="text-ink mx-1">-json out/synoptic.json</code>
      </div>
    )
  }
  if (!data) {
    return (
      <div className="text-faint flex h-full items-center justify-center text-[12px]">
        reading the module…
      </div>
    )
  }

  return (
    <DataCtx.Provider value={data}>
      <Shell />
    </DataCtx.Provider>
  )
}

const rootRoute = createRootRoute({ component: Root })
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: RouterView,
})
const groupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/g/$',
  component: RouterView,
})
const routeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/r/$ri',
  component: PipelineView,
})
const stageRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/r/$ri/s/$si',
  component: FlowView,
})
const fnRoute = createRoute({ getParentRoute: () => rootRoute, path: '/f', component: FnView })
const typeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/t', component: TypeView })
const rpcRoute = createRoute({ getParentRoute: () => rootRoute, path: '/rpc', component: RPCView })
const rpcMethodRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/rpc/$si/$mi',
  component: RPCMethodView,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  groupRoute,
  routeRoute,
  stageRoute,
  fnRoute,
  typeRoute,
  rpcRoute,
  rpcMethodRoute,
])

const router = createRouter({ routeTree, history: createHashHistory() })

export function App() {
  return <RouterProvider router={router} />
}
