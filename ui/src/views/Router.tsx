import { useParams } from '@tanstack/react-router'
import { useMemo, useState } from 'react'

import { GraphPage } from '@/components/GraphPage'
import { ScopeCard } from '@/components/ScopeCard'
import { Badge } from '@/components/ui/badge'
import { AppLink } from '@/components/ui/link'
import { Segmented } from '@/components/ui/segmented'
import { buildRouterScene } from '@/graph/scenes'
import { useData, useSvc } from '@/lib/context'

export function RouterView() {
  const data = useData()
  const svc = useSvc()
  const params = useParams({ strict: false }) as { _splat?: string }
  const focus = params._splat ?? ''
  const [proto, setProto] = useState<'all' | 'http' | 'ws' | 'sse'>('all')
  const scene = useMemo(() => buildRouterScene(data, focus, proto, svc), [data, focus, proto, svc])
  const label = focus ? '/' + focus : data.repo
  const shown = data.routes.filter(
    (r) =>
      (proto === 'all' ? true : proto === 'http' ? !r.pr : r.pr === proto) &&
      (!svc || r.svc === svc) &&
      (focus ? r.p === '/' + focus || r.p.startsWith('/' + focus + '/') : true),
  )
  const count = shown.length
  const changed = shown.filter((r) => r.chg).length
  const changeMeta =
    data.change && changed > 0 ? ` · ${changed} changed since ${data.change.ref}` : ''
  const change = data.change
  const removed = change?.removed ?? []
  const modified = change?.modified ?? []
  const rpcServices = (data.services ?? []).filter((s) => !svc || s.svc === svc)
  const rpcCount = rpcServices.reduce((n, s) => n + s.methods.length, 0)

  return (
    <GraphPage
      scene={scene}
      card={
        <>
          <ScopeCard
            title={label}
            badge="router"
            meta={`${count} under this path${changeMeta} · click a box to open it`}
            note="each column is one path segment. open a group to walk deeper, a route to see its middleware."
          />
          {change && (
            <div className="border-line bg-panel/20 border-b px-4 py-1.5 text-[10.5px]">
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">base</span>
                <span className="text-mut">vs</span>
                <code className="text-ink">{change.ref}</code>
                <span className="text-mut">:</span>
                <span className="text-acc font-semibold">{change.added?.length ?? 0} added</span>
                <span className="text-faint">·</span>
                <span className="text-bad font-semibold">{removed.length} removed</span>
                <span className="text-faint">·</span>
                <span className="text-warn font-semibold">{modified.length} changed</span>
              </div>
              {(removed.length > 0 || modified.length > 0) && (
                <details className="mt-1">
                  <summary className="text-faint hover:text-mut cursor-pointer text-[10px]">
                    what changed
                  </summary>
                  <ul className="divide-line/60 border-line/60 mt-1 divide-y rounded-[4px] border">
                    {removed.map((r) => (
                      <li
                        key={`rm:${r.m}:${r.p}:${r.svc}`}
                        className="flex items-baseline gap-2 px-2 py-1"
                      >
                        <code className="text-bad w-12 shrink-0 text-[10px] font-semibold">
                          {r.m}
                        </code>
                        <span className="text-mut min-w-0 flex-1 truncate">{r.p}</span>
                        <Badge variant="bad">removed</Badge>
                      </li>
                    ))}
                    {modified.map((r) => (
                      <li
                        key={`mod:${r.m}:${r.p}:${r.svc}`}
                        className="flex items-baseline gap-2 px-2 py-1"
                      >
                        <code className="text-warn w-12 shrink-0 text-[10px] font-semibold">
                          {r.m}
                        </code>
                        <span className="text-ink min-w-0 flex-1 truncate">{r.p}</span>
                        <span className="text-faint shrink-0 text-[10px]">{r.why.join(', ')}</span>
                      </li>
                    ))}
                  </ul>
                </details>
              )}
            </div>
          )}
          <div className="border-line bg-panel/20 flex items-center gap-2 border-b px-4 py-1.5">
            <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">protocol</span>
            <Segmented
              value={proto}
              onChange={(v) => setProto(v as typeof proto)}
              options={[
                { value: 'all', label: 'all' },
                { value: 'http', label: 'http' },
                { value: 'ws', label: 'ws' },
                { value: 'sse', label: 'sse' },
              ]}
            />
            <span className="text-faint ml-auto text-[10px]">
              {count} {count === 1 ? 'route' : 'routes'}
            </span>
          </div>
          {rpcCount > 0 && (
            <AppLink
              href="/rpc"
              className="border-line hover:bg-panel flex flex-wrap items-center gap-x-2 border-b px-4 py-1.5 text-[11px]"
            >
              <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">rpc</span>
              <span className="text-ink font-semibold">
                {rpcServices.length} {rpcServices.length === 1 ? 'service' : 'services'}
              </span>
              <span className="text-mut">{rpcCount} methods</span>
              <span className="text-acc ml-auto">open gRPC surface ›</span>
            </AppLink>
          )}
        </>
      }
    />
  )
}
