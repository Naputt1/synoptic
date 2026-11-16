import type { KeyboardEvent, ReactNode } from 'react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { ScopeCard } from '@/components/ScopeCard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox, type ComboboxOption } from '@/components/ui/combobox'
import { AppLink } from '@/components/ui/link'
import { Segmented } from '@/components/ui/segmented'
import { comparePR, compareRefs, fetchRefs } from '@/lib/api'
import { changeCounts, changeMode, changeTitle, headLabel, refLabel } from '@/lib/change'
import { useData, useDataUpdate } from '@/lib/context'
import { routeHref } from '@/lib/data'
import type { ChangeFile, ChangeRoute, Refs } from '@/lib/types'

type ChangeKind = 'added' | 'removed' | 'modified'
type Filter = 'all' | ChangeKind
type Selection = { kind: ChangeKind; route: ChangeRoute }

type DiffKind = 'add' | 'remove' | 'hunk' | 'meta' | 'context'

type DiffLine = {
  text: string
  kind: DiffKind
  oldNo: number | null
  newNo: number | null
}

const metaPrefixes = [
  'diff --git',
  'index ',
  '--- ',
  '+++ ',
  'new file mode',
  'deleted file mode',
  'old mode',
  'new mode',
  'similarity index',
  'rename from',
  'rename to',
  'Binary files',
  'GIT binary patch',
]

const routeKey = (r: ChangeRoute) => `${r.m}:${r.p}:${r.svc ?? ''}`

/** Read a comparison out of the hash so `#/changes?base=…&to=…` is shareable. */
function compareFromHash(): { base: string; to: string; pr: string } | null {
  const raw = window.location.hash
  const qi = raw.indexOf('?')
  if (qi < 0) return null
  const params = new URLSearchParams(raw.slice(qi + 1))
  const base = params.get('base')?.trim() ?? ''
  const to = params.get('to')?.trim() ?? ''
  const pr = params.get('pr')?.trim() ?? ''
  return base || to || pr ? { base, to, pr } : null
}

/** Keep the address bar in step with the comparison the user is looking at. */
function syncCompareHash(base: string, to: string, pr: string) {
  const params = new URLSearchParams()
  if (pr) {
    params.set('pr', pr)
  } else {
    if (base) params.set('base', base)
    if (to) params.set('to', to)
  }
  const query = params.toString()
  const next = `#/changes${query ? `?${query}` : ''}`
  if (window.location.hash !== next) window.history.replaceState(null, '', next)
}

const fileTone = (status: string): 'accent' | 'bad' | 'warn' | 'default' => {
  if (status === 'A') return 'accent'
  if (status === 'D') return 'bad'
  if (status === 'R' || status === 'C') return 'warn'
  return 'default'
}

const routeTone = (tone: 'accent' | 'bad' | 'warn') =>
  tone === 'accent' ? 'text-acc' : tone === 'bad' ? 'text-bad' : 'text-warn'

const filterTone = (key: Filter, active: boolean) => {
  if (!active) return 'border-line text-mut hover:border-line2 hover:text-ink'
  if (key === 'added') return 'border-acc/45 bg-acc/10 text-acc'
  if (key === 'removed') return 'border-bad/45 bg-bad/10 text-bad'
  if (key === 'modified') return 'border-warn/45 bg-warn/10 text-warn'
  return 'border-line2 bg-panel2 text-ink'
}

function diffStats(patch: string) {
  let added = 0
  let removed = 0
  for (const line of patch.split('\n')) {
    if (line.startsWith('+') && !line.startsWith('+++')) added += 1
    else if (line.startsWith('-') && !line.startsWith('---')) removed += 1
  }
  return { added, removed }
}

/** Split a unified patch and track the old/new line numbers for each hunk. */
function parsePatch(patch: string): DiffLine[] {
  const lines: DiffLine[] = []
  let oldNo = 0
  let newNo = 0
  const body = patch.endsWith('\n') ? patch.slice(0, -1) : patch

  for (const text of body.split('\n')) {
    if (text.startsWith('@@')) {
      const m = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(text)
      if (m) {
        oldNo = Number(m[1])
        newNo = Number(m[2])
      }
      lines.push({ text, kind: 'hunk', oldNo: null, newNo: null })
      continue
    }
    if (metaPrefixes.some((p) => text.startsWith(p)) || text.startsWith('\\')) {
      lines.push({ text, kind: 'meta', oldNo: null, newNo: null })
      continue
    }
    if (text.startsWith('+')) {
      lines.push({ text, kind: 'add', oldNo: null, newNo })
      newNo += 1
      continue
    }
    if (text.startsWith('-')) {
      lines.push({ text, kind: 'remove', oldNo, newNo: null })
      oldNo += 1
      continue
    }
    lines.push({ text, kind: 'context', oldNo, newNo })
    if (text !== '') {
      oldNo += 1
      newNo += 1
    }
  }
  return lines
}

function PatchView({ patch }: { patch: string }) {
  const lines = useMemo(() => parsePatch(patch), [patch])
  return (
    <div className="max-h-[72vh] overflow-auto">
      <div className="w-max min-w-full py-1">
        {lines.map((line, i) => {
          const tone =
            line.kind === 'add'
              ? 'bg-acc/10 text-acc'
              : line.kind === 'remove'
                ? 'bg-bad/10 text-bad'
                : line.kind === 'hunk'
                  ? 'bg-info/10 text-info'
                  : line.kind === 'meta'
                    ? 'text-faint'
                    : 'text-mut'
          return (
            <div key={i} className={`flex text-[11px] leading-[1.6] ${tone}`}>
              <span className="text-faint/70 border-line/40 w-10 shrink-0 border-r pr-2 text-right text-[10px] select-none">
                {line.oldNo ?? ''}
              </span>
              <span className="text-faint/70 border-line/40 w-10 shrink-0 border-r pr-2 text-right text-[10px] select-none">
                {line.newNo ?? ''}
              </span>
              <code className="px-3 whitespace-pre">{line.text || ' '}</code>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function CodePanel({
  path,
  file,
  route,
  ri,
}: {
  path: string | null
  file?: ChangeFile
  route?: ChangeRoute
  ri: number
}) {
  const stats = file?.patch ? diffStats(file.patch) : null
  return (
    <section className="border-line bg-panel/40 min-w-0 rounded-[6px] border">
      <div className="border-line flex flex-wrap items-baseline gap-2 border-b px-3 py-1.5">
        <span className="text-mut text-[10px] tracking-[0.14em] uppercase">patch</span>
        {file && <Badge variant={fileTone(file.status)}>{file.status}</Badge>}
        <code className="text-ink min-w-0 flex-1 truncate text-[11px]" title={path ?? undefined}>
          {path ?? 'no file selected'}
        </code>
        {stats && (
          <span className="text-faint shrink-0 text-[10px]">
            <b className="text-acc font-semibold">+{stats.added}</b>{' '}
            <b className="text-bad font-semibold">−{stats.removed}</b>
          </span>
        )}
        {route && ri >= 0 && (
          <AppLink href={routeHref(ri)} className="text-acc shrink-0 text-[10px] hover:underline">
            open route ›
          </AppLink>
        )}
      </div>
      {route && (
        <div className="border-line/60 text-faint flex flex-wrap items-baseline gap-2 border-b px-3 py-1 text-[10px]">
          <code className="text-mut">{route.m}</code>
          <span className="text-ink min-w-0 truncate">{route.p}</span>
          {route.svc && <span>{route.svc}</span>}
          {route.f && (
            <span className="ml-auto shrink-0" title={route.f}>
              {route.f}
              {route.l ? `:${route.l}` : ''}
            </span>
          )}
        </div>
      )}
      {file?.patch ? (
        <PatchView patch={file.patch} />
      ) : (
        <div className="text-faint px-3 py-8 text-center text-[11px] leading-relaxed">
          {!path
            ? route
              ? 'this route has no head-side file to patch — pick a changed file from the left rail.'
              : 'select a route diff or a changed file to inspect its patch.'
            : !file
              ? 'this path is not in the changed file list of this comparison.'
              : 'no patch was included for this file.'}
        </div>
      )}
    </section>
  )
}

function RouteRow({
  route,
  ri,
  tone,
  selected,
  onSelect,
  why,
}: {
  route: ChangeRoute
  ri: number
  tone: 'accent' | 'bad' | 'warn'
  selected: boolean
  onSelect: () => void
  why?: string[]
}) {
  const pathTitle = route.f ? `${route.f}${route.l ? `:${route.l}` : ''}` : route.p
  return (
    <div
      className={`flex items-baseline gap-2 px-3 py-1.5 text-[11px] ${
        selected ? 'bg-acc/10' : 'hover:bg-panel2'
      }`}
    >
      <button
        type="button"
        onClick={onSelect}
        aria-pressed={selected}
        className="flex min-w-0 flex-1 items-baseline gap-2 text-left"
        title={`show ${pathTitle} in the patch panel`}
      >
        <code className={`w-12 shrink-0 text-[10px] font-semibold ${routeTone(tone)}`}>
          {route.m}
        </code>
        <span className="text-ink min-w-0 flex-1 truncate">{route.p}</span>
        {route.svc && <span className="text-faint shrink-0 text-[10px]">{route.svc}</span>}
        {why && why.length > 0 && (
          <span
            className="text-faint max-w-[160px] shrink-0 truncate text-[10px]"
            title={why.join(', ')}
          >
            {why.join(', ')}
          </span>
        )}
        <Badge variant={tone}>
          {tone === 'accent' ? 'added' : tone === 'bad' ? 'removed' : 'modified'}
        </Badge>
      </button>
      {ri >= 0 && (
        <AppLink
          href={routeHref(ri)}
          className="text-faint hover:text-acc shrink-0 text-[9.5px]"
          title="open the route details"
        >
          open route ›
        </AppLink>
      )}
    </div>
  )
}

function Group({
  label,
  count,
  children,
  empty,
}: {
  label: string
  count: number
  children: ReactNode
  empty: string
}) {
  return (
    <>
      <div className="border-line/60 text-faint flex items-baseline justify-between border-b px-3 py-1.5 text-[9.5px] tracking-[0.14em] uppercase">
        <span>{label}</span>
        <span className="tracking-normal normal-case">{count}</span>
      </div>
      <div className="divide-line/60 divide-y">
        {count === 0 ? <div className="text-faint px-3 py-2 text-[11px]">{empty}</div> : children}
      </div>
    </>
  )
}

export function ChangesView() {
  const data = useData()
  const setData = useDataUpdate()
  const change = data.change

  const [refs, setRefs] = useState<Refs | null | undefined>(undefined)
  const hashCompare = useMemo(() => compareFromHash(), [])
  const [mode, setMode] = useState<'commits' | 'pr'>(
    hashCompare?.pr ? 'pr' : change?.mode === 'pr' ? 'pr' : 'commits',
  )
  const [base, setBase] = useState(hashCompare?.base || change?.ref || '')
  const [head, setHead] = useState(hashCompare?.to || change?.to || '')
  const [pr, setPr] = useState(hashCompare?.pr || (change?.pr ? String(change.pr) : ''))
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState<Filter>('all')
  const [selection, setSelection] = useState<Selection | null>(null)
  const [selectedFile, setSelectedFile] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    void fetchRefs().then((result) => {
      if (!live) return
      setRefs(result)
      const current = result?.current
      if (current) setBase((v) => v || current)
    })
    return () => {
      live = false
    }
  }, [])

  const refOptions = useMemo<ComboboxOption[]>(() => {
    const out: ComboboxOption[] = []
    const seen = new Set<string>()
    const add = (value: string | undefined, label: string, kind: string, detail?: string) => {
      if (!value || seen.has(value)) return
      seen.add(value)
      out.push({ value, label, kind, detail })
    }
    if (refs?.current) add(refs.current, refs.current, 'branch')
    for (const b of refs?.branches ?? []) add(b, b, 'branch')
    for (const t of refs?.tags ?? []) add(t, t, 'tag')
    for (const c of refs?.commits ?? []) add(c.sha, c.subject, 'commit', refLabel(c.sha))
    return out
  }, [refs])

  const prOptions = useMemo<ComboboxOption[]>(
    () =>
      (refs?.prs ?? []).map((p) => ({
        value: String(p.number),
        label: p.title,
        kind: 'pr',
        detail: `#${p.number}`,
      })),
    [refs],
  )

  const routeIndex = useMemo(() => {
    const map = new Map<string, number>()
    data.routes.forEach((r, i) => map.set(`${r.m}:${r.p}:${r.svc ?? ''}`, i))
    return map
  }, [data.routes])

  const added = change?.added ?? []
  const removed = change?.removed ?? []
  const modified = change?.modified ?? []
  const commits = change?.commits ?? []
  const files = change?.fileList ?? []
  const counts = change
    ? changeCounts(change)
    : { added: 0, removed: 0, modified: 0, commits: 0, files: 0 }
  const totalRoutes = added.length + removed.length + modified.length
  const refsReady = refs != null

  const activeRI = selection ? (routeIndex.get(routeKey(selection.route)) ?? -1) : -1
  const headRoute = activeRI >= 0 ? data.routes[activeRI] : undefined
  const activePath = selection ? (selection.route.f ?? headRoute?.f ?? null) : selectedFile
  const activeFile = activePath ? files.find((f) => f.path === activePath) : undefined
  const activeRoute = selection
    ? headRoute
      ? {
          ...selection.route,
          f: selection.route.f ?? headRoute.f,
          l: selection.route.l ?? headRoute.l,
        }
      : selection.route
    : undefined

  const filterCount =
    filter === 'all'
      ? totalRoutes
      : filter === 'added'
        ? added.length
        : filter === 'removed'
          ? removed.length
          : modified.length

  const selectRoute = (kind: ChangeKind, route: ChangeRoute) => {
    setSelection({ kind, route })
    setSelectedFile(null)
  }

  const selectFile = (path: string) => {
    setSelection(null)
    setSelectedFile(path)
  }

  const isSelected = (kind: ChangeKind, route: ChangeRoute) =>
    selection?.kind === kind && routeKey(selection.route) === routeKey(route)

  const busy = useRef(false)
  const runCompare = useCallback(
    async (baseRef: string, headRef: string, prRef: string) => {
      if (busy.current) return
      const baseValue = baseRef.trim()
      const headValue = headRef.trim()
      const prValue = prRef.trim()
      if (!prValue && !baseValue) {
        setError('choose a base ref before comparing')
        return
      }

      setError(null)
      setLoading(true)
      busy.current = true
      try {
        const result = prValue ? await comparePR(prValue) : await compareRefs(baseValue, headValue)
        setData(result)
        setSelection(null)
        setSelectedFile(null)
        setFilter('all')
        setMode(result.change?.pr ? 'pr' : 'commits')
        if (result.change) {
          const nextBase = result.change.ref ?? baseValue
          const nextHead = result.change.to ?? headValue
          const nextPR = result.change.pr ? String(result.change.pr) : prValue
          setBase(nextBase)
          setHead(nextHead)
          if (nextPR) setPr(nextPR)
          syncCompareHash(nextBase, nextHead, nextPR)
        }
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e))
      } finally {
        busy.current = false
        setLoading(false)
      }
    },
    [setData],
  )

  const compareNow = () => {
    if (!refsReady || loading) return
    if (mode === 'pr' && !pr.trim()) {
      setError('enter a pull request number or URL')
      return
    }
    if (mode === 'commits' && !base.trim()) {
      setError('choose a base ref before comparing')
      return
    }
    void runCompare(base, head, mode === 'pr' ? pr : '')
  }

  const autoRan = useRef(false)
  useEffect(() => {
    if (autoRan.current || !refs || !hashCompare) return
    autoRan.current = true
    void runCompare(hashCompare.base, hashCompare.to, hashCompare.pr)
  }, [refs, hashCompare, runCompare])

  const onEnter = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      void compareNow()
    }
  }

  const filters: { key: Filter; label: string; count: number }[] = [
    { key: 'all', label: 'all', count: totalRoutes },
    { key: 'added', label: 'added', count: added.length },
    { key: 'removed', label: 'removed', count: removed.length },
    { key: 'modified', label: 'modified', count: modified.length },
  ]

  return (
    <div className="flex h-full flex-col overflow-auto">
      <div className="border-line bg-panel/30 border-b">
        <div className="flex flex-wrap items-end gap-x-3 gap-y-2 px-4 py-2">
          <div className="flex items-center gap-2">
            <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">compare</span>
            <Segmented
              value={mode}
              onChange={(v) => {
                setMode(v === 'pr' ? 'pr' : 'commits')
                setError(null)
              }}
              options={[
                { value: 'commits', label: 'commits' },
                { value: 'pr', label: 'pull request' },
              ]}
            />
          </div>
          {mode === 'commits' ? (
            <>
              <label className="flex items-center gap-1.5">
                <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">
                  base <span className="text-bad">*</span>
                </span>
                <Combobox
                  value={base}
                  onValueChange={setBase}
                  onKeyDown={onEnter}
                  options={refOptions}
                  disabled={!refsReady || loading}
                  placeholder="branch, tag or sha"
                  ariaLabel="base ref"
                  empty="no matching ref; you can still type one"
                  className="w-[180px] sm:w-[210px]"
                />
              </label>
              <span className="text-faint pb-1 text-[11px]">→</span>
              <label className="flex items-center gap-1.5">
                <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">head</span>
                <Combobox
                  value={head}
                  onValueChange={setHead}
                  onKeyDown={onEnter}
                  options={refOptions}
                  disabled={!refsReady || loading}
                  placeholder="working tree (leave empty)"
                  ariaLabel="head ref; empty compares the working tree"
                  empty="no matching ref; you can still type one"
                  className="w-[180px] sm:w-[210px]"
                />
              </label>
              {head !== '' && (
                <Button
                  variant="ghost"
                  size="sm"
                  type="button"
                  onClick={() => setHead('')}
                  disabled={loading}
                  title="compare against the working tree instead"
                >
                  working tree
                </Button>
              )}
            </>
          ) : (
            <label className="flex items-center gap-1.5">
              <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">
                pull request
              </span>
              <Combobox
                value={pr}
                onValueChange={setPr}
                onKeyDown={onEnter}
                options={prOptions}
                disabled={!refsReady || loading}
                placeholder="number or URL"
                ariaLabel="pull request number or URL"
                empty="no matching pull request; you can still type a number or URL"
                className="w-[200px] sm:w-[260px]"
              />
            </label>
          )}
          <Button
            variant="accent"
            size="sm"
            type="button"
            onClick={() => void compareNow()}
            disabled={!refsReady || loading}
            aria-busy={loading}
          >
            {loading ? 'comparing…' : 'compare'}
          </Button>
          {loading && (
            <span role="status" className="text-warn pb-1 text-[10px]">
              reading the comparison…
            </span>
          )}
        </div>
        <div className="text-faint flex flex-wrap items-center gap-x-3 gap-y-1 px-4 pb-1.5 text-[10px]">
          {refs?.current && (
            <span>
              current branch <code className="text-mut">{refs.current}</code>
            </span>
          )}
          <span>
            {mode === 'commits'
              ? 'head empty compares the working tree'
              : 'PR number or GitHub URL'}
          </span>
          {refs === undefined && <span className="text-warn">reading refs…</span>}
          {refs === null && (
            <span>
              read-only · run <code className="text-ink">synoptic serve</code> to compare other refs
            </span>
          )}
        </div>
        {mode === 'pr' && refs?.prsError && (
          <div className="border-warn/30 bg-warn/10 text-warn border-t px-4 py-1 text-[10px]">
            pull requests unavailable: {refs.prsError}
          </div>
        )}
      </div>

      {error && (
        <div
          role="alert"
          className="border-bad/45 bg-bad/10 text-bad flex items-start gap-2 border-b px-4 py-2 text-[11px]"
        >
          <b className="shrink-0 font-semibold">compare failed:</b>
          <span className="max-h-24 min-w-0 flex-1 overflow-auto whitespace-pre-wrap">{error}</span>
          <button
            type="button"
            onClick={() => setError(null)}
            className="hover:text-ink shrink-0 text-[10px] underline"
          >
            dismiss
          </button>
        </div>
      )}

      {!change ? (
        <>
          <ScopeCard
            title="changes"
            badge="git"
            meta={refsReady ? 'no comparison selected' : 'no comparison baked into this page'}
            note={
              refsReady
                ? 'choose a base ref above and compare: the review workspace shows commits, changed files and the route diff.'
                : 'run synoptic with -base <ref>, -base <from> -to <to>, or -pr <number> to compare two sides, or run synoptic serve to compare from here.'
            }
          />
          <div className="p-4">
            <div className="border-line bg-panel/40 text-faint rounded-[6px] border px-3 py-6 text-center text-[11px]">
              {refsReady
                ? 'pick a base ref above, optionally a head ref, then compare. leave head empty to include uncommitted edits.'
                : 'this report describes one tree. rerun with a comparison to see commits, files and the route diff here.'}
            </div>
          </div>
        </>
      ) : (
        <>
          <ScopeCard
            title={`changes · ${changeTitle(change)}`}
            badge={changeMode(change)}
            meta={`${counts.added} added · ${counts.removed} removed · ${counts.modified} changed · ${change.files} ${change.files === 1 ? 'file' : 'files'} touched · ${counts.commits} ${counts.commits === 1 ? 'commit' : 'commits'}`}
            note={
              change.mode === 'pr'
                ? 'the base side is the merge-base of the pull request, so routes added on the base branch after the fork do not appear as removals.'
                : change.mode === 'range'
                  ? 'both sides are commits; uncommitted edits in the checkout are not part of this comparison.'
                  : 'the base side is read-only; file-touch marks and the route diff are best-effort.'
            }
          />

          <div className="border-line/60 bg-panel/20 flex flex-wrap items-baseline gap-x-2 gap-y-1 border-b px-4 py-1.5 text-[10.5px]">
            <Badge variant="info">{changeMode(change)}</Badge>
            <code className="text-ink">{change.ref}</code>
            <span className="text-faint">→</span>
            <code className="text-ink">{headLabel(change)}</code>
            {change.prUrl && (
              <a
                className="text-acc ml-auto text-[10px] hover:underline"
                href={change.prUrl}
                target="_blank"
                rel="noreferrer"
              >
                open PR ↗
              </a>
            )}
          </div>

          <div className="grid items-start gap-3 p-4 lg:grid-cols-[280px_minmax(0,1fr)] xl:grid-cols-[290px_minmax(0,1fr)_minmax(0,1.1fr)]">
            <aside className="min-w-0 space-y-3">
              <section className="border-line bg-panel/40 rounded-[6px] border">
                <div className="border-line text-mut flex items-baseline justify-between border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
                  <span>filters</span>
                  <span className="text-faint tracking-normal normal-case">
                    {totalRoutes} route changes
                  </span>
                </div>
                <div className="border-line/60 flex flex-wrap gap-1.5 border-b px-2 py-2">
                  {filters.map((f) => (
                    <button
                      key={f.key}
                      type="button"
                      onClick={() => setFilter(f.key)}
                      aria-pressed={filter === f.key}
                      className={`rounded-[3px] border px-1.5 py-0.5 text-[10px] transition-colors ${filterTone(f.key, filter === f.key)}`}
                    >
                      {f.label} <b className="font-semibold">{f.count}</b>
                    </button>
                  ))}
                </div>
                <div className="text-faint px-3 py-1.5 text-[10px]">
                  {counts.added} added · {counts.removed} removed · {counts.modified} modified ·{' '}
                  {counts.commits} {counts.commits === 1 ? 'commit' : 'commits'}
                </div>
              </section>

              <section className="border-line bg-panel/40 rounded-[6px] border">
                <div className="border-line text-mut flex items-baseline justify-between border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
                  <span>commits</span>
                  <span className="text-faint tracking-normal normal-case">{counts.commits}</span>
                </div>
                <div className="divide-line/60 divide-y">
                  {commits.length === 0 && (
                    <div className="text-faint px-3 py-2 text-[11px]">
                      {change.mode === 'worktree'
                        ? 'no commit between the base and HEAD; uncommitted edits have no commit entry'
                        : 'no commit between the two sides'}
                    </div>
                  )}
                  {commits.map((c) => (
                    <div key={c.sha} className="px-3 py-1.5 text-[11px]">
                      <div className="flex items-baseline gap-2">
                        <code className="text-acc shrink-0 text-[10px]" title={c.sha}>
                          {refLabel(c.sha)}
                        </code>
                        <span className="text-ink min-w-0 flex-1 truncate" title={c.subject}>
                          {c.subject}
                        </span>
                      </div>
                      <div className="text-faint mt-0.5 flex items-baseline gap-2 text-[9.5px]">
                        <span className="min-w-0 flex-1 truncate">{c.author}</span>
                        <span className="shrink-0">{c.date.slice(0, 10)}</span>
                      </div>
                    </div>
                  ))}
                </div>
                {(change.commitsMore ?? 0) > 0 && (
                  <div className="text-faint border-line/60 border-t px-3 py-1.5 text-[10px]">
                    showing {commits.length} of {commits.length + (change.commitsMore ?? 0)}
                  </div>
                )}
              </section>

              <section className="border-line bg-panel/40 rounded-[6px] border">
                <div className="border-line text-mut flex items-baseline justify-between border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
                  <span>changed files</span>
                  <span className="text-faint tracking-normal normal-case">
                    {change.files} .go {change.files === 1 ? 'file' : 'files'}
                  </span>
                </div>
                <div className="divide-line/60 divide-y">
                  {files.length === 0 && (
                    <div className="text-faint px-3 py-2 text-[11px]">no .go file changed</div>
                  )}
                  {files.map((f) => (
                    <button
                      key={`${f.status}:${f.path}`}
                      type="button"
                      onClick={() => selectFile(f.path)}
                      aria-pressed={activePath === f.path}
                      className={`flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px] transition-colors ${
                        activePath === f.path ? 'bg-acc/10' : 'hover:bg-panel2'
                      }`}
                    >
                      <Badge variant={fileTone(f.status)}>{f.status}</Badge>
                      <code className="text-ink min-w-0 flex-1 truncate" title={f.path}>
                        {f.path}
                      </code>
                      {f.patch ? (
                        <span className="text-acc shrink-0 text-[9px]">diff</span>
                      ) : (
                        <span className="text-faint shrink-0 text-[9px]">no diff</span>
                      )}
                    </button>
                  ))}
                </div>
                {(change.filesMore ?? 0) > 0 && (
                  <div className="text-faint border-line/60 border-t px-3 py-1.5 text-[10px]">
                    showing {files.length} of {files.length + (change.filesMore ?? 0)}
                  </div>
                )}
              </section>
            </aside>

            <section className="border-line bg-panel/40 min-w-0 rounded-[6px] border">
              <div className="border-line text-mut flex items-baseline justify-between border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
                <span>route diff</span>
                <span className="text-faint tracking-normal normal-case">
                  {filter === 'all' ? `${totalRoutes} rows` : `${filterCount} ${filter}`}
                </span>
              </div>
              <div className="border-line/60 text-faint border-b px-3 py-1.5 text-[10px]">
                click a row to load its head-side file into the patch panel. open route opens the
                route details.
              </div>
              {(filter === 'all' || filter === 'added') && (
                <Group label="added" count={added.length} empty="no route was added">
                  {added.map((r) => (
                    <RouteRow
                      key={`add:${routeKey(r)}`}
                      route={r}
                      ri={routeIndex.get(routeKey(r)) ?? -1}
                      tone="accent"
                      selected={isSelected('added', r)}
                      onSelect={() => selectRoute('added', r)}
                    />
                  ))}
                </Group>
              )}
              {(filter === 'all' || filter === 'removed') && (
                <Group label="removed" count={removed.length} empty="no route was removed">
                  {removed.map((r) => (
                    <RouteRow
                      key={`rm:${routeKey(r)}`}
                      route={r}
                      ri={routeIndex.get(routeKey(r)) ?? -1}
                      tone="bad"
                      selected={isSelected('removed', r)}
                      onSelect={() => selectRoute('removed', r)}
                    />
                  ))}
                </Group>
              )}
              {(filter === 'all' || filter === 'modified') && (
                <Group
                  label="modified"
                  count={modified.length}
                  empty="no route kept its identity but changed"
                >
                  {modified.map((r) => (
                    <RouteRow
                      key={`mod:${routeKey(r)}`}
                      route={r}
                      ri={routeIndex.get(routeKey(r)) ?? -1}
                      tone="warn"
                      selected={isSelected('modified', r)}
                      onSelect={() => selectRoute('modified', r)}
                      why={r.why}
                    />
                  ))}
                </Group>
              )}
            </section>

            <div className="min-w-0 lg:col-span-2 xl:col-span-1">
              <CodePanel path={activePath} file={activeFile} route={activeRoute} ri={activeRI} />
            </div>
          </div>
        </>
      )}
    </div>
  )
}
