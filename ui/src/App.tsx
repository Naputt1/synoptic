import {
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useParams,
  useRouterState,
  useSearch,
} from '@tanstack/react-router'
import { Bug, Code2, Crosshair, Search } from 'lucide-react'
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'

import { GoSource } from './components/GoSource'
import { Badge } from './components/ui/badge'
import { Button, buttonVariants } from './components/ui/button'
import { SearchDialog } from './components/ui/command'
import { AppLink } from './components/ui/link'
import { Segmented } from './components/ui/segmented'
import { Sheet } from './components/ui/sheet'
import { TraceDialog } from './components/ui/trace'
import { buildFlowScene } from './graph/flowscene'
import { SceneCanvas } from './graph/SceneCanvas'
import { buildPipelineScene, buildRouterScene, externalCalls, type Scene } from './graph/scenes'
import {
  flowOf,
  fnHref,
  frameName,
  loadData,
  msOf,
  routeHref,
  stageHref,
  tagLabel,
  timingOf,
  touchTags,
} from './lib/data'
import { goto } from './lib/nav'
import type {
  Call,
  Data,
  Effect,
  FuncFlow,
  Route,
  Stage,
  Step,
  StreamEvent,
  StreamMessage,
  SynType,
} from './lib/types'
import { DocPanel, Inputs } from './panes/DocPanel'

/* ---------- data ---------- */

const DataCtx = createContext<Data | null>(null)
const useData = () => {
  const d = useContext(DataCtx)
  if (!d) throw new Error('data not loaded')
  return d
}

/** Selected service of a multi-entry module; '' means every service. */
const SvcCtx = createContext<{ svc: string; setSvc: (v: string) => void }>({
  svc: '',
  setSvc: () => {},
})
const useSvc = () => useContext(SvcCtx).svc
const useSvcSwitch = () => useContext(SvcCtx)

/* ---------- crumbs ---------- */

type Crumb = { label: string; href?: string }

// Search params are strings, and an absent value must not become stage 0.
const numParam = (v?: string): number | undefined => {
  if (v === undefined || v === '') return undefined
  const n = Number(v)
  return Number.isInteger(n) ? n : undefined
}

function useCrumbs(): Crumb[] {
  const data = useData()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const search = useSearch({ strict: false }) as {
    k?: string
    i?: string
    t?: string
    ri?: string
    si?: string
  }
  const parts = pathname.split('/').filter(Boolean)
  const crumbs: Crumb[] = [{ label: data.repo, href: '/' }]

  const routeChain = (ri: number, si?: number, skipStage?: string) => {
    const r = data.routes[ri]
    if (!r) return
    const segs = r.p.split('/').filter(Boolean).slice(0, 2)
    segs.forEach((s, i) => crumbs.push({ label: s, href: '/g/' + segs.slice(0, i + 1).join('/') }))
    crumbs.push({ label: `${r.m} ${r.p}`, href: routeHref(ri) })
    if (si !== undefined) {
      const st = r.ch[si]
      const same =
        st && (st.n === skipStage || Boolean(skipStage && skipStage.endsWith('.' + st.n)))
      if (st && !same) {
        const fk = (st.fk ?? '').split('|')
        const self = fk.length >= 3 ? data.types?.[`${fk[0]}|${fk[1]}`] : undefined
        const origin = `&ri=${ri}&si=${si}`
        if (self) {
          const impl = self.impls?.length === 1 ? data.types?.[self.impls[0]] : undefined
          if (impl)
            crumbs.push({ label: impl.n, href: `/t?k=${encodeURIComponent(impl.key)}${origin}` })
          crumbs.push({ label: self.n, href: `/t?k=${encodeURIComponent(self.key)}${origin}` })
        }
        crumbs.push({ label: st.n, href: stageHref(ri, si) })
      }
    }
  }

  if (parts[0] === 'g') {
    const segs = parts.slice(1)
    segs.forEach((s, i) => crumbs.push({ label: s, href: '/g/' + segs.slice(0, i + 1).join('/') }))
  }

  if (parts[0] === 'r') {
    const ri = Number(parts[1])
    routeChain(ri, parts[2] === 's' ? Number(parts[3]) : undefined)
  }

  if (parts[0] === 't') {
    const key = search.k ?? ''
    const iface = search.i ? data.types?.[search.i] : undefined
    const self = data.types?.[key]
    const ri = numParam(search.ri)
    const si = numParam(search.si)
    const origin =
      ri !== undefined && data.routes[ri] ? `&ri=${ri}${si !== undefined ? `&si=${si}` : ''}` : ''
    if (ri !== undefined && data.routes[ri]) {
      routeChain(ri, si)
    }
    if (iface)
      crumbs.push({ label: iface.n, href: `/t?k=${encodeURIComponent(iface.key)}${origin}` })
    if (self)
      crumbs.push({
        label: self.n,
        href: `/t?k=${encodeURIComponent(self.key)}${search.i ? `&i=${encodeURIComponent(search.i)}` : ''}${origin}`,
      })
  }

  if (parts[0] === 'rpc') {
    const svc = data.services?.[Number(parts[1])]
    crumbs.push({ label: 'rpc', href: '/rpc' })
    if (svc) {
      const method = svc.methods[Number(parts[2])]
      crumbs.push({ label: svc.n, href: '/rpc' })
      if (method) crumbs.push({ label: method.n, href: '#' })
    }
  }

  if (parts[0] === 'f') {
    const f = data.flows[search.k ?? '']
    const iface = search.i ? data.types?.[search.i] : undefined
    const st = search.t ? data.types?.[search.t] : undefined
    // keep the route and stage that opened this function, so back navigation
    // has the whole chain instead of restarting at the repo
    const ri = numParam(search.ri)
    const si = numParam(search.si)
    const origin =
      ri !== undefined && data.routes[ri] ? `&ri=${ri}${si !== undefined ? `&si=${si}` : ''}` : ''
    if (ri !== undefined && data.routes[ri]) {
      routeChain(ri, si, f?.n)
    }
    if (iface)
      crumbs.push({ label: iface.n, href: `/t?k=${encodeURIComponent(iface.key)}${origin}` })
    if (st)
      crumbs.push({
        label: st.n,
        href: `/t?k=${encodeURIComponent(st.key)}${iface ? `&i=${encodeURIComponent(iface.key)}` : ''}${origin}`,
      })
    if (f && crumbs[crumbs.length - 1]?.label !== f.n) crumbs.push({ label: f.n, href: '#' })
  }

  return crumbs
}

/* ---------- shell ---------- */

function Topbar({ onSearch, onTrace }: { onSearch: () => void; onTrace: () => void }) {
  const data = useData()
  const svc = useSvc()
  const { setSvc } = useSvcSwitch()
  const scope = useMemo(() => {
    if (!svc) {
      return {
        routes: data.stats.routes,
        streams: data.stats.streams ?? 0,
        services: data.stats.services ?? 0,
        rpcs: data.stats.rpcs ?? 0,
        documented: data.stats.documented,
      }
    }
    const routes = data.routes.filter((r) => r.svc === svc)
    const services = (data.services ?? []).filter((s) => s.svc === svc)
    return {
      routes: routes.length,
      streams: routes.filter((r) => r.pr || r.st).length,
      services: services.length,
      rpcs: services.reduce((n, s) => n + s.methods.length, 0),
      documented: routes.filter((r) => r.ch.some((st) => st.r === 'handler' && st.d)).length,
    }
  }, [svc, data])
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const parts = pathname.split('/').filter(Boolean)
  const level =
    parts[0] === 'rpc'
      ? ''
      : parts[0] === 'f'
        ? ''
        : parts[0] === 'r'
          ? parts[2] === 's'
            ? 'step'
            : 'route'
          : 'router'
  const ri = parts[0] === 'r' ? Number(parts[1]) : -1
  const go = (lv: string) => {
    if (lv === 'router') goto('/')
    else if (lv === 'route' && ri >= 0) goto(routeHref(ri))
    else if (lv === 'step' && ri >= 0) {
      const r = data.routes[ri]
      const si = r && parts[2] === 's' ? Number(parts[3]) : 0
      if (r && r.ch[si]) goto(stageHref(ri, si))
    }
  }

  return (
    <header className="border-line flex flex-wrap items-center gap-x-5 gap-y-2 border-b px-4 py-2">
      <div className="flex items-baseline gap-2">
        <span className="text-faint text-[10px] tracking-[0.18em] uppercase">route synoptic</span>
        <span className="text-ink text-[13px]">{data.repo}</span>
      </div>
      <div className="text-mut hidden gap-x-3 text-[10.5px] sm:flex">
        <span>
          <b className="text-ink font-semibold">{scope.routes}</b> routes
        </span>
        {scope.streams > 0 && (
          <span>
            <b className="text-ink font-semibold">{scope.streams}</b> stream
            {scope.streams === 1 ? '' : 's'}
          </span>
        )}
        {scope.services > 0 && (
          <span>
            <b className="text-ink font-semibold">{scope.services}</b> rpc ·{' '}
            <b className="text-ink font-semibold">{scope.rpcs}</b>{' '}
            {scope.rpcs === 1 ? 'method' : 'methods'}
          </span>
        )}
        <span>
          <b className="text-ink font-semibold">{data.stats.middleware}</b> middleware
        </span>
        <span>
          <b className="text-ink font-semibold">{scope.documented}</b>/{scope.routes} documented
        </span>
        <span>
          <b className="text-ink font-semibold">{data.stats.funcs}</b> flows
        </span>
      </div>
      <div className="ml-auto flex flex-wrap items-center gap-3">
        {(data.svcs?.length ?? 0) > 1 && (
          <div className="flex items-center gap-1.5">
            <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">service</span>
            <Segmented
              value={svc || 'all'}
              onChange={(v) => setSvc(v === 'all' ? '' : v)}
              options={[
                { value: 'all', label: 'all', title: `${data.stats.routes} routes` },
                ...(data.svcs ?? []).map((s) => ({
                  value: s.n,
                  label: s.n,
                  title: `${s.routes} routes · ${s.rpcs} rpc · ${s.e}`,
                })),
              ]}
            />
          </div>
        )}
        <div className="flex items-center gap-1.5">
          <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">level</span>
          <Segmented
            value={level}
            onChange={go}
            options={[
              { value: 'router', label: 'router' },
              { value: 'route', label: 'route' },
              { value: 'step', label: 'step' },
            ]}
          />
        </div>
        {scope.services > 0 && (
          <div className="flex items-center gap-1.5">
            <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">surface</span>
            <Segmented
              value={parts[0] === 'rpc' ? 'rpc' : 'http'}
              onChange={(v) => goto(v === 'rpc' ? '/rpc' : '/')}
              options={[
                { value: 'http', label: 'http', title: `${scope.routes} routes` },
                {
                  value: 'rpc',
                  label: `rpc ${scope.rpcs}`,
                  title: `${scope.services} services · ${scope.rpcs} methods`,
                },
              ]}
            />
          </div>
        )}
        <Button
          variant="default"
          size="sm"
          onClick={onTrace}
          title="paste logs and trace error messages back to source"
        >
          <Bug className="size-3" /> trace
          <kbd className="border-line text-faint ml-1 rounded-[3px] border px-1 text-[9.5px]">
            ctrl ⇧ E
          </kbd>
        </Button>
        <Button variant="default" size="sm" onClick={onSearch}>
          <Search className="size-3" /> search
          <kbd className="border-line text-faint ml-1 rounded-[3px] border px-1 text-[9.5px]">
            ⌘K
          </kbd>
        </Button>
      </div>
    </header>
  )
}

function Crumbs() {
  const crumbs = useCrumbs()
  const svc = useSvc()
  return (
    <div className="border-line text-mut flex flex-wrap items-center gap-x-1.5 gap-y-1 border-b px-4 py-1.5 text-[11px]">
      <Button
        variant="ghost"
        size="sm"
        className="mr-1 h-5 px-1.5"
        onClick={() => window.history.back()}
      >
        back
      </Button>
      {crumbs.map((c, i) => (
        <span key={i} className="flex items-center gap-1.5">
          {i > 0 && <span className="text-faint">›</span>}
          {c.href && i < crumbs.length - 1 ? (
            <AppLink href={c.href} className="hover:bg-panel hover:text-ink rounded-[3px] px-1">
              {c.label}
            </AppLink>
          ) : (
            <span className={i === crumbs.length - 1 ? 'text-ink' : ''}>{c.label}</span>
          )}
        </span>
      ))}
      {svc && (
        <span
          className="border-line text-faint ml-1 rounded-[3px] border px-1 py-px text-[10px]"
          title="routes and rpc services are filtered to this entry point"
        >
          service {svc}
        </span>
      )}
    </div>
  )
}

function Shell() {
  const data = useData()
  const [searchOpen, setSearchOpen] = useState(false)
  const [traceOpen, setTraceOpen] = useState(false)
  const [svc, setSvc] = useState('')
  const [toast, setToast] = useState<{ id: number; text: string; tone: 'ok' | 'bad' } | null>(null)

  useEffect(() => {
    let hide = 0
    const onToast = (e: Event) => {
      const detail = (e as CustomEvent<{ message: string; tone?: 'ok' | 'bad' }>).detail
      setToast({ id: Date.now(), text: detail.message, tone: detail.tone ?? 'ok' })
      window.clearTimeout(hide)
      hide = window.setTimeout(() => setToast(null), 1800)
    }
    const onSearch = () => setSearchOpen(true)
    const onTrace = () => setTraceOpen(true)
    window.addEventListener('synoptic:toast', onToast)
    window.addEventListener('synoptic:search', onSearch)
    window.addEventListener('synoptic:trace', onTrace)
    return () => {
      window.clearTimeout(hide)
      window.removeEventListener('synoptic:toast', onToast)
      window.removeEventListener('synoptic:search', onSearch)
      window.removeEventListener('synoptic:trace', onTrace)
    }
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing = (e.target as HTMLElement | null)?.closest('input,textarea')
      if (e.ctrlKey && e.shiftKey && !e.metaKey && !e.altKey && e.key.toLowerCase() === 'e') {
        e.preventDefault()
        setSearchOpen(false)
        setTraceOpen((v) => !v)
        return
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setTraceOpen(false)
        setSearchOpen((v) => !v)
        return
      }
      if (
        document.querySelector(
          '[data-slot="context-menu-content"], [data-slot="search-dialog"], [data-slot="trace-dialog"], [role="dialog"]',
        )
      ) {
        if (e.key === 'Escape') {
          if (searchOpen) {
            e.preventDefault()
            setSearchOpen(false)
          } else if (traceOpen) {
            e.preventDefault()
            setTraceOpen(false)
          }
        }
        return
      }
      if (typing) return
      if (e.key === '/') {
        e.preventDefault()
        setSearchOpen(true)
      } else if (e.key === 'Escape') {
        window.history.back()
      }
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [searchOpen, traceOpen])

  return (
    <SvcCtx.Provider value={{ svc, setSvc }}>
      <div className="bg-canvas flex h-full flex-col">
        <Topbar onSearch={() => setSearchOpen(true)} onTrace={() => setTraceOpen(true)} />
        <Crumbs />
        <div className="relative min-h-0 flex-1">
          <Outlet />
        </div>
        <div className="border-line text-faint shrink-0 border-t px-4 py-1 text-[10px] leading-4">
          click any box to open it · right-click for actions · esc goes back · ⌘K searches ·
          ctrl+shift+E traces a log
        </div>
        {toast && (
          <div
            key={toast.id}
            role="status"
            aria-live="polite"
            className={`toast-in pointer-events-none fixed inset-x-0 bottom-9 z-[70] mx-auto w-fit rounded-[5px] border px-3 py-1.5 text-[11px] shadow-[0_12px_32px_-12px_rgba(0,0,0,0.9)] ${
              toast.tone === 'bad'
                ? 'border-bad/45 bg-bad/10 text-bad'
                : 'border-line bg-panel2 text-ink'
            }`}
          >
            {toast.text}
          </div>
        )}
        <SearchDialog data={data} open={searchOpen} onOpenChange={setSearchOpen} svc={svc} />
        <TraceDialog data={data} open={traceOpen} onOpenChange={setTraceOpen} svc={svc} />
      </div>
    </SvcCtx.Provider>
  )
}

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

/* ---------- shared page bits ---------- */

function ScopeCard({
  title,
  badge,
  meta,
  doc,
  note,
  tags,
  onSource,
  sourceLabel,
  rpc,
}: {
  title: string
  badge?: string
  meta?: string
  doc?: string
  note?: string
  tags?: string[]
  onSource?: () => void
  sourceLabel?: string
  rpc?: { label: string; href: string }
}) {
  return (
    <div className="border-line bg-panel/40 flex flex-wrap items-start gap-x-4 gap-y-1 border-b px-4 py-2">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-ink truncate text-[14px]">{title}</span>
          {badge && <Badge variant="accent">{badge}</Badge>}
          {(tags ?? []).map((t) => (
            <Badge key={t} variant={t === 'db' ? 'warn' : t === 'cache' ? 'info' : 'default'}>
              {t === 'db' ? 'database' : t === 'cache' ? 'cache' : t === 'net' ? 'outbound' : t}
            </Badge>
          ))}
        </div>
        {meta && <div className="text-faint mt-0.5 truncate text-[10.5px]">{meta}</div>}
        {doc && <p className="text-ink/80 mt-1 line-clamp-2 text-[11.5px] leading-[1.5]">{doc}</p>}
        {note && <p className="text-faint mt-1 text-[11px] leading-[1.5] italic">{note}</p>}
      </div>
      {rpc && (
        <AppLink
          href={rpc.href}
          className={buttonVariants({ variant: 'outline', size: 'sm' })}
          title={`${rpc.label} on the RPC surface`}
        >
          rpc · {rpc.label}
        </AppLink>
      )}
      {onSource && (
        <Button variant="outline" size="sm" onClick={onSource}>
          <Code2 className="size-3" /> {sourceLabel ?? 'source'}
        </Button>
      )}
    </div>
  )
}

function GraphPage({
  scene,
  card,
  children,
  focusLine,
  trace,
}: {
  scene: Scene
  card: React.ReactNode
  children?: React.ReactNode
  focusLine?: number
  trace?: { msg: string; file: string; line: number }
}) {
  return (
    <div className="flex h-full flex-col">
      {card}
      {trace && (
        <div className="border-line bg-acc/[0.05] flex items-center gap-2 border-b px-3 py-1.5 text-[11px]">
          <Crosshair className="text-acc size-3.5 shrink-0" />
          <span className="text-acc shrink-0">traced message</span>
          <span className="text-ink min-w-0 flex-1 truncate font-mono">{trace.msg}</span>
          <span className="text-faint shrink-0 font-mono text-[10px]">
            {trace.file}:{trace.line}
          </span>
        </div>
      )}
      <div className="relative min-h-0 flex-1">
        <SceneCanvas scene={scene} focusLine={focusLine} />
      </div>
      {children && (
        <div className="border-line bg-panel/30 max-h-[44vh] shrink-0 overflow-auto border-t">
          {children}
        </div>
      )}
    </div>
  )
}

function CallsList({ calls }: { calls?: Call[] }) {
  if (!calls || calls.length === 0) return null
  return (
    <div className="mt-4">
      <div className="text-faint text-[9.5px] tracking-[0.14em] uppercase">calls in this repo</div>
      <div className="mt-1.5 flex flex-col gap-1">
        {calls.map((c, i) => (
          <div key={i} className="flex items-baseline gap-2 text-[11.5px]">
            <span className="text-ink">{c.e}</span>
            {c.f &&
              (c.k ? (
                <AppLink
                  href={`/f?k=${encodeURIComponent(c.k)}`}
                  className="text-faint hover:text-acc truncate text-[10px]"
                >
                  {c.f}
                  {c.l ? `:${c.l}` : ''}
                </AppLink>
              ) : (
                <span className="text-faint truncate text-[10px]">
                  {c.f}
                  {c.l ? `:${c.l}` : ''}
                </span>
              ))}
            {c.d && <span className="text-faint truncate text-[10px]">{c.d}</span>}
          </div>
        ))}
      </div>
    </div>
  )
}

function DetailSheet({
  open,
  onOpenChange,
  title,
  subtitle,
  doc,
  src,
  calls,
  stage,
  inputs,
  focus,
  focusLines,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  title: string
  subtitle?: string
  doc?: string
  src?: string
  calls?: Call[]
  stage?: Stage
  inputs?: Stage['in']
  focus?: number
  focusLines?: number[]
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title={title} subtitle={subtitle}>
      {doc && <p className="text-ink/85 text-[12px] leading-[1.6]">{doc}</p>}
      {stage && (
        <div className="mt-3 flex flex-wrap gap-1">
          <Badge>{stage.r ?? stage.k}</Badge>
          {stage.x && <Badge variant="info">outside this repo</Badge>}
          {touchTags(stage).map((t) => (
            <Badge key={t} variant={t === 'db' ? 'warn' : 'default'}>
              {t}
            </Badge>
          ))}
        </div>
      )}
      {inputs && inputs.length > 0 && <Inputs items={inputs} />}
      {calls && calls.length > 0 && <CallsList calls={calls} />}
      {src ? (
        <div className="mt-4">
          <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">source</div>
          <GoSource code={src} focus={focus} focusLines={focusLines} />
        </div>
      ) : (
        <p className="text-faint mt-4 text-[11px] italic">
          no body in this repo — the call is wired here, the code lives in{' '}
          {stage?.x ?? 'another module'}
        </p>
      )}
    </Sheet>
  )
}

function fieldsOf(m: StreamMessage): string {
  const names = (m.f ?? []).map((f) => f.n)
  if (!names.length) return m.e ?? ''
  return names.slice(0, 4).join(', ') + (names.length > 4 ? ` +${names.length - 4}` : '')
}

function MessageCard({
  m,
  event,
  onOpen,
}: {
  m: StreamMessage
  event?: string
  onOpen: () => void
}) {
  return (
    <button
      type="button"
      onClick={onOpen}
      className="border-line bg-panel hover:border-line2 hover:bg-panel2 block w-full rounded-[4px] border px-2 py-1.5 text-left transition-colors"
    >
      <div className="flex flex-wrap items-baseline gap-x-2">
        <code className="text-ink text-[11px] leading-tight">{m.ev || m.n || frameName(m)}</code>
        {m.t && <span className="text-faint text-[9px] break-all">{m.t}</span>}
      </div>
      <div className="text-mut mt-0.5 text-[9.5px] break-words">
        {fieldsOf(m) || (event ? m.n : 'shape unresolved')}
      </div>
    </button>
  )
}

function MessageSheet({
  message,
  event,
  open,
  onOpenChange,
}: {
  message?: StreamMessage
  event?: string
  open: boolean
  onOpenChange: (v: boolean) => void
}) {
  return (
    <Sheet
      open={open}
      onOpenChange={onOpenChange}
      title={message ? frameName(message) : 'frame'}
      subtitle={event}
    >
      {message && (
        <div>
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge variant={message.d === 'recv' ? 'info' : 'accent'}>
              {message.d === 'recv' ? 'received' : 'sent'}
            </Badge>
            {message.t && <Badge>{message.t}</Badge>}
            {message.file && (
              <span className="text-faint ml-auto text-[10px]">
                {message.file}:{message.l ?? 0}
              </span>
            )}
          </div>
          {message.n && message.n !== frameName(message) && (
            <p className="text-faint mt-2 text-[10px]">name {message.n}</p>
          )}
          <div className="mt-4">
            <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">fields</div>
            {message.f?.length ? (
              <div className="divide-line/40 border-line/40 divide-y border-y">
                {message.f.map((f) => (
                  <div key={f.n} className="flex flex-wrap items-baseline gap-x-2 py-1.5">
                    <code className="text-ink text-[11px]">{f.n}</code>
                    {f.t && <span className="text-mut text-[10px]">{f.t}</span>}
                    {f.tg && <span className="text-faint text-[9.5px]">{f.tg}</span>}
                    {f.rq && (
                      <span className="text-warn text-[9px] tracking-[0.1em] uppercase">
                        required
                      </span>
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-faint text-[11px] italic">
                no fields resolved — only the source expression is known.
              </p>
            )}
          </div>
          {message.e && (
            <div className="mt-4">
              <div className="text-faint mb-1 text-[9.5px] tracking-[0.14em] uppercase">
                expression
              </div>
              <code className="text-mut block text-[10.5px] break-words">{message.e}</code>
            </div>
          )}
          {message.ex && (
            <pre className="bg-canvas/70 text-mut mt-3 overflow-x-auto rounded-[4px] p-2 text-[10px] leading-relaxed">
              {message.ex}
            </pre>
          )}
        </div>
      )}
    </Sheet>
  )
}

type ExtCall = { g: string; n: string; fk?: string; f?: string; l?: number; ev?: string }

function ExternalCalls({ items, onFn }: { items: ExtCall[]; onFn?: (key: string) => string }) {
  const groups = ['db', 'cache', 'net']
    .map((g) => ({ g, items: items.filter((x) => x.g === g) }))
    .filter((x) => x.items.length > 0)
  return (
    <div className="space-y-2.5">
      {groups.map((group) => (
        <div key={group.g}>
          <div className="mb-1 flex items-center gap-2">
            <Badge variant={group.g === 'db' ? 'warn' : group.g === 'cache' ? 'accent' : 'info'}>
              {tagLabel[group.g] ?? group.g}
            </Badge>
            <span className="text-faint text-[10px]">{group.items.length}</span>
          </div>
          <div className="divide-line/40 border-line/40 divide-y border-y">
            {group.items.map((x, i) => (
              <div key={`${x.g}:${x.n}:${x.f ?? ''}:${x.l ?? 0}:${i}`} className="py-1.5">
                {x.fk && onFn ? (
                  <AppLink
                    href={onFn(x.fk)}
                    className="text-acc block text-left font-mono text-[10.5px] leading-relaxed break-words hover:underline"
                  >
                    {x.n}
                  </AppLink>
                ) : (
                  <code className="text-ink block font-mono text-[10.5px] leading-relaxed break-words">
                    {x.n}
                  </code>
                )}
                {x.f && (
                  <div className="text-faint mt-0.5 font-mono text-[9px] break-all">
                    {x.f}
                    {x.l ? `:${x.l}` : ''}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

function TraceRows({
  steps,
  depth = 0,
  onFn,
  limit = 3,
}: {
  steps: Step[]
  depth?: number
  onFn?: (key: string) => string
  limit?: number
}) {
  const list = steps.slice(0, depth === 0 ? limit : 2)
  return (
    <div className={depth === 0 ? 'space-y-1' : 'border-line/60 mt-1 space-y-1 border-l pl-3'}>
      {list.map((s, i) => {
        const branch = s.b?.length || s.e?.length
        return (
          <div key={`${depth}:${i}:${s.l ?? 0}:${s.t}`}>
            <div className="flex items-start gap-2">
              <span className="text-faint mt-[1px] w-3 shrink-0 text-right font-mono text-[9px]">
                {depth === 0 ? i + 1 : ''}
              </span>
              <span className="text-faint mt-[1px] w-7 shrink-0 text-[8px] tracking-[0.08em] uppercase">
                {s.k === 'ret' ? 'return' : s.k}
              </span>
              <div className="min-w-0">
                <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                  {s.fk && onFn ? (
                    <AppLink
                      href={onFn(s.fk)}
                      className={`text-acc text-left font-mono text-[10.5px] leading-relaxed break-words hover:underline`}
                    >
                      {s.t}
                    </AppLink>
                  ) : (
                    <div
                      className={`font-mono text-[10.5px] leading-relaxed break-words ${
                        s.k === 'ret' || s.k === 'exit' ? 'text-warn' : 'text-ink'
                      }`}
                    >
                      {s.t}
                    </div>
                  )}
                  {(s.k === 'ret' || s.k === 'exit') && s.rk ? (
                    <span
                      className={`shrink-0 rounded border px-1 text-[8px] tracking-[0.08em] uppercase ${
                        s.rk === 'error'
                          ? 'border-bad/40 text-bad'
                          : s.rk === 'nil'
                            ? 'border-acc/40 text-acc'
                            : 'border-line text-mut'
                      }`}
                    >
                      {s.rk}
                    </span>
                  ) : null}
                </div>
                {(s.cd || s.s) && (
                  <div className="text-faint font-mono text-[9px] break-words">{s.cd || s.s}</div>
                )}
                {s.l ? <div className="text-faint font-mono text-[8.5px]">:{s.l}</div> : null}
              </div>
            </div>
            {branch && s.b?.length ? <TraceRows steps={s.b} depth={depth + 1} onFn={onFn} /> : null}
            {branch && s.e?.length ? (
              <>
                <div className="text-faint mt-1 pl-5 text-[9px] tracking-[0.12em] uppercase">
                  else
                </div>
                <TraceRows steps={s.e} depth={depth + 1} onFn={onFn} />
              </>
            ) : null}
          </div>
        )
      })}
      {steps.length > list.length && depth === 0 ? (
        <div className="text-faint pl-5 text-[9.5px] italic">
          +{steps.length - list.length} more steps in the full flow
        </div>
      ) : null}
    </div>
  )
}

function EffectRows({ items, onFn }: { items: Effect[]; onFn?: (key: string) => string }) {
  return (
    <div className="divide-line/40 border-line/40 divide-y border-y">
      {items.map((x, i) => (
        <div key={`${x.k}:${x.n}:${x.l ?? 0}:${i}`} className="flex items-start gap-2 py-1.5">
          <Badge variant={x.k === 'publish' ? 'accent' : x.k === 'write' ? 'warn' : 'info'}>
            {x.k}
          </Badge>
          <div className="min-w-0 flex-1">
            {x.fk && onFn ? (
              <AppLink
                href={onFn(x.fk)}
                className="text-acc text-left font-mono text-[10.5px] break-words hover:underline"
              >
                {x.n}
              </AppLink>
            ) : (
              <code className="text-ink font-mono text-[10.5px] break-words">{x.n}</code>
            )}
            {x.s && x.s !== x.n ? (
              <div className="text-faint font-mono text-[9px] break-words">{x.s}</div>
            ) : null}
          </div>
          {x.l ? <span className="text-faint font-mono text-[8.5px]">:{x.l}</span> : null}
        </div>
      ))}
    </div>
  )
}

function TriggeredBy({
  event,
  isSse,
  onFn,
}: {
  event?: StreamEvent
  isSse: boolean
  onFn?: (key: string) => string
}) {
  const origins = event?.or ?? []
  if (!origins.length) {
    return isSse ? (
      <div className="mt-4">
        <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">
          triggered by
        </div>
        <p className="text-faint text-[10.5px] italic">
          no in-repo publisher resolved for this event.
        </p>
      </div>
    ) : null
  }
  return (
    <div className="mt-4">
      <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">triggered by</div>
      <div className="divide-line/40 border-line/40 divide-y border-y">
        {origins.map((o, i) => (
          <div
            key={`${o.n}:${o.f ?? ''}:${o.l ?? 0}:${i}`}
            className="flex items-start gap-2 py-1.5"
          >
            <Badge variant={o.k === 'route' ? 'info' : 'plain'}>{o.k ?? 'background'}</Badge>
            <div className="min-w-0 flex-1">
              {o.k === 'route' && o.p ? (
                <AppLink
                  href={routeHref(o.ri ?? 0)}
                  className="text-acc text-left font-mono text-[10.5px] break-words hover:underline"
                >
                  {o.m} {o.p}
                </AppLink>
              ) : o.fk && onFn ? (
                <AppLink
                  href={onFn(o.fk)}
                  className="text-acc text-left font-mono text-[10.5px] break-words hover:underline"
                >
                  {o.n}
                </AppLink>
              ) : (
                <div className="text-ink font-mono text-[10.5px] break-words">{o.n}</div>
              )}
              {o.k === 'route' && o.fk && onFn ? (
                <AppLink
                  href={onFn(o.fk)}
                  className="text-acc mt-0.5 font-mono text-[9px] hover:underline"
                >
                  method ›
                </AppLink>
              ) : null}
              {o.via ? <div className="text-faint text-[9px]">via {o.via}</div> : null}
              {o.f ? (
                <div className="text-faint font-mono text-[9px] break-all">
                  {o.f}
                  {o.l ? `:${o.l}` : ''}
                  {o.c && o.c > 1 ? ` · ${o.c} calls` : ''}
                </div>
              ) : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function StreamBoard({
  route,
  flows,
  ri,
}: {
  route: Route
  flows?: Record<string, FuncFlow>
  ri: number
}) {
  const st = route.st
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState('')
  const [picked, setPicked] = useState<{ m: StreamMessage; event?: string } | null>(null)
  useEffect(() => {
    setQuery('')
    setSelected('')
    setPicked(null)
  }, [route.p, route.m])

  const isSse = st?.k === 'sse'
  const recv = st?.recv ?? EMPTY_MESSAGES
  const send = st?.send ?? EMPTY_MESSAGES
  const ext = externalCalls(route, flows)
  const known = useMemo(() => {
    const names = [
      ...new Set(
        [
          ...(st?.events ?? []),
          ...recv.map((m) => m.ev ?? ''),
          ...send.map((m) => m.ev ?? ''),
        ].filter(Boolean),
      ),
    ]
    return names.length || !isSse ? names : ['event stream']
  }, [isSse, st, recv, send])
  const eventOf = useCallback(
    (m: StreamMessage) => m.ev || (m.n && known.includes(m.n) ? m.n : ''),
    [known],
  )
  const uniq = (list: StreamMessage[]) => [
    ...new Map(list.map((m) => [`${m.t}|${m.n}|${m.ev}`, m])).values(),
  ]
  const exchanges = useMemo(
    () =>
      known.map((event) => ({
        event,
        inbound: recv.filter((m) => eventOf(m) === event),
        outbound: isSse ? send : send.filter((m) => eventOf(m) === event),
      })),
    [eventOf, isSse, known, recv, send],
  )
  const preamble = uniq(recv.filter((m) => !eventOf(m) && !/^[A-Z]$/.test(m.t ?? '')))
  const unmapped = uniq(send.filter((m) => !eventOf(m)))
  const q = query.trim().toLowerCase()
  const matches = (x: (typeof exchanges)[number]) => {
    if (!q) return true
    return [
      x.event,
      ...x.inbound.flatMap((m) => [m.n, m.t, m.e, ...(m.f ?? []).map((f) => f.n)]),
      ...x.outbound.flatMap((m) => [m.n, m.t, m.e, ...(m.f ?? []).map((f) => f.n)]),
      ...ext.filter((c) => c.ev === x.event).map((c) => c.n),
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase()
      .includes(q)
  }
  const filtered = exchanges.filter(matches)
  const defaultKey =
    exchanges[0]?.event ?? (preamble.length ? '__preamble' : unmapped.length ? '__unmapped' : '')
  const validSelected =
    selected === '__preamble'
      ? preamble.length > 0
      : selected === '__unmapped'
        ? unmapped.length > 0
        : exchanges.some((x) => x.event === selected)
  const active = validSelected ? selected : defaultKey
  const exchange = exchanges.find((x) => x.event === active)
  const selectedEvent = st?.evs?.find((e) => e.n === active)
  const sharedKey = !selectedEvent?.fk && isSse ? st?.hk : undefined
  const flowKey = selectedEvent?.fk ?? sharedKey
  const eventFlow = flowKey ? flows?.[flowKey] : undefined
  const sharedFlow = !selectedEvent?.fk && !!sharedKey && !!eventFlow
  const effects = eventFlow?.fx ?? []
  const eventCalls = active.startsWith('__') ? [] : ext.filter((x) => x.ev === active)
  const deeperCalls = eventCalls.filter(
    (x) => !effects.some((e) => e.l === x.l && (x.n === e.n || x.n.includes(e.n))),
  )
  const routeCalls = ext.filter((x) => !x.ev)
  const firstFrame = exchange
    ? [...exchange.inbound, ...exchange.outbound].find((m) => m.file)
    : undefined
  const handlerStage = route.ch[route.ch.length - 1]
  const handlerHref = flowKey
    ? fnHref(flowKey, { ri, si: route.ch.length - 1 })
    : handlerStage?.fk
      ? fnHref(handlerStage.fk, { ri, si: route.ch.length - 1 })
      : undefined
  const openFn = (key: string) => fnHref(key, { ri, si: Math.max(0, route.ch.length - 1) })
  const frameCount = exchange
    ? exchange.inbound.length + exchange.outbound.length
    : active === '__preamble'
      ? preamble.length
      : unmapped.length

  const listRows = (
    <>
      {preamble.length > 0 && (
        <button
          type="button"
          onClick={() => setSelected('__preamble')}
          className={`block w-full border-l-2 px-3 py-2 text-left transition-colors ${
            active === '__preamble'
              ? 'border-acc bg-acc/10'
              : 'hover:bg-panel2/60 border-transparent'
          }`}
        >
          <div className="text-ink font-mono text-[11px]">preamble</div>
          <div className="text-faint mt-0.5 text-[9.5px]">
            {preamble.length} frame{preamble.length === 1 ? '' : 's'} before the switch
          </div>
        </button>
      )}
      {preamble.length > 0 && <div className="border-line/50 border-t" />}
      {filtered.map((x) => {
        const calls = ext.filter((c) => c.ev === x.event).length
        return (
          <button
            key={x.event}
            type="button"
            onClick={() => setSelected(x.event)}
            className={`block w-full border-l-2 px-3 py-2 text-left transition-colors ${
              active === x.event ? 'border-acc bg-acc/10' : 'hover:bg-panel2/60 border-transparent'
            }`}
          >
            <div className="text-ink font-mono text-[11px] break-words">{x.event}</div>
            {(!isSse || calls > 0) && (
              <div className="text-faint mt-0.5 text-[9.5px]">
                {isSse ? '' : `${x.inbound.length} in · ${x.outbound.length} out`}
                {calls > 0 ? `${isSse ? '' : ' · '}${calls} call${calls === 1 ? '' : 's'}` : ''}
              </div>
            )}
          </button>
        )
      })}
      {unmapped.length > 0 && <div className="border-line/50 border-t" />}
      {unmapped.length > 0 && (
        <button
          type="button"
          onClick={() => setSelected('__unmapped')}
          className={`block w-full border-l-2 px-3 py-2 text-left transition-colors ${
            active === '__unmapped'
              ? 'border-acc bg-acc/10'
              : 'hover:bg-panel2/60 border-transparent'
          }`}
        >
          <div className="text-ink font-mono text-[11px]">unmapped sends</div>
          <div className="text-faint mt-0.5 text-[9.5px]">
            {unmapped.length} frame{unmapped.length === 1 ? '' : 's'} outside an event
          </div>
        </button>
      )}
      {!filtered.length && q && (
        <p className="text-faint px-3 py-6 text-center text-[10.5px] italic">
          no event matches “{query}”.
        </p>
      )}
      {!exchanges.length && !preamble.length && !unmapped.length && (
        <p className="text-faint px-3 py-6 text-[10.5px] italic">no event dispatch resolved.</p>
      )}
    </>
  )

  return (
    <div className="stream-board h-full">
      <aside className="stream-list border-line bg-panel/20 border-r">
        <div className="border-line sticky top-0 z-10 border-b bg-[var(--color-panel)] p-2">
          <div className="mb-2 flex items-baseline gap-2">
            <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">
              {exchanges.length} events
            </span>
            <span className="text-faint ml-auto text-[9.5px]">
              {isSse
                ? `${send.length} payload${send.length === 1 ? '' : 's'}`
                : `${recv.length} in · ${send.length} out`}
            </span>
          </div>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="filter event, type or field"
            className="border-line bg-canvas text-ink placeholder:text-faint focus:border-line2 w-full rounded-[4px] border px-2 py-1 text-[11px] outline-none"
          />
        </div>
        <div>{listRows}</div>
      </aside>

      <section className="stream-detail min-w-0">
        {active ? (
          <div className="mx-auto max-w-[760px] p-4">
            <div className="border-line flex flex-wrap items-start gap-3 border-b pb-3">
              <div className="min-w-0 flex-1">
                <div className="text-faint text-[9px] tracking-[0.14em] uppercase">
                  {active.startsWith('__') ? 'outside the event switch' : 'selected event'}
                </div>
                <div className="text-ink mt-0.5 font-mono text-[18px] leading-tight break-words">
                  {active === '__preamble'
                    ? 'preamble'
                    : active === '__unmapped'
                      ? 'unmapped sends'
                      : active}
                </div>
                <div className="text-faint mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[10px]">
                  <span>
                    {frameCount} frame{frameCount === 1 ? '' : 's'}
                  </span>
                  {exchange && !isSse && (
                    <span>
                      {exchange.inbound.length} client · {exchange.outbound.length} server
                    </span>
                  )}
                  {eventCalls.length > 0 && (
                    <span>
                      {eventCalls.length} external call{eventCalls.length === 1 ? '' : 's'}
                    </span>
                  )}
                  {firstFrame?.file && (
                    <span className="font-mono">
                      {firstFrame.file}:{firstFrame.l ?? 0}
                    </span>
                  )}
                </div>
              </div>
              {handlerHref && (
                <AppLink
                  href={handlerHref}
                  className={buttonVariants({ variant: 'outline', size: 'sm' })}
                >
                  <Code2 className="size-3" /> handler
                </AppLink>
              )}
            </div>

            {active === '__preamble' ? (
              <div className="mt-4">
                <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">
                  received before the switch
                </div>
                <div className="space-y-1">
                  {preamble.map((m, i) => (
                    <MessageCard key={`pre:${i}`} m={m} onOpen={() => setPicked({ m })} />
                  ))}
                </div>
              </div>
            ) : active === '__unmapped' ? (
              <div className="mt-4">
                <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">
                  sent outside a resolved event
                </div>
                <div className="space-y-1">
                  {unmapped.map((m, i) => (
                    <MessageCard key={`un:${i}`} m={m} onOpen={() => setPicked({ m })} />
                  ))}
                </div>
              </div>
            ) : exchange ? (
              <>
                <TriggeredBy event={selectedEvent} isSse={isSse} onFn={openFn} />

                {!isSse && (
                  <div className="mt-4">
                    <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">
                      client sends
                    </div>
                    {exchange.inbound.length ? (
                      <div className="space-y-1">
                        {exchange.inbound.map((m, i) => (
                          <MessageCard
                            key={`in:${i}`}
                            m={m}
                            event={exchange.event}
                            onOpen={() => setPicked({ m, event: exchange.event })}
                          />
                        ))}
                      </div>
                    ) : (
                      <p className="border-line text-faint rounded-[4px] border border-dashed px-3 py-2 text-[10.5px]">
                        server-initiated · no client frame for this event
                      </p>
                    )}
                  </div>
                )}

                {eventFlow?.flow?.length ? (
                  <div className="mt-4">
                    <div className="text-faint mb-1.5 flex items-center gap-2 text-[9.5px] tracking-[0.14em] uppercase">
                      <span>processing</span>
                      {sharedFlow ? (
                        <span className="text-faint text-[9px] tracking-normal normal-case">
                          shared stream loop
                        </span>
                      ) : null}
                      {flowKey ? (
                        <AppLink
                          href={fnHref(flowKey, { ri, si: Math.max(0, route.ch.length - 1) })}
                          className="text-acc ml-auto text-[9.5px] tracking-normal normal-case hover:underline"
                        >
                          full flow ›
                        </AppLink>
                      ) : null}
                    </div>
                    <TraceRows steps={eventFlow.flow} onFn={openFn} limit={sharedFlow ? 9 : 3} />
                  </div>
                ) : null}

                <div className="text-faint my-4 flex items-center gap-2 text-[9.5px] tracking-[0.14em] uppercase">
                  <span className="bg-line h-px flex-1" />
                  {isSse ? 'handler writes' : 'handler dispatches'}
                  <span className="bg-line h-px flex-1" />
                </div>

                <div>
                  <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">
                    {isSse ? 'payload sent' : 'server replies'}
                  </div>
                  {exchange.outbound.length ? (
                    <div className="space-y-1">
                      {exchange.outbound.map((m, i) => (
                        <MessageCard
                          key={`out:${i}`}
                          m={m}
                          event={exchange.event}
                          onOpen={() => setPicked({ m, event: exchange.event })}
                        />
                      ))}
                    </div>
                  ) : (
                    <p className="border-line text-faint rounded-[4px] border border-dashed px-3 py-2 text-[10.5px]">
                      no reply resolved.
                    </p>
                  )}
                  {isSse &&
                    exchange.outbound.length > 0 &&
                    exchange.outbound.every((m) => !m.ev) && (
                      <p className="text-faint mt-1.5 text-[10px]">
                        this payload shape is sent for every event.
                      </p>
                    )}
                </div>

                {effects.length > 0 && (
                  <div className="mt-6">
                    <div className="text-faint mb-2 text-[9.5px] tracking-[0.14em] uppercase">
                      after effects
                    </div>
                    <EffectRows items={effects} onFn={openFn} />
                  </div>
                )}
              </>
            ) : null}

            <div className="border-line mt-6 border-t pt-3">
              <div className="text-faint mb-2 text-[9.5px] tracking-[0.14em] uppercase">
                deeper calls
              </div>
              {deeperCalls.length ? (
                <ExternalCalls items={deeperCalls} onFn={openFn} />
              ) : (
                <p className="text-faint text-[10.5px] italic">
                  no deeper database, cache or outbound call is tied to this event.
                </p>
              )}
              {routeCalls.length > 0 && (
                <details className="border-line/50 mt-3 border-t pt-2">
                  <summary className="text-faint hover:text-mut cursor-pointer text-[10px]">
                    route-wide calls · {routeCalls.length}
                  </summary>
                  <div className="mt-2">
                    <ExternalCalls items={routeCalls} onFn={openFn} />
                  </div>
                </details>
              )}
            </div>
          </div>
        ) : (
          <p className="text-faint p-6 text-[11px] italic">
            no event dispatch resolved — open the flow to follow the handler.
          </p>
        )}
      </section>

      <MessageSheet
        message={picked?.m}
        event={picked?.event}
        open={picked !== null}
        onOpenChange={(v) => !v && setPicked(null)}
      />
    </div>
  )
}

/* ---------- views ---------- */

function RouterView() {
  const data = useData()
  const svc = useSvc()
  const params = useParams({ strict: false }) as { _splat?: string }
  const focus = params._splat ?? ''
  const [proto, setProto] = useState<'all' | 'http' | 'ws' | 'sse'>('all')
  const scene = useMemo(
    () => buildRouterScene(data, focus, proto, svc),
    [data, focus, proto, svc],
  )
  const label = focus ? '/' + focus : data.repo
  const count = data.routes.filter(
    (r) =>
      (proto === 'all' ? true : proto === 'http' ? !r.pr : r.pr === proto) &&
      (!svc || r.svc === svc) &&
      (focus ? r.p === '/' + focus || r.p.startsWith('/' + focus + '/') : true),
  ).length
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
            meta={`${count} under this path · click a box to open it`}
            note="each column is one path segment. open a group to walk deeper, a route to see its middleware."
          />
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

function PipelineView() {
  const data = useData()
  const params = useParams({ strict: false }) as { ri: string }
  const ri = Number(params.ri)
  const r = data.routes[ri]
  const timing = r ? timingOf(data, r) : undefined
  const over = timing
  const scene = useMemo(
    () => (r ? buildPipelineScene(r, ri, over, data.flows) : { nodes: [], edges: [], key: '' }),
    [r, ri, over, data.flows],
  )
  const [sheet, setSheet] = useState<number | null>(null)
  const [streamView, setStreamView] = useState<'exchanges' | 'canvas'>('exchanges')

  useEffect(() => {
    setStreamView('exchanges')
  }, [ri])

  if (!r) return <div className="text-faint p-6 text-[12px]">no such route</div>

  const handler = r.ch[r.ch.length - 1]
  const mw = r.ch.filter((s) => (s.r ?? '') === 'middleware').length
  const tags = [...new Set(r.ch.flatMap((s) => touchTags(s, false)))]
  const current = sheet !== null ? r.ch[sheet] : undefined
  const flow = current?.fk ? flowOf(data, current.fk) : undefined
  const slow = timing && timing.slow >= 0 ? timing.st.find((x) => x.i === timing.slow) : undefined
  const slowName = slow ? r.ch[slow.i]?.n : undefined
  const meta =
    `${r.f}:${r.l} · ${r.ch.length} step${r.ch.length === 1 ? '' : 's'} · ${mw} middleware first` +
    (over ? ` · ${msOf(over.total)} per request · ${over.spans} spans` : '')
  const note = over
    ? slow && slowName
      ? `slowest stage: ${slowName} — ${msOf(slow.m)} median, ${msOf(slow.p95)} p95, ${Math.round(slow.sh * 100)}% of the time`
      : 'the trace matched this route but not its stages'
    : handler?.d
      ? undefined
      : 'no doc comment on the handler — open it, the flow is the description'

  const card = (
    <ScopeCard
      title={`${r.m} ${r.p}`}
      badge={r.pr ? r.pr.toUpperCase() : 'route'}
      meta={meta}
      doc={over ? undefined : handler?.d}
      note={note}
      tags={tags}
      onSource={handler ? () => setSheet(r.ch.length - 1) : undefined}
      sourceLabel="handler"
    />
  )
  const sheets = (
    <DetailSheet
      open={sheet !== null}
      onOpenChange={(v) => !v && setSheet(null)}
      title={current?.n ?? ''}
      subtitle={current?.f ? `${current.f}:${current.l}` : undefined}
      doc={current?.d}
      src={current?.s ?? flow?.s}
      calls={current?.c}
      stage={current}
      inputs={current?.in}
    />
  )

  if (r.pr && r.st) {
    return (
      <div className="flex h-full flex-col">
        {card}
        <div className="border-line bg-panel/30 flex flex-wrap items-center gap-2 border-b px-3 py-1.5">
          <Segmented
            value={streamView}
            options={[
              { value: 'exchanges', label: 'events' },
              { value: 'canvas', label: 'flow' },
            ]}
            onChange={(v) => setStreamView(v as 'exchanges' | 'canvas')}
          />
          <span className="text-faint text-[10px]">
            pick an event to read its request, replies and calls
          </span>
        </div>
        <div className="relative min-h-0 flex-1">
          {streamView === 'exchanges' ? (
            <StreamBoard route={r} flows={data.flows} ri={ri} />
          ) : (
            <SceneCanvas scene={scene} />
          )}
        </div>
        <div className="border-line bg-panel/30 max-h-[26vh] shrink-0 overflow-auto border-t">
          <DocPanel r={r} />
        </div>
        {sheets}
      </div>
    )
  }

  return (
    <GraphPage scene={scene} card={card}>
      <DocPanel r={r} />
      {sheets}
    </GraphPage>
  )
}

function FlowView() {
  const data = useData()
  const params = useParams({ strict: false }) as { ri: string; si: string }
  const ri = Number(params.ri)
  const si = Number(params.si)
  const r = data.routes[ri]
  const st = r?.ch[si]
  const flow = st?.fk ? flowOf(data, st.fk) : undefined
  const key = st?.fk ?? 'none'
  const nextStage = useMemo(
    () =>
      r && si + 1 < r.ch.length
        ? { label: r.ch[si + 1].n, href: '/r/' + ri + '/s/' + (si + 1) }
        : undefined,
    [r, ri, si],
  )
  const prevStage = useMemo(
    () =>
      r && si > 0 ? { label: r.ch[si - 1].n, href: '/r/' + ri + '/s/' + (si - 1) } : undefined,
    [r, ri, si],
  )
  const scene = useMemo(
    () =>
      flow
        ? buildFlowScene(flow, key, nextStage, prevStage, st?.in, { ri, si }, data.errs)
        : { nodes: [], edges: [], key: '' },
    [flow, key, nextStage, prevStage, st?.in, ri, si, data.errs],
  )
  const [sheet, setSheet] = useState(false)
  const [focus, setFocus] = useState<{ line?: number; lines?: number[] }>({})
  useEffect(() => {
    const onSrc = (e: Event) => {
      const detail = (e as CustomEvent<{ line: number; startLine?: number; lines?: number[] }>)
        .detail
      const offset = (detail.startLine ?? 1) - 1
      const lines = detail.lines?.map((l) => l - offset)
      setFocus({ line: lines?.[0] ?? detail.line - offset, lines })
      setSheet(true)
    }
    window.addEventListener('synoptic:source', onSrc)
    return () => window.removeEventListener('synoptic:source', onSrc)
  }, [])

  if (!r || !st) return <div className="text-faint p-6 text-[12px]">no such step</div>

  return (
    <GraphPage
      scene={scene}
      card={
        <ScopeCard
          title={st.n}
          badge={st.r ?? st.k}
          meta={st.f ? `${st.f}:${st.l}` : undefined}
          doc={st.d}
          note={
            !st.d
              ? flow
                ? 'no doc comment on this one — the flow below is what it does'
                : 'no doc comment, and the body is outside this repo'
              : undefined
          }
          tags={touchTags(st)}
          onSource={() => {
            setFocus({})
            setSheet(true)
          }}
          sourceLabel="source"
        />
      }
    >
      {st.in && st.in.length > 0 && <Inputs items={st.in} flush />}
      <DetailSheet
        open={sheet}
        onOpenChange={setSheet}
        title={st.n}
        subtitle={st.f ? `${st.f}:${st.l}` : undefined}
        doc={st.d}
        src={st.s ?? flow?.s}
        calls={st.c}
        stage={st}
        inputs={st.in}
        focus={focus.line}
        focusLines={focus.lines}
      />
    </GraphPage>
  )
}

function FnView() {
  const data = useData()
  const search = useSearch({ strict: false }) as {
    k?: string
    ri?: string
    si?: string
    hl?: string
  }
  const key = search.k ?? ''
  const flow: FuncFlow | undefined = flowOf(data, key)
  const hl = numParam(search.hl)
  const traceSite =
    hl !== undefined ? data.errs?.find((e) => e.fn === key && e.l === hl) : undefined
  const parentRi = Number(search.ri)
  const parentSi = Number(search.si)
  const parentOk =
    search.ri !== undefined &&
    search.si !== undefined &&
    Number.isInteger(parentRi) &&
    Number.isInteger(parentSi)
  // A trace URL carries only the function and line. Recover the route that
  // reaches it so the crumbs and stage jumps stay useful.
  const derived = useMemo(() => {
    if (parentOk || !traceSite) return undefined
    const ref = traceSite.r?.find((r) => r.k !== 'rpc')
    const route = ref ? data.routes[ref.ri] : undefined
    const stage = route?.ch.findIndex((s) => s.fk === key) ?? -1
    return route && ref && stage >= 0 ? { ri: ref.ri, si: stage } : undefined
  }, [parentOk, traceSite, data.routes, key])
  const atRi = parentOk ? parentRi : derived?.ri
  const atSi = parentOk ? parentSi : derived?.si
  const origin = useMemo(
    () => (atRi !== undefined && atSi !== undefined ? { ri: atRi, si: atSi } : undefined),
    [atRi, atSi],
  )
  const parent = atRi !== undefined ? data.routes[atRi] : undefined
  const nextStage = useMemo(
    () =>
      parent && atSi !== undefined && atSi + 1 < parent.ch.length
        ? { label: parent.ch[atSi + 1].n, href: `/r/${atRi}/s/${atSi + 1}` }
        : undefined,
    [parent, atRi, atSi],
  )
  const prevStage = useMemo(
    () =>
      parent && atSi !== undefined && atSi > 0
        ? { label: parent.ch[atSi - 1].n, href: `/r/${atRi}/s/${atSi - 1}` }
        : undefined,
    [parent, atRi, atSi],
  )
  const scene = useMemo(
    () =>
      flow
        ? buildFlowScene(flow, key, nextStage, prevStage, flow.in, origin, data.errs)
        : { nodes: [], edges: [], key: '' },
    [flow, key, nextStage, prevStage, origin, data.errs],
  )
  const [sheet, setSheet] = useState(false)
  const [focus, setFocus] = useState<{ line?: number; lines?: number[] }>({})
  useEffect(() => {
    const onSrc = (e: Event) => {
      const detail = (e as CustomEvent<{ line: number; startLine?: number; lines?: number[] }>)
        .detail
      const offset = (detail.startLine ?? 1) - 1
      const lines = detail.lines?.map((l) => l - offset)
      setFocus({ line: lines?.[0] ?? detail.line - offset, lines })
      setSheet(true)
    }
    window.addEventListener('synoptic:source', onSrc)
    return () => window.removeEventListener('synoptic:source', onSrc)
  }, [])

  const rpc = useMemo(() => {
    const [si, mi] = (flow?.rpc ?? '').split('/').map(Number)
    const svc = data.services?.[si]
    const m = svc?.methods[mi]
    if (!svc || !m) return undefined
    const name = svc.n.includes('.') ? svc.n.slice(svc.n.lastIndexOf('.') + 1) : svc.n
    return { label: `${name}/${m.n}`, href: `/rpc/${si}/${mi}` }
  }, [flow, data])

  if (!flow)
    return <div className="text-faint p-6 text-[12px]">this function is not in the module</div>

  return (
    <GraphPage
      scene={scene}
      focusLine={hl}
      trace={
        traceSite ? { msg: traceSite.m, file: traceSite.f, line: traceSite.l } : undefined
      }
      card={
        <ScopeCard
          title={flow.n}
          badge="func"
          meta={
            flow.f
              ? `${flow.f}:${flow.l}${parent ? ` · ${parent.m} ${parent.p} · stage ${parentSi + 1}` : ''}`
              : key
          }
          doc={flow.d}
          note={!flow.d ? 'no doc comment on this one — the flow below is what it does' : undefined}
          onSource={() => {
            setFocus({})
            setSheet(true)
          }}
          sourceLabel="source"
          rpc={rpc}
        />
      }
    >
      {flow.in && flow.in.length > 0 && <Inputs items={flow.in} flush />}
      <DetailSheet
        open={sheet}
        onOpenChange={setSheet}
        title={flow.n}
        subtitle={flow.f ? `${flow.f}:${flow.l}` : key}
        doc={flow.d}
        src={flow.s}
        inputs={flow.in}
        focus={focus.line}
        focusLines={focus.lines}
      />
    </GraphPage>
  )
}

/* ---------- types ---------- */

function TypeView() {
  const data = useData()
  const search = useSearch({ strict: false }) as {
    k?: string
    i?: string
    ri?: string
    si?: string
  }
  const key = search.k ?? ''
  const t: SynType | undefined = data.types?.[key]
  if (!t) return <div className="text-faint p-6 text-[12px]">this type is not in the module</div>
  const iface = search.i ? data.types?.[search.i] : undefined
  const ri = numParam(search.ri)
  const si = numParam(search.si)
  const origin =
    ri !== undefined && data.routes[ri] ? `&ri=${ri}${si !== undefined ? `&si=${si}` : ''}` : ''
  const methodHref = (mk?: string) => {
    if (!mk) return
    const sk = t.k === 'struct' ? t.key : undefined
    const ik = t.k === 'interface' ? t.key : iface?.key
    const q = [`k=${encodeURIComponent(mk)}`]
    if (ik) q.push(`i=${encodeURIComponent(ik)}`)
    if (sk) q.push(`t=${encodeURIComponent(sk)}`)
    return `/f?${q.join('&')}${origin}`
  }
  // resolve a field's written type back to a catalog entry, so fields with a
  // struct or interface type can be opened like methods
  const fieldType = (raw: string): SynType | undefined => {
    const norm = raw.replace(/^\*+/, '').trim()
    const base = norm.match(/([A-Za-z_]\w*)$/)?.[1]
    if (!base) return undefined
    const qual = norm.includes('.') ? norm.slice(0, norm.lastIndexOf('.')).split('.').pop() : ''
    if (qual) {
      return Object.values(data.types ?? {}).find(
        (x) => x.n === base && (x.pkg === qual || x.pkg.endsWith('/' + qual)),
      )
    }
    const same = data.types?.[`${t.pkg}|${base}`]
    if (same) return same
    const matches = Object.values(data.types ?? {}).filter((x) => x.n === base)
    return matches.length === 1 ? matches[0] : undefined
  }
  return (
    <div className="flex h-full flex-col overflow-auto">
      <ScopeCard
        title={t.n}
        badge={t.k}
        meta={t.f ? `${t.f}:${t.l ?? 1}` : t.pkg}
        note={
          t.k === 'interface'
            ? 'interface · implemented by the structs below'
            : 'struct · fields and methods'
        }
      />
      <div className="grid gap-3 p-4 lg:grid-cols-2">
        {t.k === 'struct' && (
          <section className="border-line bg-panel/40 rounded-[6px] border">
            <div className="border-line text-mut border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
              fields
            </div>
            <div className="divide-line/60 divide-y">
              {(t.fields ?? []).length === 0 && (
                <div className="text-faint px-3 py-2 text-[11px]">no fields</div>
              )}
              {(t.fields ?? []).map((f) => {
                const ft = fieldType(f.t)
                const href = ft ? `/t?k=${encodeURIComponent(ft.key)}${origin}` : undefined
                const body = (
                  <>
                    <span className="text-ink">{f.n}</span>
                    <span className="text-info truncate">{f.t}</span>
                    {f.e && (
                      <span className="text-faint text-[9px] tracking-[0.1em] uppercase">
                        embedded
                      </span>
                    )}
                    {ft && <span className="text-faint ml-auto shrink-0 text-[9px]">open ›</span>}
                  </>
                )
                const cls = `flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px] ${ft ? 'hover:bg-panel2' : ''}`
                return href ? (
                  <AppLink key={f.n + f.t} href={href} className={cls}>
                    {body}
                  </AppLink>
                ) : (
                  <div key={f.n + f.t} className={cls}>
                    {body}
                  </div>
                )
              })}
            </div>
          </section>
        )}
        <section className="border-line bg-panel/40 rounded-[6px] border">
          <div className="border-line text-mut border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
            methods
          </div>
          <div className="divide-line/60 divide-y">
            {(t.methods ?? []).length === 0 && (
              <div className="text-faint px-3 py-2 text-[11px]">no methods</div>
            )}
            {(t.methods ?? []).map((m) => {
              // flows are baked from routes, so a method in the catalog does
              // not always have one. Linking it would land on a dead page.
              const traced = Boolean(m.k && data.flows[m.k])
              const href = traced ? methodHref(m.k) : undefined
              const body = (
                <>
                  <span className="text-ink">{m.n}</span>
                  <span className="text-mut min-w-0 flex-1 truncate">{m.sig}</span>
                  {traced && <span className="text-faint shrink-0 text-[9px]">open ›</span>}
                </>
              )
              const cls = `flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px] ${traced ? 'hover:bg-panel2' : ''}`
              return href ? (
                <AppLink key={m.n + (m.k ?? '')} href={href} className={cls}>
                  {body}
                </AppLink>
              ) : (
                <div key={m.n + (m.k ?? '')} className={cls}>
                  {body}
                </div>
              )
            })}
          </div>
        </section>
        {t.k === 'interface' && (
          <section className="border-line bg-panel/40 rounded-[6px] border">
            <div className="border-line text-mut border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
              implemented by
            </div>
            <div className="divide-line/60 divide-y">
              {(t.impls ?? []).length === 0 && (
                <div className="text-faint px-3 py-2 text-[11px]">no in-module implementation</div>
              )}
              {(t.impls ?? []).map((ik) => {
                const it = data.types?.[ik]
                if (!it) return null
                return (
                  <AppLink
                    key={ik}
                    href={`/t?k=${encodeURIComponent(ik)}&i=${encodeURIComponent(t.key)}${origin}`}
                    className="hover:bg-panel2 flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px]"
                  >
                    <span className="text-ink">{it.n}</span>
                    <span className="text-mut min-w-0 flex-1 truncate">{it.pkg}</span>
                    <span className="text-faint text-[9px]">open ›</span>
                  </AppLink>
                )
              })}
            </div>
          </section>
        )}
        {t.k === 'struct' && (t.impls ?? []).length > 0 && (
          <section className="border-line bg-panel/40 rounded-[6px] border">
            <div className="border-line text-mut border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
              implements
            </div>
            <div className="divide-line/60 divide-y">
              {(t.impls ?? []).map((ik) => {
                const it = data.types?.[ik]
                if (!it) return null
                return (
                  <AppLink
                    key={ik}
                    href={`/t?k=${encodeURIComponent(ik)}${origin}`}
                    className="hover:bg-panel2 flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px]"
                  >
                    <span className="text-ink">{it.n}</span>
                    <span className="text-mut min-w-0 flex-1 truncate">{it.pkg}</span>
                  </AppLink>
                )
              })}
            </div>
          </section>
        )}
      </div>
    </div>
  )
}

/* ---------- rpc ---------- */

const EMPTY_MESSAGES: StreamMessage[] = []
const emptyScene: Scene = { nodes: [], edges: [], key: '' }

function kindTone(k: string): 'default' | 'info' | 'warn' | 'accent' {
  if (k === 'bidi') return 'accent'
  if (k === 'server stream') return 'info'
  if (k === 'client stream') return 'warn'
  return 'default'
}

function RpcCell({ label, value }: { label: string; value?: string }) {
  if (!value) return null
  return (
    <div className="min-w-0">
      <div className="text-faint text-[9.5px] tracking-[0.14em] uppercase">{label}</div>
      <div className="text-ink mt-0.5 truncate text-[11.5px]" title={value}>
        {value}
      </div>
    </div>
  )
}

function RPCView() {
  const data = useData()
  const svc = useSvc()
  const services = (data.services ?? []).map((s, si) => ({ s, si }))
  const shown = services.filter(({ s }) => !svc || s.svc === svc)
  if (services.length === 0) {
    return (
      <div className="text-faint p-6 text-[12px]">no gRPC services were found in this module</div>
    )
  }
  if (shown.length === 0) {
    return (
      <div className="text-faint p-6 text-[12px]">
        no gRPC services in {svc} — switch the service filter back to all
      </div>
    )
  }
  return (
    <div className="h-full overflow-auto p-4">
      <div className="mb-3 flex items-baseline gap-2">
        <span className="text-ink text-[13px]">RPC</span>
        <span className="text-faint text-[10.5px]">
          {shown.length} {shown.length === 1 ? 'service' : 'services'} ·{' '}
          {shown.reduce((n, { s }) => n + s.methods.length, 0)} methods
          {svc ? ` · ${svc}` : ''}
        </span>
      </div>
      <div className="grid gap-3 lg:grid-cols-2">
        {shown.map(({ s, si }) => (
          <section
            key={s.n}
            className="border-line bg-panel/30 overflow-hidden rounded-[6px] border"
          >
            <header className="border-line border-b px-3 py-2">
              <div className="flex items-baseline gap-2">
                <span className="text-ink truncate text-[12.5px]">{s.n}</span>
                {s.p && <span className="text-faint text-[10px]">{s.p}</span>}
                <span className="text-faint ml-auto shrink-0 text-[10px]">
                  {s.methods.length} methods
                </span>
              </div>
              {s.f && (
                <div className="text-faint mt-0.5 text-[10px]">
                  {s.f}
                  {s.l ? `:${s.l}` : ''}
                </div>
              )}
              {(s.mw ?? []).length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1">
                  {s.mw!.map((mw) => (
                    <Badge key={mw.n} variant="plain">
                      {mw.n}
                    </Badge>
                  ))}
                </div>
              )}
            </header>
            <div className="divide-line/60 divide-y">
              {s.methods.map((m, mi) => (
                <AppLink
                  key={m.n}
                  href={`/rpc/${si}/${mi}`}
                  className="hover:bg-panel2 flex w-full flex-col gap-1 px-3 py-2 text-left"
                >
                  <div className="flex items-baseline gap-2">
                    <Badge variant={kindTone(m.k)}>{m.k}</Badge>
                    <span className="text-ink truncate text-[11.5px]">{m.n}</span>
                    <span className="text-faint ml-auto shrink-0 text-[9.5px]">{m.p}</span>
                  </div>
                  <div className="text-mut flex min-w-0 items-baseline gap-1.5 text-[10.5px]">
                    <span className="truncate">{m.rq || 'request'}</span>
                    <span className="text-faint">→</span>
                    <span className="truncate">{m.rs || 'response'}</span>
                  </div>
                  {m.d && <div className="text-mut line-clamp-2 text-[10.5px]">{m.d}</div>}
                </AppLink>
              ))}
            </div>
          </section>
        ))}
      </div>
    </div>
  )
}

function RPCMethodView() {
  const data = useData()
  const params = useParams({ strict: false }) as { si: string; mi: string }
  const si = Number(params.si)
  const mi = Number(params.mi)
  const svc = data.services?.[si]
  const method = svc?.methods[mi]
  const key = method?.fk ?? ''
  const flow: FuncFlow | undefined = key ? flowOf(data, key) : undefined
  const search = useSearch({ strict: false }) as { hl?: string }
  const hl = numParam(search.hl)
  const traceSite =
    hl !== undefined ? data.errs?.find((e) => e.fn === key && e.l === hl) : undefined
  const scene = useMemo(
    () =>
      flow
        ? buildFlowScene(flow, key, undefined, undefined, flow.in, undefined, data.errs)
        : emptyScene,
    [flow, key, data.errs],
  )
  const [sheet, setSheet] = useState(false)
  const [focus, setFocus] = useState<{ line?: number; lines?: number[] }>({})
  useEffect(() => {
    const onSrc = (e: Event) => {
      const detail = (e as CustomEvent<{ line: number; startLine?: number; lines?: number[] }>)
        .detail
      const offset = (detail.startLine ?? 1) - 1
      const lines = detail.lines?.map((l) => l - offset)
      setFocus({ line: lines?.[0] ?? detail.line - offset, lines })
      setSheet(true)
    }
    window.addEventListener('synoptic:source', onSrc)
    return () => window.removeEventListener('synoptic:source', onSrc)
  }, [])

  if (!svc || !method)
    return <div className="text-faint p-6 text-[12px]">this RPC method is not indexed</div>

  return (
    <GraphPage
      scene={scene}
      focusLine={hl}
      trace={
        traceSite ? { msg: traceSite.m, file: traceSite.f, line: traceSite.l } : undefined
      }
      card={
        <ScopeCard
          title={`${svc.n} / ${method.n}`}
          badge={method.k}
          meta={method.f ? `${method.f}:${method.l} · ${svc.n}` : svc.n}
          doc={method.d}
          note={!method.d ? 'no doc comment on this method' : undefined}
          onSource={
            flow
              ? () => {
                  setFocus({})
                  setSheet(true)
                }
              : undefined
          }
          sourceLabel="source"
        />
      }
    >
      <div className="grid gap-3 p-3 md:grid-cols-4">
        <RpcCell label="service" value={svc.n} />
        <RpcCell label="kind" value={method.k} />
        <RpcCell label="request" value={method.rq} />
        <RpcCell label="response" value={method.rs} />
      </div>
      {(svc.mw ?? []).length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 px-3 pb-3">
          <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">interceptors</span>
          {svc.mw!.map((mw) => (
            <Badge key={mw.n} variant="plain">
              {mw.n}
            </Badge>
          ))}
        </div>
      )}
      <div className="border-line flex flex-wrap items-center gap-2 border-t px-3 py-2">
        <code className="text-mut min-w-0 flex-1 truncate text-[10.5px]">{method.curl}</code>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            if (method.curl) navigator.clipboard?.writeText(method.curl)
            window.dispatchEvent(
              new CustomEvent('synoptic:toast', { detail: { message: 'grpcurl copied' } }),
            )
          }}
        >
          copy
        </Button>
        {method.fk && (
          <AppLink
            href={`/f?k=${encodeURIComponent(method.fk)}`}
            className={buttonVariants({ variant: 'ghost', size: 'sm' })}
          >
            open flow
          </AppLink>
        )}
      </div>
      {flow && (
        <DetailSheet
          open={sheet}
          onOpenChange={setSheet}
          title={flow.n}
          subtitle={flow.f ? `${flow.f}:${flow.l}` : flow.n}
          doc={flow.d}
          src={flow.s}
          focus={focus.line}
          focusLines={focus.lines}
        />
      )}
    </GraphPage>
  )
}

/* ---------- router ---------- */

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
