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
import { ArrowLeft, ArrowRight, Code2, Pause, Play, RotateCcw, Search } from 'lucide-react'
import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { GoSource } from './components/GoSource'
import { Badge } from './components/ui/badge'
import { Button } from './components/ui/button'
import { SearchDialog } from './components/ui/command'
import { Segmented } from './components/ui/segmented'
import { Sheet } from './components/ui/sheet'
import { SceneCanvas } from './graph/SceneCanvas'
import { buildPipelineScene, buildRouterScene, type Scene } from './graph/scenes'
import { buildFlowScene } from './graph/flowscene'
import { DocPanel, Inputs } from './panes/DocPanel'
import { flowOf, loadData, msOf, routeHref, stageHref, timingOf, touchTags } from './lib/data'
import { goto } from './lib/nav'
import type { Call, Data, FuncFlow, Stage, SynType } from './lib/types'

/* ---------- data ---------- */

const DataCtx = createContext<Data | null>(null)
const useData = () => {
  const d = useContext(DataCtx)
  if (!d) throw new Error('data not loaded')
  return d
}

/* ---------- playback ---------- */

type Mode = 'static' | 'replay' | 'trace'

type Playback = {
  step: number
  steps: number
  playing: boolean
  mode: Mode
  setSteps: (n: number) => void
  setMode: (m: Mode) => void
  play: () => void
  pause: () => void
  next: () => void
  prev: () => void
  reset: () => void
}

const PBCtx = createContext<Playback | null>(null)
const usePB = () => {
  const v = useContext(PBCtx)
  if (!v) throw new Error('playback missing')
  return v
}

function PlaybackProvider({ children }: { children: React.ReactNode }) {
  const [step, setStep] = useState(-1)
  const [steps, setSteps] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [mode, setModeState] = useState<Mode>('static')

  useEffect(() => {
    if (!playing) return
    if (step >= steps - 1) {
      setPlaying(false)
      return
    }
    const t = window.setTimeout(() => setStep((s) => s + 1), 820)
    return () => window.clearTimeout(t)
  }, [playing, step, steps])

  const value: Playback = useMemo(
    () => ({
      step,
      steps,
      playing,
      mode,
      setSteps,
      setMode: (m) => {
        setModeState(m)
        setStep(-1)
        setPlaying(m === 'replay')
      },
      play: () => {
        if (steps > 0 && step >= steps - 1) setStep(-1)
        setPlaying(true)
      },
      pause: () => setPlaying(false),
      next: () => {
        setPlaying(false)
        setStep((s) => Math.min(s + 1, steps - 1))
      },
      prev: () => {
        setPlaying(false)
        setStep((s) => Math.max(s - 1, -1))
      },
      reset: () => {
        setPlaying(false)
        setStep(-1)
      },
    }),
    [step, steps, playing, mode],
  )

  return <PBCtx.Provider value={value}>{children}</PBCtx.Provider>
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
  const pb = usePB()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const parts = pathname.split('/').filter(Boolean)
  const level = parts[0] === 'f' ? 'code' : parts[0] === 'r' ? (parts[2] === 's' ? 'step' : 'route') : 'router'
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
        <div className="flex items-center gap-1.5">
          <span className="text-[9.5px] uppercase tracking-[0.14em] text-faint">mode</span>
          <Segmented
            value={pb.mode}
            onChange={(v) => pb.setMode(v as Mode)}
            options={[
              { value: 'static', label: 'static' },
              { value: 'replay', label: 'replay' },
              { value: 'trace', label: 'trace' },
            ]}
          />
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon" onClick={pb.prev} aria-label="previous step">
            <ArrowLeft className="size-3.5" />
          </Button>
          <Button variant="ghost" size="icon" onClick={pb.playing ? pb.pause : pb.play} aria-label="play">
            {pb.playing ? <Pause className="size-3.5" /> : <Play className="size-3.5" />}
          </Button>
          <Button variant="ghost" size="icon" onClick={pb.next} aria-label="next step">
            <ArrowRight className="size-3.5" />
          </Button>
          <Button variant="ghost" size="icon" onClick={pb.reset} aria-label="reset">
            <RotateCcw className="size-3.5" />
          </Button>
          <span className="ml-1 w-[78px] text-[10px] text-mut">
            {pb.steps ? `${pb.step + 1} / ${pb.steps}` : 'no steps'}
          </span>
        </div>
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
  const pb = usePB()
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
      } else if (e.key === 'ArrowRight') {
        pb.next()
      } else if (e.key === 'ArrowLeft') {
        pb.prev()
      } else if (e.key === ' ') {
        e.preventDefault()
        pb.playing ? pb.pause() : pb.play()
      }
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [pb, searchOpen])

  return (
    <div className="flex h-full flex-col bg-canvas">
      <Topbar onSearch={() => setSearchOpen(true)} />
      <Crumbs />
      <div className="relative min-h-0 flex-1">
        <Outlet />
      </div>
      <div className="pointer-events-none absolute bottom-2 left-4 text-[10px] text-faint">
        click any box to open it · right-click for actions · ← → steps · space plays · esc goes back · ⌘K searches
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
      <PlaybackProvider>
        <Shell />
      </PlaybackProvider>
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

function useSceneSteps(scene: Scene, key: string) {
  const pb = usePB()
  const { setSteps, reset } = pb
  useEffect(() => {
    setSteps(scene.steps)
    reset()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])
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

/* ---------- views ---------- */

function RouterView() {
  const data = useData()
  const params = useParams({ strict: false }) as { _splat?: string }
  const focus = params._splat ?? ''
  const scene = useMemo(() => buildRouterScene(data, focus), [data, focus])
  const label = focus ? '/' + focus : data.repo
  const count = focus ? data.routes.filter((r) => r.p.startsWith('/' + focus + '/')).length : data.stats.routes

  return (
    <GraphPage
      scene={scene}
      card={
        <ScopeCard
          title={label}
          badge="router"
          meta={`${count} under this path · click a box to open it`}
          note="each column is one path segment. open a group to walk deeper, a route to see its middleware."
        />
      }
    />
  )
}

function PipelineView() {
  const data = useData()
  const pb = usePB()
  const params = useParams({ strict: false }) as { ri: string }
  const ri = Number(params.ri)
  const r = data.routes[ri]
  const timing = r ? timingOf(data, r) : undefined
  const over = pb.mode === 'trace' ? timing : undefined
  const scene = useMemo(
    () => (r ? buildPipelineScene(r, ri, pb.step, over, data.flows) : { nodes: [], edges: [], steps: 0, key: '' }),
    [r, ri, pb.step, over],
  )
  useSceneSteps(scene, 'r' + ri)
  const [sheet, setSheet] = useState<number | null>(null)

  if (!r) return <div className="p-6 text-[12px] text-faint">no such route</div>

  const handler = r.ch[r.ch.length - 1]
  const mw = r.ch.filter((s) => (s.r ?? '') === 'middleware').length
  const tags = [...new Set(r.ch.flatMap((s) => touchTags(s, false)))]
  const current = sheet !== null ? r.ch[sheet] : undefined
  const flow = current?.fk ? flowOf(data, current.fk) : undefined
  const slow = timing && timing.slow >= 0 ? timing.st.find((x) => x.i === timing.slow) : undefined
  const slowName = slow ? r.ch[slow.i]?.n : undefined
  const meta =
    `${r.f}:${r.l} · ${r.ch.length} steps · ${mw} middleware first` +
    (over ? ` · ${msOf(over.total)} per request · ${over.spans} spans` : '')
  const note =
    pb.mode === 'trace' && !timing
      ? 'no trace in this page — rebuild with -trace <spans.json> to see where the time goes'
      : over
        ? slow && slowName
          ? `slowest stage: ${slowName} — ${msOf(slow.m)} median, ${msOf(slow.p95)} p95, ${Math.round(slow.sh * 100)}% of the time`
          : 'the trace matched this route but not its stages'
        : handler?.d
          ? undefined
          : 'no doc comment on the handler — open it, the flow is the description'

  return (
    <GraphPage
      scene={scene}
      card={
      <ScopeCard
        title={`${r.m} ${r.p}`}
        badge="route"
        meta={meta}
        doc={over ? undefined : handler?.d}
        note={note}
        tags={tags}
        onSource={handler ? () => setSheet(r.ch.length - 1) : undefined}
        sourceLabel="handler"
      />
      }
    >
      <DocPanel r={r} />
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
    </GraphPage>
  )
}

function FlowView() {
  const data = useData()
  const pb = usePB()
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
    () => (flow ? buildFlowScene(flow, key, pb.step, nextStage, prevStage, st?.in, { ri, si }) : { nodes: [], edges: [], steps: 0, key: '' }),
    [flow, key, pb.step, nextStage?.label, nextStage?.href, prevStage?.label, prevStage?.href, st?.in, ri, si],
  )
  useSceneSteps(scene, 's' + ri + ':' + si)
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
  const pb = usePB()
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
    () => (flow ? buildFlowScene(flow, key, pb.step, nextStage, prevStage, flow.in, origin) : { nodes: [], edges: [], steps: 0, key: '' }),
    [flow, key, pb.step, flow?.in, nextStage?.label, nextStage?.href, prevStage?.label, prevStage?.href, parentOk, parentRi, parentSi],
  )
  useSceneSteps(scene, 'f' + key)
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

/* ---------- router ---------- */

const rootRoute = createRootRoute({ component: Root })
const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: RouterView })
const groupRoute = createRoute({ getParentRoute: () => rootRoute, path: '/g/$', component: RouterView })
const routeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/r/$ri', component: PipelineView })
const stageRoute = createRoute({ getParentRoute: () => rootRoute, path: '/r/$ri/s/$si', component: FlowView })
const fnRoute = createRoute({ getParentRoute: () => rootRoute, path: '/f', component: FnView })
const typeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/t', component: TypeView })

const routeTree = rootRoute.addChildren([indexRoute, groupRoute, routeRoute, stageRoute, fnRoute, typeRoute])

const router = createRouter({ routeTree, history: createHashHistory() })

export function App() {
  return <RouterProvider router={router} />
}
