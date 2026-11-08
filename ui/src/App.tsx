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
import { Code2, Search } from 'lucide-react'
import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { GoSource } from './components/GoSource'
import { Badge } from './components/ui/badge'
import { Button } from './components/ui/button'
import { SearchDialog } from './components/ui/command'
import { Segmented } from './components/ui/segmented'
import { Sheet } from './components/ui/sheet'
import { SceneCanvas } from './graph/SceneCanvas'
import { buildPipelineScene, buildRouterScene, externalCalls, type Scene } from './graph/scenes'
import { buildFlowScene } from './graph/flowscene'
import { DocPanel, Inputs } from './panes/DocPanel'
import { flowOf, fnHref, frameName, loadData, msOf, routeHref, stageHref, tagLabel, timingOf, touchTags } from './lib/data'
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

/* ---------- data ---------- */

const DataCtx = createContext<Data | null>(null)
const useData = () => {
  const d = useContext(DataCtx)
  if (!d) throw new Error('data not loaded')
  return d
}

/* ---------- crumbs ---------- */

type Crumb = { label: string; href?: string }

function useCrumbs(): Crumb[] {
  const data = useData()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const search = useSearch({ strict: false }) as { k?: string; i?: string; t?: string; ri?: string; si?: string }
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
      const same = st && (st.n === skipStage || Boolean(skipStage && skipStage.endsWith('.' + st.n)))
      if (st && !same) {
        const fk = (st.fk ?? '').split('|')
        const self = fk.length >= 3 ? data.types?.[`${fk[0]}|${fk[1]}`] : undefined
        const origin = `&ri=${ri}&si=${si}`
        if (self) {
          const impl = self.impls?.length === 1 ? data.types?.[self.impls[0]] : undefined
          if (impl) crumbs.push({ label: impl.n, href: `/t?k=${encodeURIComponent(impl.key)}${origin}` })
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
    const ri = Number(search.ri ?? '')
    const si = Number(search.si ?? '')
    const origin = Number.isFinite(ri) ? `&ri=${ri}${Number.isFinite(si) ? `&si=${si}` : ''}` : ''
    if (Number.isFinite(ri) && data.routes[ri]) {
      routeChain(ri, Number.isFinite(si) ? si : undefined)
    }
    if (iface) crumbs.push({ label: iface.n, href: `/t?k=${encodeURIComponent(iface.key)}${origin}` })
    if (self) crumbs.push({ label: self.n, href: `/t?k=${encodeURIComponent(self.key)}${search.i ? `&i=${encodeURIComponent(search.i)}` : ''}${origin}` })
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
    const ri = Number(search.ri ?? '')
    const si = Number(search.si ?? '')
    const origin = Number.isFinite(ri) ? `&ri=${ri}${Number.isFinite(si) ? `&si=${si}` : ''}` : ''
    if (Number.isFinite(ri) && data.routes[ri]) {
      routeChain(ri, Number.isFinite(si) ? si : undefined, f?.n)
    }
    if (iface) crumbs.push({ label: iface.n, href: `/t?k=${encodeURIComponent(iface.key)}${origin}` })
    if (st) crumbs.push({ label: st.n, href: `/t?k=${encodeURIComponent(st.key)}${iface ? `&i=${encodeURIComponent(iface.key)}` : ''}${origin}` })
    if (f && crumbs[crumbs.length - 1]?.label !== f.n) crumbs.push({ label: f.n, href: '#' })
  }

  return crumbs
}

/* ---------- shell ---------- */

function Topbar({ onSearch }: { onSearch: () => void }) {
  const data = useData()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const parts = pathname.split('/').filter(Boolean)
  const level = parts[0] === 'rpc' ? '' : parts[0] === 'f' ? 'code' : parts[0] === 'r' ? (parts[2] === 's' ? 'step' : 'route') : 'router'
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
    <header className="flex flex-wrap items-center gap-x-5 gap-y-2 border-b border-line px-4 py-2">
      <div className="flex items-baseline gap-2">
        <span className="text-[10px] uppercase tracking-[0.18em] text-faint">route synoptic</span>
        <span className="text-[13px] text-ink">{data.repo}</span>
      </div>
      <div className="hidden gap-x-3 text-[10.5px] text-mut sm:flex">
        <span><b className="font-semibold text-ink">{data.stats.routes}</b> routes</span>
        {(data.stats.streams ?? 0) > 0 && (
          <span><b className="font-semibold text-ink">{data.stats.streams}</b> stream{data.stats.streams === 1 ? '' : 's'}</span>
        )}
        {(data.stats.services ?? 0) > 0 && (
          <span>
            <b className="font-semibold text-ink">{data.stats.services}</b> rpc ·{' '}
            <b className="font-semibold text-ink">{data.stats.rpcs}</b> {data.stats.rpcs === 1 ? 'method' : 'methods'}
          </span>
        )}
        <span><b className="font-semibold text-ink">{data.stats.middleware}</b> middleware</span>
        <span><b className="font-semibold text-ink">{data.stats.documented}</b>/{data.stats.routes} documented</span>
        <span><b className="font-semibold text-ink">{data.stats.funcs}</b> flows</span>
      </div>
      <div className="ml-auto flex flex-wrap items-center gap-3">
        <div className="flex items-center gap-1.5">
          <span className="text-[9.5px] uppercase tracking-[0.14em] text-faint">level</span>
          <Segmented
            value={level}
            onChange={go}
            options={[
              { value: 'router', label: 'router' },
              { value: 'route', label: 'route' },
              { value: 'step', label: 'step' },
              { value: 'code', label: 'code' },
            ]}
          />
        </div>
        {(data.stats.services ?? 0) > 0 && (
          <Button variant={parts[0] === 'rpc' ? 'default' : 'ghost'} size="sm" onClick={() => goto('/rpc')}>
            rpc <span className="text-[10px] opacity-70">{data.stats.rpcs}</span>
          </Button>
        )}
        <Button variant="default" size="sm" onClick={onSearch}>
          <Search className="size-3" /> search
          <kbd className="ml-1 rounded-[3px] border border-line px-1 text-[9.5px] text-faint">⌘K</kbd>
        </Button>
      </div>
    </header>
  )
}

function Crumbs() {
  const crumbs = useCrumbs()
  return (
    <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1 border-b border-line px-4 py-1.5 text-[11px] text-mut">
      <Button variant="ghost" size="sm" className="mr-1 h-5 px-1.5" onClick={() => window.history.back()}>
        back
      </Button>
      {crumbs.map((c, i) => (
        <span key={i} className="flex items-center gap-1.5">
          {i > 0 && <span className="text-faint">›</span>}
          {c.href && i < crumbs.length - 1 ? (
            <button className="rounded-[3px] px-1 hover:bg-panel hover:text-ink" onClick={() => goto(c.href!)}>
              {c.label}
            </button>
          ) : (
            <span className={i === crumbs.length - 1 ? 'text-ink' : ''}>{c.label}</span>
          )}
        </span>
      ))}
    </div>
  )
}

function Shell() {
  const data = useData()
  const [searchOpen, setSearchOpen] = useState(false)
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
    window.addEventListener('synoptic:toast', onToast)
    window.addEventListener('synoptic:search', onSearch)
    return () => {
      window.clearTimeout(hide)
      window.removeEventListener('synoptic:toast', onToast)
      window.removeEventListener('synoptic:search', onSearch)
    }
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing = (e.target as HTMLElement | null)?.closest('input,textarea')
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setSearchOpen((v) => !v)
        return
      }
      if (
        document.querySelector(
          '[data-slot="context-menu-content"], [data-slot="search-dialog"], [role="dialog"]',
        )
      ) {
        if (e.key === 'Escape' && searchOpen) {
          e.preventDefault()
          setSearchOpen(false)
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
  }, [searchOpen])

  return (
    <div className="flex h-full flex-col bg-canvas">
      <Topbar onSearch={() => setSearchOpen(true)} />
      <Crumbs />
      <div className="relative min-h-0 flex-1">
        <Outlet />
      </div>
      <div className="pointer-events-none absolute bottom-2 left-4 text-[10px] text-faint">
        click any box to open it · right-click for actions · esc goes back · ⌘K searches
      </div>
      {toast && (
        <div
          key={toast.id}
          role="status"
          aria-live="polite"
          className={`toast-in pointer-events-none fixed inset-x-0 bottom-9 z-[70] mx-auto w-fit rounded-[5px] border px-3 py-1.5 text-[11px] shadow-[0_12px_32px_-12px_rgba(0,0,0,0.9)] ${
            toast.tone === 'bad' ? 'border-bad/45 bg-bad/10 text-bad' : 'border-line bg-panel2 text-ink'
          }`}
        >
          {toast.text}
        </div>
      )}
      <SearchDialog data={data} open={searchOpen} onOpenChange={setSearchOpen} />
    </div>
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
      <div className="flex h-full items-center justify-center p-8 text-[12px] text-bad">
        {error} — run the generator with <code className="mx-1 text-ink">-json out/synoptic.json</code>
      </div>
    )
  }
  if (!data) {
    return <div className="flex h-full items-center justify-center text-[12px] text-faint">reading the module…</div>
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
}: {
  title: string
  badge?: string
  meta?: string
  doc?: string
  note?: string
  tags?: string[]
  onSource?: () => void
  sourceLabel?: string
}) {
  return (
    <div className="flex flex-wrap items-start gap-x-4 gap-y-1 border-b border-line bg-panel/40 px-4 py-2">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate text-[14px] text-ink">{title}</span>
          {badge && <Badge variant="accent">{badge}</Badge>}
          {(tags ?? []).map((t) => (
            <Badge key={t} variant={t === 'db' ? 'warn' : t === 'cache' ? 'info' : 'default'}>
              {t === 'db' ? 'database' : t === 'cache' ? 'cache' : t === 'net' ? 'outbound' : t}
            </Badge>
          ))}
        </div>
        {meta && <div className="mt-0.5 truncate text-[10.5px] text-faint">{meta}</div>}
        {doc && <p className="mt-1 line-clamp-2 text-[11.5px] leading-[1.5] text-ink/80">{doc}</p>}
        {note && <p className="mt-1 text-[11px] italic leading-[1.5] text-faint">{note}</p>}
      </div>
      {onSource && (
        <Button variant="outline" size="sm" onClick={onSource}>
          <Code2 className="size-3" /> {sourceLabel ?? 'source'}
        </Button>
      )}
    </div>
  )
}

function GraphPage({ scene, card, children }: { scene: Scene; card: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="flex h-full flex-col">
      {card}
      <div className="relative min-h-0 flex-1">
        <SceneCanvas scene={scene} />
      </div>
      {children && (
        <div className="max-h-[44vh] shrink-0 overflow-auto border-t border-line bg-panel/30">{children}</div>
      )}
    </div>
  )
}

function CallsList({ calls }: { calls?: Call[] }) {
  if (!calls || calls.length === 0) return null
  return (
    <div className="mt-4">
      <div className="text-[9.5px] uppercase tracking-[0.14em] text-faint">calls in this repo</div>
      <div className="mt-1.5 flex flex-col gap-1">
        {calls.map((c, i) => (
          <div key={i} className="flex items-baseline gap-2 text-[11.5px]">
            <span className="text-ink">{c.e}</span>
            {c.f && (
              <button
                className="truncate text-[10px] text-faint hover:text-acc"
                onClick={() => goto(`/f?k=${encodeURIComponent(c.k ?? '')}`)}
                disabled={!c.k}
              >
                {c.f}
                {c.l ? `:${c.l}` : ''}
              </button>
            )}
            {c.d && <span className="truncate text-[10px] text-faint">{c.d}</span>}
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
      {doc && <p className="text-[12px] leading-[1.6] text-ink/85">{doc}</p>}
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
          <div className="mb-1.5 text-[9.5px] uppercase tracking-[0.14em] text-faint">source</div>
          <GoSource code={src} focus={focus} focusLines={focusLines} />
        </div>
      ) : (
        <p className="mt-4 text-[11px] italic text-faint">
          no body in this repo — the call is wired here, the code lives in {stage?.x ?? 'another module'}
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

function MessageCard({ m, event, onOpen }: { m: StreamMessage; event?: string; onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      className="block w-full rounded-[4px] border border-line bg-panel px-2 py-1.5 text-left transition-colors hover:border-line2 hover:bg-panel2"
    >
      <div className="flex flex-wrap items-baseline gap-x-2">
        <code className="text-[11px] leading-tight text-ink">{m.ev || m.n || frameName(m)}</code>
        {m.t && <span className="break-all text-[9px] text-faint">{m.t}</span>}
      </div>
      <div className="mt-0.5 break-words text-[9.5px] text-mut">
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
    <Sheet open={open} onOpenChange={onOpenChange} title={message ? frameName(message) : 'frame'} subtitle={event}>
      {message && (
        <div>
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge variant={message.d === 'recv' ? 'info' : 'accent'}>{message.d === 'recv' ? 'received' : 'sent'}</Badge>
            {message.t && <Badge>{message.t}</Badge>}
            {message.file && (
              <span className="ml-auto text-[10px] text-faint">
                {message.file}:{message.l ?? 0}
              </span>
            )}
          </div>
          {message.n && message.n !== frameName(message) && (
            <p className="mt-2 text-[10px] text-faint">name {message.n}</p>
          )}
          <div className="mt-4">
            <div className="mb-1.5 text-[9.5px] uppercase tracking-[0.14em] text-faint">fields</div>
            {message.f?.length ? (
              <div className="divide-y divide-line/40 border-y border-line/40">
                {message.f.map((f) => (
                  <div key={f.n} className="flex flex-wrap items-baseline gap-x-2 py-1.5">
                    <code className="text-[11px] text-ink">{f.n}</code>
                    {f.t && <span className="text-[10px] text-mut">{f.t}</span>}
                    {f.tg && <span className="text-[9.5px] text-faint">{f.tg}</span>}
                    {f.rq && <span className="text-[9px] uppercase tracking-[0.1em] text-warn">required</span>}
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-[11px] italic text-faint">no fields resolved — only the source expression is known.</p>
            )}
          </div>
          {message.e && (
            <div className="mt-4">
              <div className="mb-1 text-[9.5px] uppercase tracking-[0.14em] text-faint">expression</div>
              <code className="block break-words text-[10.5px] text-mut">{message.e}</code>
            </div>
          )}
          {message.ex && (
            <pre className="mt-3 overflow-x-auto rounded-[4px] bg-canvas/70 p-2 text-[10px] leading-relaxed text-mut">
              {message.ex}
            </pre>
          )}
        </div>
      )}
    </Sheet>
  )
}

type ExtCall = { g: string; n: string; fk?: string; f?: string; l?: number; ev?: string }

function ExternalCalls({ items, onFn }: { items: ExtCall[]; onFn?: (key: string) => void }) {
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
            <span className="text-[10px] text-faint">{group.items.length}</span>
          </div>
          <div className="divide-y divide-line/40 border-y border-line/40">
            {group.items.map((x, i) => (
              <div key={`${x.g}:${x.n}:${x.f ?? ''}:${x.l ?? 0}:${i}`} className="py-1.5">
                {x.fk && onFn ? (
                  <button
                    type="button"
                    onClick={() => onFn(x.fk!)}
                    className="block break-words text-left font-mono text-[10.5px] leading-relaxed text-acc hover:underline"
                  >
                    {x.n}
                  </button>
                ) : (
                  <code className="block break-words font-mono text-[10.5px] leading-relaxed text-ink">{x.n}</code>
                )}
                {x.f && (
                  <div className="mt-0.5 break-all font-mono text-[9px] text-faint">
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
  onFn?: (key: string) => void
  limit?: number
}) {
  const list = steps.slice(0, depth === 0 ? limit : 2)
  return (
    <div className={depth === 0 ? 'space-y-1' : 'mt-1 space-y-1 border-l border-line/60 pl-3'}>
      {list.map((s, i) => {
        const branch = s.b?.length || s.e?.length
        return (
          <div key={`${depth}:${i}:${s.l ?? 0}:${s.t}`}>
            <div className="flex items-start gap-2">
              <span className="mt-[1px] w-3 shrink-0 text-right font-mono text-[9px] text-faint">
                {depth === 0 ? i + 1 : ''}
              </span>
              <span className="mt-[1px] w-7 shrink-0 text-[8px] uppercase tracking-[0.08em] text-faint">
                {s.k === 'ret' ? 'return' : s.k}
              </span>
              <div className="min-w-0">
                <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                  {s.fk && onFn ? (
                    <button
                      type="button"
                      onClick={() => onFn(s.fk!)}
                      className={`break-words text-left font-mono text-[10.5px] leading-relaxed text-acc hover:underline`}
                    >
                      {s.t}
                    </button>
                  ) : (
                    <div
                      className={`break-words font-mono text-[10.5px] leading-relaxed ${
                        s.k === 'ret' || s.k === 'exit' ? 'text-warn' : 'text-ink'
                      }`}
                    >
                      {s.t}
                    </div>
                  )}
                  {(s.k === 'ret' || s.k === 'exit') && s.rk ? (
                    <span
                      className={`shrink-0 rounded border px-1 text-[8px] uppercase tracking-[0.08em] ${
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
                  <div className="break-words font-mono text-[9px] text-faint">{s.cd || s.s}</div>
                )}
                {s.l ? <div className="font-mono text-[8.5px] text-faint">:{s.l}</div> : null}
              </div>
            </div>
            {branch && s.b?.length ? <TraceRows steps={s.b} depth={depth + 1} onFn={onFn} /> : null}
            {branch && s.e?.length ? (
              <>
                <div className="mt-1 pl-5 text-[9px] uppercase tracking-[0.12em] text-faint">else</div>
                <TraceRows steps={s.e} depth={depth + 1} onFn={onFn} />
              </>
            ) : null}
          </div>
        )
      })}
      {steps.length > list.length && depth === 0 ? (
        <div className="pl-5 text-[9.5px] italic text-faint">
          +{steps.length - list.length} more steps in the full flow
        </div>
      ) : null}
    </div>
  )
}

function EffectRows({ items, onFn }: { items: Effect[]; onFn?: (key: string) => void }) {
  return (
    <div className="divide-y divide-line/40 border-y border-line/40">
      {items.map((x, i) => (
        <div key={`${x.k}:${x.n}:${x.l ?? 0}:${i}`} className="flex items-start gap-2 py-1.5">
          <Badge variant={x.k === 'publish' ? 'accent' : x.k === 'write' ? 'warn' : 'info'}>{x.k}</Badge>
          <div className="min-w-0 flex-1">
            {x.fk && onFn ? (
              <button
                type="button"
                onClick={() => onFn(x.fk!)}
                className="break-words text-left font-mono text-[10.5px] text-acc hover:underline"
              >
                {x.n}
              </button>
            ) : (
              <code className="break-words font-mono text-[10.5px] text-ink">{x.n}</code>
            )}
            {x.s && x.s !== x.n ? (
              <div className="break-words font-mono text-[9px] text-faint">{x.s}</div>
            ) : null}
          </div>
          {x.l ? <span className="font-mono text-[8.5px] text-faint">:{x.l}</span> : null}
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
  onFn?: (key: string) => void
}) {
  const origins = event?.or ?? []
  if (!origins.length) {
    return isSse ? (
      <div className="mt-4">
        <div className="mb-1.5 text-[9.5px] uppercase tracking-[0.14em] text-faint">triggered by</div>
        <p className="text-[10.5px] italic text-faint">no in-repo publisher resolved for this event.</p>
      </div>
    ) : null
  }
  return (
    <div className="mt-4">
      <div className="mb-1.5 text-[9.5px] uppercase tracking-[0.14em] text-faint">triggered by</div>
      <div className="divide-y divide-line/40 border-y border-line/40">
        {origins.map((o, i) => (
          <div key={`${o.n}:${o.f ?? ''}:${o.l ?? 0}:${i}`} className="flex items-start gap-2 py-1.5">
            <Badge variant={o.k === 'route' ? 'info' : 'plain'}>{o.k ?? 'background'}</Badge>
            <div className="min-w-0 flex-1">
              {o.k === 'route' && o.p ? (
                <button
                  type="button"
                  onClick={() => goto(routeHref(o.ri ?? 0))}
                  className="break-words text-left font-mono text-[10.5px] text-acc hover:underline"
                >
                  {o.m} {o.p}
                </button>
              ) : o.fk && onFn ? (
                <button
                  type="button"
                  onClick={() => onFn(o.fk!)}
                  className="break-words text-left font-mono text-[10.5px] text-acc hover:underline"
                >
                  {o.n}
                </button>
              ) : (
                <div className="break-words font-mono text-[10.5px] text-ink">{o.n}</div>
              )}
              {o.k === 'route' && o.fk && onFn ? (
                <button
                  type="button"
                  onClick={() => onFn(o.fk!)}
                  className="mt-0.5 font-mono text-[9px] text-acc hover:underline"
                >
                  method ›
                </button>
              ) : null}
              {o.via ? <div className="text-[9px] text-faint">via {o.via}</div> : null}
              {o.f ? (
                <div className="break-all font-mono text-[9px] text-faint">
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

function StreamBoard({ route, flows, ri }: { route: Route; flows?: Record<string, FuncFlow>; ri: number }) {
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
  const recv = st?.recv ?? []
  const send = st?.send ?? []
  const ext = externalCalls(route, flows)
  const known = useMemo(() => {
    const names = [
      ...new Set(
        [...(st?.events ?? []), ...recv.map((m) => m.ev ?? ''), ...send.map((m) => m.ev ?? '')].filter(Boolean),
      ),
    ]
    return names.length || !isSse ? names : ['event stream']
  }, [isSse, st, recv, send])
  const eventOf = (m: StreamMessage) => m.ev || (m.n && known.includes(m.n) ? m.n : '')
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
    [isSse, known, recv, send],
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
  const defaultKey = exchanges[0]?.event ?? (preamble.length ? '__preamble' : unmapped.length ? '__unmapped' : '')
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
  const firstFrame = exchange ? [...exchange.inbound, ...exchange.outbound].find((m) => m.file) : undefined
  const handlerStage = route.ch[route.ch.length - 1]
  const handlerHref = flowKey
    ? fnHref(flowKey, { ri, si: route.ch.length - 1 })
    : handlerStage?.fk
      ? fnHref(handlerStage.fk, { ri, si: route.ch.length - 1 })
      : undefined
  const openFn = (key: string) => goto(fnHref(key, { ri, si: Math.max(0, route.ch.length - 1) }))
  const frameCount = exchange ? exchange.inbound.length + exchange.outbound.length : active === '__preamble' ? preamble.length : unmapped.length

  const listRows = (
    <>
      {preamble.length > 0 && (
        <button
          type="button"
          onClick={() => setSelected('__preamble')}
          className={`block w-full border-l-2 px-3 py-2 text-left transition-colors ${
            active === '__preamble' ? 'border-acc bg-acc/10' : 'border-transparent hover:bg-panel2/60'
          }`}
        >
          <div className="font-mono text-[11px] text-ink">preamble</div>
          <div className="mt-0.5 text-[9.5px] text-faint">{preamble.length} frame{preamble.length === 1 ? '' : 's'} before the switch</div>
        </button>
      )}
      {preamble.length > 0 && <div className="border-t border-line/50" />}
      {filtered.map((x) => {
        const calls = ext.filter((c) => c.ev === x.event).length
        return (
          <button
            key={x.event}
            type="button"
            onClick={() => setSelected(x.event)}
            className={`block w-full border-l-2 px-3 py-2 text-left transition-colors ${
              active === x.event ? 'border-acc bg-acc/10' : 'border-transparent hover:bg-panel2/60'
            }`}
          >
            <div className="break-words font-mono text-[11px] text-ink">{x.event}</div>
            {(!isSse || calls > 0) && (
              <div className="mt-0.5 text-[9.5px] text-faint">
                {isSse ? '' : `${x.inbound.length} in · ${x.outbound.length} out`}
                {calls > 0 ? `${isSse ? '' : ' · '}${calls} call${calls === 1 ? '' : 's'}` : ''}
              </div>
            )}
          </button>
        )
      })}
      {unmapped.length > 0 && <div className="border-t border-line/50" />}
      {unmapped.length > 0 && (
        <button
          type="button"
          onClick={() => setSelected('__unmapped')}
          className={`block w-full border-l-2 px-3 py-2 text-left transition-colors ${
            active === '__unmapped' ? 'border-acc bg-acc/10' : 'border-transparent hover:bg-panel2/60'
          }`}
        >
          <div className="font-mono text-[11px] text-ink">unmapped sends</div>
          <div className="mt-0.5 text-[9.5px] text-faint">{unmapped.length} frame{unmapped.length === 1 ? '' : 's'} outside an event</div>
        </button>
      )}
      {!filtered.length && q && <p className="px-3 py-6 text-center text-[10.5px] italic text-faint">no event matches “{query}”.</p>}
      {!exchanges.length && !preamble.length && !unmapped.length && (
        <p className="px-3 py-6 text-[10.5px] italic text-faint">no event dispatch resolved.</p>
      )}
    </>
  )

  return (
    <div className="stream-board h-full">
      <aside className="stream-list border-r border-line bg-panel/20">
        <div className="sticky top-0 z-10 border-b border-line bg-[var(--color-panel)] p-2">
          <div className="mb-2 flex items-baseline gap-2">
            <span className="text-[9.5px] uppercase tracking-[0.14em] text-faint">
              {exchanges.length} events
            </span>
            <span className="ml-auto text-[9.5px] text-faint">
              {isSse ? `${send.length} payload${send.length === 1 ? '' : 's'}` : `${recv.length} in · ${send.length} out`}
            </span>
          </div>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="filter event, type or field"
            className="w-full rounded-[4px] border border-line bg-canvas px-2 py-1 text-[11px] text-ink outline-none placeholder:text-faint focus:border-line2"
          />
        </div>
        <div>{listRows}</div>
      </aside>

      <section className="stream-detail min-w-0">
        {active ? (
          <div className="mx-auto max-w-[760px] p-4">
            <div className="flex flex-wrap items-start gap-3 border-b border-line pb-3">
              <div className="min-w-0 flex-1">
                <div className="text-[9px] uppercase tracking-[0.14em] text-faint">
                  {active.startsWith('__') ? 'outside the event switch' : 'selected event'}
                </div>
                <div className="mt-0.5 break-words font-mono text-[18px] leading-tight text-ink">
                  {active === '__preamble' ? 'preamble' : active === '__unmapped' ? 'unmapped sends' : active}
                </div>
                <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[10px] text-faint">
                  <span>{frameCount} frame{frameCount === 1 ? '' : 's'}</span>
                  {exchange && !isSse && <span>{exchange.inbound.length} client · {exchange.outbound.length} server</span>}
                  {eventCalls.length > 0 && <span>{eventCalls.length} external call{eventCalls.length === 1 ? '' : 's'}</span>}
                  {firstFrame?.file && <span className="font-mono">{firstFrame.file}:{firstFrame.l ?? 0}</span>}
                </div>
              </div>
              {handlerHref && (
                <Button variant="outline" size="sm" onClick={() => goto(handlerHref)}>
                  <Code2 className="size-3" /> handler
                </Button>
              )}
            </div>

            {active === '__preamble' ? (
              <div className="mt-4">
                <div className="mb-1.5 text-[9.5px] uppercase tracking-[0.14em] text-faint">received before the switch</div>
                <div className="space-y-1">
                  {preamble.map((m, i) => (
                    <MessageCard key={`pre:${i}`} m={m} onOpen={() => setPicked({ m })} />
                  ))}
                </div>
              </div>
            ) : active === '__unmapped' ? (
              <div className="mt-4">
                <div className="mb-1.5 text-[9.5px] uppercase tracking-[0.14em] text-faint">sent outside a resolved event</div>
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
                    <div className="mb-1.5 text-[9.5px] uppercase tracking-[0.14em] text-faint">client sends</div>
                    {exchange.inbound.length ? (
                      <div className="space-y-1">
                        {exchange.inbound.map((m, i) => (
                          <MessageCard key={`in:${i}`} m={m} event={exchange.event} onOpen={() => setPicked({ m, event: exchange.event })} />
                        ))}
                      </div>
                    ) : (
                      <p className="rounded-[4px] border border-dashed border-line px-3 py-2 text-[10.5px] text-faint">
                        server-initiated · no client frame for this event
                      </p>
                    )}
                  </div>
                )}

                {eventFlow?.flow?.length ? (
                  <div className="mt-4">
                    <div className="mb-1.5 flex items-center gap-2 text-[9.5px] uppercase tracking-[0.14em] text-faint">
                      <span>processing</span>
                      {sharedFlow ? (
                        <span className="normal-case tracking-normal text-[9px] text-faint">shared stream loop</span>
                      ) : null}
                      {flowKey ? (
                        <button
                          type="button"
                          onClick={() => openFn(flowKey)}
                          className="ml-auto normal-case tracking-normal text-[9.5px] text-acc hover:underline"
                        >
                          full flow ›
                        </button>
                      ) : null}
                    </div>
                    <TraceRows steps={eventFlow.flow} onFn={openFn} limit={sharedFlow ? 9 : 3} />
                  </div>
                ) : null}

                <div className="my-4 flex items-center gap-2 text-[9.5px] uppercase tracking-[0.14em] text-faint">
                  <span className="h-px flex-1 bg-line" />
                  {isSse ? 'handler writes' : 'handler dispatches'}
                  <span className="h-px flex-1 bg-line" />
                </div>

                <div>
                  <div className="mb-1.5 text-[9.5px] uppercase tracking-[0.14em] text-faint">
                    {isSse ? 'payload sent' : 'server replies'}
                  </div>
                  {exchange.outbound.length ? (
                    <div className="space-y-1">
                      {exchange.outbound.map((m, i) => (
                        <MessageCard key={`out:${i}`} m={m} event={exchange.event} onOpen={() => setPicked({ m, event: exchange.event })} />
                      ))}
                    </div>
                  ) : (
                    <p className="rounded-[4px] border border-dashed border-line px-3 py-2 text-[10.5px] text-faint">no reply resolved.</p>
                  )}
                  {isSse && exchange.outbound.length > 0 && exchange.outbound.every((m) => !m.ev) && (
                    <p className="mt-1.5 text-[10px] text-faint">this payload shape is sent for every event.</p>
                  )}
                </div>

                {effects.length > 0 && (
                  <div className="mt-6">
                    <div className="mb-2 text-[9.5px] uppercase tracking-[0.14em] text-faint">
                      after effects
                    </div>
                    <EffectRows items={effects} onFn={openFn} />
                  </div>
                )}
              </>
            ) : null}

            <div className="mt-6 border-t border-line pt-3">
              <div className="mb-2 text-[9.5px] uppercase tracking-[0.14em] text-faint">deeper calls</div>
              {deeperCalls.length ? (
                <ExternalCalls items={deeperCalls} onFn={openFn} />
              ) : (
                <p className="text-[10.5px] italic text-faint">no deeper database, cache or outbound call is tied to this event.</p>
              )}
              {routeCalls.length > 0 && (
                <details className="mt-3 border-t border-line/50 pt-2">
                  <summary className="cursor-pointer text-[10px] text-faint hover:text-mut">
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
          <p className="p-6 text-[11px] italic text-faint">no event dispatch resolved — open the flow to follow the handler.</p>
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
  const params = useParams({ strict: false }) as { _splat?: string }
  const focus = params._splat ?? ''
  const [proto, setProto] = useState<'all' | 'http' | 'ws' | 'sse'>('all')
  const scene = useMemo(() => buildRouterScene(data, focus, proto), [data, focus, proto])
  const label = focus ? '/' + focus : data.repo
  const count = data.routes.filter(
    (r) =>
      (proto === 'all' ? true : proto === 'http' ? !r.pr : r.pr === proto) &&
      (focus ? r.p.startsWith('/' + focus + '/') : true),
  ).length

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
          <div className="flex items-center gap-2 border-b border-line bg-panel/20 px-4 py-1.5">
            <span className="text-[9.5px] uppercase tracking-[0.14em] text-faint">protocol</span>
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
            <span className="ml-auto text-[10px] text-faint">
              {count} {count === 1 ? 'route' : 'routes'}
            </span>
          </div>
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
    () => (r ? buildPipelineScene(r, ri, over, data.flows) : { nodes: [], edges: [], key: "" }),
    [r, ri, over],
  )
  const [sheet, setSheet] = useState<number | null>(null)
  const [streamView, setStreamView] = useState<'exchanges' | 'canvas'>('exchanges')

  useEffect(() => {
    setStreamView('exchanges')
  }, [ri])

  if (!r) return <div className="p-6 text-[12px] text-faint">no such route</div>

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
  const note =
    over
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
        <div className="flex flex-wrap items-center gap-2 border-b border-line bg-panel/30 px-3 py-1.5">
          <Segmented
            value={streamView}
            options={[
              { value: 'exchanges', label: 'events' },
              { value: 'canvas', label: 'flow' },
            ]}
            onChange={(v) => setStreamView(v as 'exchanges' | 'canvas')}
          />
          <span className="text-[10px] text-faint">pick an event to read its request, replies and calls</span>
        </div>
        <div className="relative min-h-0 flex-1">
          {streamView === 'exchanges' ? (
            <StreamBoard route={r} flows={data.flows} ri={ri} />
          ) : (
            <SceneCanvas scene={scene} />
          )}
        </div>
        <div className="max-h-[26vh] shrink-0 overflow-auto border-t border-line bg-panel/30">
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
  const nextStage = r && si + 1 < r.ch.length ? { label: r.ch[si + 1].n, href: '/r/' + ri + '/s/' + (si + 1) } : undefined
  const prevStage = r && si > 0 ? { label: r.ch[si - 1].n, href: '/r/' + ri + '/s/' + (si - 1) } : undefined
  const scene = useMemo(
    () => (flow ? buildFlowScene(flow, key, nextStage, prevStage, st?.in, { ri, si }) : { nodes: [], edges: [], key: "" }),
    [flow, key, nextStage?.label, nextStage?.href, prevStage?.label, prevStage?.href, st?.in, ri, si],
  )
  const [sheet, setSheet] = useState(false)
  const [focus, setFocus] = useState<{ line?: number; lines?: number[] }>({})
  useEffect(() => {
    const onSrc = (e: Event) => {
      const detail = (e as CustomEvent<{ line: number; startLine?: number; lines?: number[] }>).detail
      const offset = (detail.startLine ?? 1) - 1
      const lines = detail.lines?.map((l) => l - offset)
      setFocus({ line: lines?.[0] ?? detail.line - offset, lines })
      setSheet(true)
    }
    window.addEventListener('synoptic:source', onSrc)
    return () => window.removeEventListener('synoptic:source', onSrc)
  }, [])

  if (!r || !st) return <div className="p-6 text-[12px] text-faint">no such step</div>

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
  const search = useSearch({ strict: false }) as { k?: string; ri?: string; si?: string }
  const key = search.k ?? ''
  const flow: FuncFlow | undefined = flowOf(data, key)
  const parentRi = Number(search.ri)
  const parentSi = Number(search.si)
  const parentOk = search.ri !== undefined && search.si !== undefined && Number.isInteger(parentRi) && Number.isInteger(parentSi)
  const origin = parentOk ? { ri: parentRi, si: parentSi } : undefined
  const parent = parentOk ? data.routes[parentRi] : undefined
  const nextStage = parent && parentSi + 1 < parent.ch.length
    ? { label: parent.ch[parentSi + 1].n, href: `/r/${parentRi}/s/${parentSi + 1}` }
    : undefined
  const prevStage = parent && parentSi > 0
    ? { label: parent.ch[parentSi - 1].n, href: `/r/${parentRi}/s/${parentSi - 1}` }
    : undefined
  const scene = useMemo(
    () => (flow ? buildFlowScene(flow, key, nextStage, prevStage, flow.in, origin) : { nodes: [], edges: [], key: "" }),
    [flow, key, flow?.in, nextStage?.label, nextStage?.href, prevStage?.label, prevStage?.href, parentOk, parentRi, parentSi],
  )
  const [sheet, setSheet] = useState(false)
  const [focus, setFocus] = useState<{ line?: number; lines?: number[] }>({})
  useEffect(() => {
    const onSrc = (e: Event) => {
      const detail = (e as CustomEvent<{ line: number; startLine?: number; lines?: number[] }>).detail
      const offset = (detail.startLine ?? 1) - 1
      const lines = detail.lines?.map((l) => l - offset)
      setFocus({ line: lines?.[0] ?? detail.line - offset, lines })
      setSheet(true)
    }
    window.addEventListener('synoptic:source', onSrc)
    return () => window.removeEventListener('synoptic:source', onSrc)
  }, [])

  if (!flow) return <div className="p-6 text-[12px] text-faint">this function is not in the module</div>

  return (
    <GraphPage
      scene={scene}
      card={
      <ScopeCard
        title={flow.n}
        badge="func"
        meta={flow.f ? `${flow.f}:${flow.l}${parent ? ` · ${parent.m} ${parent.p} · stage ${parentSi + 1}` : ''}` : key}
        doc={flow.d}
        note={!flow.d ? 'no doc comment on this one — the flow below is what it does' : undefined}
        onSource={() => {
          setFocus({})
          setSheet(true)
        }}
        sourceLabel="source"
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
  const search = useSearch({ strict: false }) as { k?: string; i?: string; ri?: string; si?: string }
  const key = search.k ?? ''
  const t: SynType | undefined = data.types?.[key]
  if (!t) return <div className="p-6 text-[12px] text-faint">this type is not in the module</div>
  const iface = search.i ? data.types?.[search.i] : undefined
  const ri = Number(search.ri ?? '')
  const si = Number(search.si ?? '')
  const origin = Number.isFinite(ri) ? `&ri=${ri}${Number.isFinite(si) ? `&si=${si}` : ''}` : ''
  const openMethod = (mk?: string) => {
    if (!mk) return
    const sk = t.k === 'struct' ? t.key : undefined
    const ik = t.k === 'interface' ? t.key : iface?.key
    const q = [`k=${encodeURIComponent(mk)}`]
    if (ik) q.push(`i=${encodeURIComponent(ik)}`)
    if (sk) q.push(`t=${encodeURIComponent(sk)}`)
    goto(`/f?${q.join('&')}${origin}`)
  }
  // resolve a field's written type back to a catalog entry, so fields with a
  // struct or interface type can be opened like methods
  const fieldType = (raw: string): SynType | undefined => {
    const norm = raw.replace(/^\*+/, '').trim()
    const base = norm.match(/([A-Za-z_]\w*)$/)?.[1]
    if (!base) return undefined
    const qual = norm.includes('.') ? norm.slice(0, norm.lastIndexOf('.')).split('.').pop() : ''
    if (qual) {
      return Object.values(data.types ?? {}).find((x) => x.n === base && (x.pkg === qual || x.pkg.endsWith('/' + qual)))
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
        note={t.k === 'interface' ? 'interface · implemented by the structs below' : 'struct · fields and methods'}
      />
      <div className="grid gap-3 p-4 lg:grid-cols-2">
        {t.k === 'struct' && (
          <section className="rounded-[6px] border border-line bg-panel/40">
            <div className="border-b border-line px-3 py-1.5 text-[10px] uppercase tracking-[0.14em] text-mut">fields</div>
            <div className="divide-y divide-line/60">
              {(t.fields ?? []).length === 0 && <div className="px-3 py-2 text-[11px] text-faint">no fields</div>}
              {(t.fields ?? []).map((f) => {
                const ft = fieldType(f.t)
                return (
                  <button
                    key={f.n + f.t}
                    disabled={!ft}
                    onClick={() => ft && goto(`/t?k=${encodeURIComponent(ft.key)}${origin}`)}
                    className={`flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px] ${ft ? 'hover:bg-panel2' : 'cursor-default'}`}
                  >
                    <span className="text-ink">{f.n}</span>
                    <span className="truncate text-info">{f.t}</span>
                    {f.e && <span className="text-[9px] uppercase tracking-[0.1em] text-faint">embedded</span>}
                    {ft && <span className="ml-auto shrink-0 text-[9px] text-faint">open ›</span>}
                  </button>
                )
              })}
            </div>
          </section>
        )}
        <section className="rounded-[6px] border border-line bg-panel/40">
          <div className="border-b border-line px-3 py-1.5 text-[10px] uppercase tracking-[0.14em] text-mut">methods</div>
          <div className="divide-y divide-line/60">
            {(t.methods ?? []).length === 0 && <div className="px-3 py-2 text-[11px] text-faint">no methods</div>}
            {(t.methods ?? []).map((m) => (
              <button
                key={m.n + (m.k ?? '')}
                disabled={!m.k}
                onClick={() => openMethod(m.k)}
                className={`flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px] ${m.k ? 'hover:bg-panel2' : 'cursor-default'}`}
              >
                <span className="text-ink">{m.n}</span>
                <span className="min-w-0 flex-1 truncate text-mut">{m.sig}</span>
                {m.k && <span className="shrink-0 text-[9px] text-faint">open ›</span>}
              </button>
            ))}
          </div>
        </section>
        {t.k === 'interface' && (
          <section className="rounded-[6px] border border-line bg-panel/40">
            <div className="border-b border-line px-3 py-1.5 text-[10px] uppercase tracking-[0.14em] text-mut">implemented by</div>
            <div className="divide-y divide-line/60">
              {(t.impls ?? []).length === 0 && <div className="px-3 py-2 text-[11px] text-faint">no in-module implementation</div>}
              {(t.impls ?? []).map((ik) => {
                const it = data.types?.[ik]
                if (!it) return null
                return (
                  <button key={ik} onClick={() => goto(`/t?k=${encodeURIComponent(ik)}&i=${encodeURIComponent(t.key)}${origin}`)}
                    className="flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px] hover:bg-panel2">
                    <span className="text-ink">{it.n}</span>
                    <span className="min-w-0 flex-1 truncate text-mut">{it.pkg}</span>
                    <span className="text-[9px] text-faint">open ›</span>
                  </button>
                )
              })}
            </div>
          </section>
        )}
        {t.k === 'struct' && (t.impls ?? []).length > 0 && (
          <section className="rounded-[6px] border border-line bg-panel/40">
            <div className="border-b border-line px-3 py-1.5 text-[10px] uppercase tracking-[0.14em] text-mut">implements</div>
            <div className="divide-y divide-line/60">
              {(t.impls ?? []).map((ik) => {
                const it = data.types?.[ik]
                if (!it) return null
                return (
                  <button key={ik} onClick={() => goto(`/t?k=${encodeURIComponent(ik)}${origin}`)}
                    className="flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px] hover:bg-panel2">
                    <span className="text-ink">{it.n}</span>
                    <span className="min-w-0 flex-1 truncate text-mut">{it.pkg}</span>
                  </button>
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

const emptyScene: Scene = { nodes: [], edges: [], key: "" }

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
      <div className="text-[9.5px] uppercase tracking-[0.14em] text-faint">{label}</div>
      <div className="mt-0.5 truncate text-[11.5px] text-ink" title={value}>{value}</div>
    </div>
  )
}

function RPCView() {
  const data = useData()
  const services = data.services ?? []
  if (services.length === 0) {
    return <div className="p-6 text-[12px] text-faint">no gRPC services were found in this module</div>
  }
  return (
    <div className="h-full overflow-auto p-4">
      <div className="mb-3 flex items-baseline gap-2">
        <span className="text-[13px] text-ink">RPC</span>
        <span className="text-[10.5px] text-faint">
          {services.length} services · {services.reduce((n, s) => n + s.methods.length, 0)} methods
        </span>
      </div>
      <div className="grid gap-3 lg:grid-cols-2">
        {services.map((s, si) => (
          <section key={s.n} className="overflow-hidden rounded-[6px] border border-line bg-panel/30">
            <header className="border-b border-line px-3 py-2">
              <div className="flex items-baseline gap-2">
                <span className="truncate text-[12.5px] text-ink">{s.n}</span>
                {s.p && <span className="text-[10px] text-faint">{s.p}</span>}
                <span className="ml-auto shrink-0 text-[10px] text-faint">{s.methods.length} methods</span>
              </div>
              {s.f && <div className="mt-0.5 text-[10px] text-faint">{s.f}{s.l ? `:${s.l}` : ''}</div>}
              {(s.mw ?? []).length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1">
                  {s.mw!.map((mw) => (
                    <Badge key={mw.n} variant="plain">{mw.n}</Badge>
                  ))}
                </div>
              )}
            </header>
            <div className="divide-y divide-line/60">
              {s.methods.map((m, mi) => (
                <button
                  key={m.n}
                  onClick={() => goto(`/rpc/${si}/${mi}`)}
                  className="flex w-full flex-col gap-1 px-3 py-2 text-left hover:bg-panel2"
                >
                  <div className="flex items-baseline gap-2">
                    <Badge variant={kindTone(m.k)}>{m.k}</Badge>
                    <span className="truncate text-[11.5px] text-ink">{m.n}</span>
                    <span className="ml-auto shrink-0 text-[9.5px] text-faint">{m.p}</span>
                  </div>
                  <div className="flex min-w-0 items-baseline gap-1.5 text-[10.5px] text-mut">
                    <span className="truncate">{m.rq || 'request'}</span>
                    <span className="text-faint">→</span>
                    <span className="truncate">{m.rs || 'response'}</span>
                  </div>
                  {m.d && <div className="line-clamp-2 text-[10.5px] text-mut">{m.d}</div>}
                </button>
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
  const scene = useMemo(
    () => (flow ? buildFlowScene(flow, key, undefined, undefined, flow.in, undefined) : emptyScene),
    [flow, key, flow?.in],
  )
  const [sheet, setSheet] = useState(false)
  const [focus, setFocus] = useState<{ line?: number; lines?: number[] }>({})
  useEffect(() => {
    const onSrc = (e: Event) => {
      const detail = (e as CustomEvent<{ line: number; startLine?: number; lines?: number[] }>).detail
      const offset = (detail.startLine ?? 1) - 1
      const lines = detail.lines?.map((l) => l - offset)
      setFocus({ line: lines?.[0] ?? detail.line - offset, lines })
      setSheet(true)
    }
    window.addEventListener('synoptic:source', onSrc)
    return () => window.removeEventListener('synoptic:source', onSrc)
  }, [])

  if (!svc || !method) return <div className="p-6 text-[12px] text-faint">this RPC method is not indexed</div>

  return (
    <GraphPage
      scene={scene}
      card={
        <ScopeCard
          title={`${svc.n} / ${method.n}`}
          badge={method.k}
          meta={method.f ? `${method.f}:${method.l} · ${svc.n}` : svc.n}
          doc={method.d}
          note={!method.d ? 'no doc comment on this method' : undefined}
          onSource={flow ? () => { setFocus({}); setSheet(true) } : undefined}
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
          <span className="text-[9.5px] uppercase tracking-[0.14em] text-faint">interceptors</span>
          {svc.mw!.map((mw) => <Badge key={mw.n} variant="plain">{mw.n}</Badge>)}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2 border-t border-line px-3 py-2">
        <code className="min-w-0 flex-1 truncate text-[10.5px] text-mut">{method.curl}</code>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            if (method.curl) navigator.clipboard?.writeText(method.curl)
            window.dispatchEvent(new CustomEvent('synoptic:toast', { detail: { message: 'grpcurl copied' } }))
          }}
        >
          copy
        </Button>
        {method.fk && (
          <Button variant="ghost" size="sm" onClick={() => goto(`/f?k=${encodeURIComponent(method.fk!)}`)}>
            open flow
          </Button>
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
const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: RouterView })
const groupRoute = createRoute({ getParentRoute: () => rootRoute, path: '/g/$', component: RouterView })
const routeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/r/$ri', component: PipelineView })
const stageRoute = createRoute({ getParentRoute: () => rootRoute, path: '/r/$ri/s/$si', component: FlowView })
const fnRoute = createRoute({ getParentRoute: () => rootRoute, path: '/f', component: FnView })
const typeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/t', component: TypeView })
const rpcRoute = createRoute({ getParentRoute: () => rootRoute, path: '/rpc', component: RPCView })
const rpcMethodRoute = createRoute({ getParentRoute: () => rootRoute, path: '/rpc/$si/$mi', component: RPCMethodView })

const routeTree = rootRoute.addChildren([indexRoute, groupRoute, routeRoute, stageRoute, fnRoute, typeRoute, rpcRoute, rpcMethodRoute])

const router = createRouter({ routeTree, history: createHashHistory() })

export function App() {
  return <RouterProvider router={router} />
}
