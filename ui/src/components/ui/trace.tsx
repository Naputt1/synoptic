import { Dialog } from '@base-ui/react/dialog'
import {
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  ClipboardPaste,
  FileUp,
  Search,
  Trash2,
  X,
} from 'lucide-react'
import { startTransition, useEffect, useId, useMemo, useRef, useState } from 'react'

import { errSiteHref } from '@/lib/data'
import { matchLogs, type ErrGroup, type MatchWhy, type UnknownGroup } from '@/lib/errors'
import { emptyLogs, parseLogs, type ParsedLogs } from '@/lib/logparse'
import { goto } from '@/lib/nav'
import { statusBadgeTone, statusTone } from '@/lib/status'
import type { Data, JErr } from '@/lib/types'

import { AppLink } from './link'

const KIND_LABEL: Record<string, string> = {
  response: 'http',
  error: 'error',
  log: 'log',
  panic: 'panic',
}

const WHY_LABEL: Record<MatchWhy, string> = {
  stack: 'stack',
  exact: 'exact',
  template: 'template',
  partial: 'weak',
}

const WHY_TONE: Record<MatchWhy, string> = {
  stack: 'border-acc/45 text-acc',
  exact: 'border-acc/40 text-acc',
  template: 'border-info/40 text-info',
  partial: 'border-warn/40 text-warn',
}

const BAD_LEVEL = /^(warn|warning|error|fatal|panic|critical|crit|alert|emerg)$/i

const shortFn = (fn = ''): string => {
  const part = fn.split('|').pop() ?? fn
  return part.split('.').pop() ?? part
}

function siteWhere(data: Data, e: JErr): string {
  const ref = e.r?.[0]
  if (ref?.k === 'rpc') return shortFn(e.fn)
  const route = ref ? data.routes[ref.ri] : undefined
  return route ? `${route.m} ${route.p}` : shortFn(e.fn)
}

function GroupRow({
  g,
  data,
  expanded,
  onToggle,
  onOpen,
}: {
  g: ErrGroup
  data: Data
  expanded: boolean
  onToggle: () => void
  onOpen: (href?: string) => void
}) {
  const first = g.codes[0]
  return (
    <div className="border-line border-b last:border-b-0">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className="hover:bg-panel2/50 flex w-full items-start gap-2.5 px-3 py-2.5 text-left"
      >
        <span
          className={`mt-0.5 w-12 shrink-0 rounded-[3px] border px-1 text-center text-[9px] leading-[15px] tabular-nums ${
            first ? statusBadgeTone[statusTone(first)] : 'border-line text-mut uppercase'
          }`}
        >
          {first ?? KIND_LABEL[g.kind] ?? g.kind}
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-2">
            <span className="text-ink min-w-0 flex-1 truncate font-mono text-[11.5px]">{g.msg}</span>
            <span className="border-line bg-panel2 text-mut shrink-0 rounded-[3px] border px-1 text-[9px] tabular-nums">
              ×{g.count}
            </span>
          </span>
          <span className="text-faint mt-0.5 block truncate text-[9.5px]">
            {g.levels.length > 0 && `${g.levels.join(' · ')} · `}
            {siteWhere(data, g.site)} · {g.site.f}:{g.site.l}
            {g.hits.length > 1 ? ` · +${g.hits.length - 1} sites` : ''}
          </span>
          <span className="mt-1 flex flex-wrap items-center gap-1">
            <span className={`rounded-[3px] border px-1 text-[8.5px] uppercase ${WHY_TONE[g.why]}`}>
              {WHY_LABEL[g.why]}
            </span>
            {g.statuses.slice(0, 4).map((s) => (
              <span
                key={s}
                className={`rounded-[3px] border px-1 text-[8.5px] tabular-nums ${statusBadgeTone[statusTone(s)]}`}
              >
                {s}
              </span>
            ))}
            {g.codes.length > 0 && g.statuses.length === 0 && (
              <span className="text-faint text-[8.5px]">source answers {g.codes.join(' · ')}</span>
            )}
          </span>
        </span>
        {expanded ? (
          <ChevronDown className="text-faint mt-1 size-3.5 shrink-0" />
        ) : (
          <ChevronRight className="text-faint mt-1 size-3.5 shrink-0" />
        )}
      </button>

      {expanded && (
        <div className="border-line bg-canvas/30 border-t px-3 py-2">
          <div className="flex items-center justify-between gap-2">
            <span className="text-faint text-[9px] tracking-[0.1em] uppercase">
              {g.hits.length} source {g.hits.length === 1 ? 'site' : 'sites'}
            </span>
            <button
              type="button"
              onClick={() => onOpen(errSiteHref(data, g.site, g.site.r?.[0]))}
              className="text-acc hover:text-ink text-[10px] underline-offset-2 hover:underline"
            >
              open best site ›
            </button>
          </div>
          <div className="mt-1.5 flex flex-col gap-1">
            {g.hits.slice(0, 8).map((h, i) => {
              const href = errSiteHref(data, h.err, h.err.r?.[0])
              const label = `${siteWhere(data, h.err)} · ${h.err.f}:${h.err.l}`
              const line = (
                <>
                  <span className={`mr-1 rounded-[2px] border px-1 text-[8px] uppercase ${WHY_TONE[h.why]}`}>
                    {WHY_LABEL[h.why]}
                  </span>
                  <span className="truncate">{label}</span>
                </>
              )
              return href ? (
                <AppLink
                  key={`${h.err.f}:${h.err.l}:${i}`}
                  href={href}
                  onClick={() => onOpen(href)}
                  className="text-mut hover:text-ink flex min-w-0 items-center gap-1 font-mono text-[10px]"
                >
                  {line}
                </AppLink>
              ) : (
                <span key={`${h.err.f}:${h.err.l}:${i}`} className="text-mut flex min-w-0 items-center gap-1 font-mono text-[10px]">
                  {line}
                </span>
              )
            })}
            {g.hits.length > 8 && (
              <span className="text-faint text-[9.5px]">+{g.hits.length - 8} more sites</span>
            )}
          </div>

          {g.samples.length > 0 && (
            <div className="border-line mt-2 border-t pt-1.5">
              <div className="text-faint text-[9px] tracking-[0.1em] uppercase">
                log occurrences
              </div>
              <div className="mt-1 flex flex-col gap-1">
                {g.samples.map((s) => (
                  <div key={`${s.line}:${s.msg}`} className="text-faint flex min-w-0 gap-2 font-mono text-[9.5px]">
                    {s.ts && <span className="shrink-0">{s.ts}</span>}
                    {s.level && (
                      <span className={`shrink-0 uppercase ${BAD_LEVEL.test(s.level) ? 'text-warn' : ''}`}>
                        {s.level}
                      </span>
                    )}
                    <span className="text-mut truncate">{s.msg}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function UnknownRow({
  u,
  data,
  onOpen,
}: {
  u: UnknownGroup
  data: Data
  onOpen: (href: string) => void
}) {
  const suggestionHref = u.suggestion
    ? errSiteHref(data, u.suggestion.err, u.suggestion.err.r?.[0])
    : undefined
  return (
    <div className="border-line flex items-start gap-2.5 border-b px-3 py-2.5 last:border-b-0">
      <span className="border-warn/40 text-warn mt-0.5 w-12 shrink-0 rounded-[3px] border px-1 text-center text-[9px] leading-[15px] uppercase">
        miss
      </span>
      <span className="min-w-0 flex-1">
        <span className="text-ink block truncate font-mono text-[11.5px]">{u.msg}</span>
        <span className="text-faint mt-0.5 block text-[9.5px]">
          {u.levels.length > 0 && `${u.levels.join(' · ')} · `}
          ×{u.count} · no confident source match
        </span>
        {u.suggestion && suggestionHref && (
          <AppLink
            href={suggestionHref}
            onClick={() => onOpen(suggestionHref)}
            className="text-info hover:text-ink mt-1 block min-w-0 truncate font-mono text-[9.5px]"
          >
            closest: {u.suggestion.err.m} · {u.suggestion.err.f}:{u.suggestion.err.l}
          </AppLink>
        )}
      </span>
    </div>
  )
}

export function TraceDialog({
  data,
  open,
  onOpenChange,
  svc = '',
}: {
  data: Data
  open: boolean
  onOpenChange: (v: boolean) => void
  /** prefer sites reached by one service of a multi-entry module */
  svc?: string
}) {
  const [paste, setPaste] = useState('')
  const [parsed, setParsed] = useState<ParsedLogs>(emptyLogs)
  const [parsing, setParsing] = useState(false)
  const [query, setQuery] = useState('')
  const [view, setView] = useState<'matched' | 'unknown' | 'all'>('matched')
  const [onlyErrors, setOnlyErrors] = useState(true)
  const [sort, setSort] = useState<'count' | 'severity' | 'recent'>('count')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [drag, setDrag] = useState(false)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const listId = useId()

  useEffect(() => {
    if (!paste.trim()) {
      setParsed(emptyLogs())
      setParsing(false)
      return
    }
    setParsing(true)
    const id = window.setTimeout(() => {
      const next = parseLogs(paste)
      startTransition(() => {
        setParsed(next)
        setParsing(false)
        setExpanded(new Set())
      })
    }, 180)
    return () => window.clearTimeout(id)
  }, [paste])

  const matches = useMemo(
    () => matchLogs(data.errs, parsed, { routes: data.routes, svc }),
    [data.errs, data.routes, parsed, svc],
  )

  const q = query.trim().toLowerCase()
  const groups = useMemo(() => {
    let out = matches.groups
    if (onlyErrors) out = out.filter((g) => g.errorCount > 0 || g.kind === 'response' || g.kind === 'panic')
    if (q) {
      out = out.filter(
        (g) =>
          g.msg.toLowerCase().includes(q) ||
          g.site.f.toLowerCase().includes(q) ||
          siteWhere(data, g.site).toLowerCase().includes(q) ||
          g.levels.some((l) => l.includes(q)),
      )
    }
    if (sort === 'severity') {
      const order: Record<string, number> = { panic: 0, response: 1, error: 2, log: 3 }
      out = [...out].sort((a, b) => (order[a.kind] ?? 4) - (order[b.kind] ?? 4) || b.count - a.count)
    } else if (sort === 'recent') {
      out = [...out].sort((a, b) => (b.samples.at(-1)?.line ?? 0) - (a.samples.at(-1)?.line ?? 0))
    }
    return out
  }, [matches.groups, onlyErrors, q, sort, data])

  const unknown = useMemo(() => {
    if (!q) return matches.unknown
    return matches.unknown.filter(
      (u) =>
        u.msg.toLowerCase().includes(q) ||
        (u.suggestion?.err.m.toLowerCase().includes(q) ?? false) ||
        (u.suggestion?.err.f.toLowerCase().includes(q) ?? false),
    )
  }, [matches.unknown, q])

  const openHref = (href?: string) => {
    if (!href) return
    onOpenChange(false)
    goto(href)
  }

  const openGroup = (g?: ErrGroup) => {
    if (!g) return
    openHref(errSiteHref(data, g.site, g.site.r?.[0]))
  }

  const toggle = (key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const loadFile = async (file?: File) => {
    if (!file) return
    setPaste(await file.text())
    inputRef.current?.focus()
  }

  const clear = () => {
    setPaste('')
    setParsed(emptyLogs())
    setQuery('')
    setView('matched')
    inputRef.current?.focus()
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const quick = paste.split('\n').length <= 1 || e.metaKey || e.ctrlKey
    if (e.key === 'Enter' && !e.shiftKey && quick && groups.length > 0) {
      e.preventDefault()
      openGroup(groups[0])
    }
  }

  const counts = {
    lines: parsed.lines,
    errors: parsed.errorRecords,
    messages: matches.groups.length,
    matched: matches.matchedRecords,
    unknown: matches.unknown.length,
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="sheet-overlay fixed inset-0 z-50 bg-black/55 backdrop-blur-[1px]" />
        <Dialog.Popup
          data-slot="trace-dialog"
          initialFocus={inputRef}
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) onOpenChange(false)
          }}
          className="fixed inset-0 z-50 flex items-stretch justify-center p-2 outline-none sm:p-4"
        >
          <Dialog.Title className="sr-only">Trace runtime errors</Dialog.Title>
          <Dialog.Description className="sr-only">
            Paste or drop application logs to match every error message back to the source line
            that writes it.
          </Dialog.Description>
          <div className="sheet-content border-line bg-panel flex h-full w-full max-w-[1500px] flex-col overflow-hidden rounded-[8px] border shadow-[0_30px_80px_-30px_rgba(0,0,0,0.9)]">
            <div className="border-line bg-panel2/40 flex shrink-0 items-center gap-2 border-b px-3 py-2">
              <ClipboardPaste className="text-faint size-3.5 shrink-0" />
              <div className="min-w-0 flex-1">
                <div className="text-ink text-[12px] font-medium">trace runtime errors</div>
                <div className="text-faint truncate text-[9.5px]">
                  paste or drop a log · json, logfmt or plain text · source lines, not guesses
                </div>
              </div>
              <kbd className="border-line text-faint hidden rounded-[3px] border px-1 text-[9.5px] sm:inline">
                ctrl ⇧ E
              </kbd>
              <button
                type="button"
                aria-label="Close trace"
                onClick={() => onOpenChange(false)}
                className="text-faint hover:text-ink ml-1 shrink-0"
              >
                <X className="size-4" />
              </button>
            </div>

            <div className="grid min-h-0 flex-1 grid-rows-[minmax(150px,36%)_1fr] lg:grid-cols-[minmax(320px,380px)_1fr] lg:grid-rows-1">
              <div
                className={`border-line flex min-h-0 flex-col border-b lg:border-r lg:border-b-0 ${
                  drag ? 'bg-acc/[0.06]' : ''
                }`}
                onDragOver={(e) => {
                  e.preventDefault()
                  setDrag(true)
                }}
                onDragLeave={() => setDrag(false)}
                onDrop={(e) => {
                  e.preventDefault()
                  setDrag(false)
                  void loadFile(e.dataTransfer.files[0])
                }}
              >
                <div className="border-line flex shrink-0 items-center gap-1 border-b px-2 py-1.5">
                  <input
                    ref={fileRef}
                    type="file"
                    accept=".log,.txt,.json,text/plain,application/json"
                    className="hidden"
                    onChange={(e) => {
                      void loadFile(e.target.files?.[0])
                      e.target.value = ''
                    }}
                  />
                  <button
                    type="button"
                    onClick={() => fileRef.current?.click()}
                    className="text-mut hover:text-ink flex items-center gap-1 rounded-[4px] px-1.5 py-0.5 text-[10px]"
                  >
                    <FileUp className="size-3" /> open log
                  </button>
                  <span className="text-faint/60 text-[9px]">or drop one here</span>
                  {paste && (
                    <button
                      type="button"
                      onClick={clear}
                      className="text-faint hover:text-ink ml-auto flex items-center gap-1 rounded-[4px] px-1.5 py-0.5 text-[10px]"
                    >
                      <Trash2 className="size-3" /> clear
                    </button>
                  )}
                </div>
                <textarea
                  ref={inputRef}
                  value={paste}
                  onChange={(e) => setPaste(e.target.value)}
                  onKeyDown={onKeyDown}
                  placeholder="paste application logs, a panic with its stack, or one error line"
                  aria-label="Runtime log text"
                  aria-controls={listId}
                  spellCheck={false}
                  className="text-ink placeholder:text-faint min-h-[110px] w-full flex-1 resize-none bg-transparent p-3 font-mono text-[10.5px] leading-[1.55] outline-none"
                />
                <div className="border-line text-faint flex shrink-0 flex-wrap items-center gap-x-2 gap-y-1 border-t px-3 py-1.5 text-[9px]">
                  {parsing ? (
                    <span className="text-acc">parsing…</span>
                  ) : (
                    <span>
                      {counts.lines.toLocaleString()} lines · {counts.errors.toLocaleString()} error
                      records
                    </span>
                  )}
                  {parsed.truncated && <span className="text-warn">input capped at 4 MB / 20k lines</span>}
                  <span className="text-faint/70 ml-auto">enter opens the top match for one line</span>
                </div>
              </div>

              <div className="flex min-h-0 flex-col">
                <div className="border-line flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-1.5">
                  <div className="text-faint text-[9px] tracking-[0.1em] uppercase">
                    {counts.messages} messages
                    <span className="text-faint/70 ml-1 normal-case tracking-normal">
                      · {counts.matched} matched · {counts.unknown} unknown
                    </span>
                  </div>
                  <div className="ml-auto flex items-center gap-1">
                    <span className="text-faint flex items-center gap-1 text-[9px]">
                      <Search className="size-2.5" />
                    </span>
                    <input
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                      placeholder="filter message or file"
                      aria-label="Filter messages"
                      className="border-line bg-panel text-ink placeholder:text-faint h-6 w-36 rounded-[4px] border px-1.5 text-[10px] outline-none"
                    />
                    <select
                      value={view}
                      onChange={(e) => setView(e.target.value as typeof view)}
                      aria-label="Result type"
                      className="border-line bg-panel text-mut h-6 rounded-[4px] border px-1 text-[9.5px] outline-none"
                    >
                      <option value="matched">matched</option>
                      <option value="unknown">unknown</option>
                      <option value="all">all</option>
                    </select>
                    <select
                      value={sort}
                      onChange={(e) => setSort(e.target.value as typeof sort)}
                      aria-label="Sort results"
                      className="border-line bg-panel text-mut h-6 rounded-[4px] border px-1 text-[9.5px] outline-none"
                    >
                      <option value="count">count</option>
                      <option value="severity">severity</option>
                      <option value="recent">recent</option>
                    </select>
                    <label className="text-faint ml-1 flex items-center gap-1 text-[9.5px]">
                      <input
                        type="checkbox"
                        checked={onlyErrors}
                        onChange={(e) => setOnlyErrors(e.target.checked)}
                        className="accent-acc size-3"
                      />
                      errors only
                    </label>
                  </div>
                </div>

                <div id={listId} className="min-h-0 flex-1 overflow-auto">
                  {!paste.trim() && (
                    <div className="text-faint flex h-full min-h-40 flex-col items-center justify-center px-6 text-center text-[12px]">
                      <ClipboardPaste className="text-faint/70 mb-2 size-5" />
                      paste a log file or drop one here
                      <div className="text-faint/70 mt-1 max-w-[360px] text-[10px] leading-relaxed">
                        errors are grouped by source message; stack frames and request fields pick
                        the right line when the same message is written in several places.
                      </div>
                    </div>
                  )}
                  {paste.trim() && !parsing && parsed.records.length === 0 && (
                    <div className="text-faint flex h-full min-h-40 flex-col items-center justify-center text-center text-[12px]">
                      <AlertTriangle className="text-warn/70 mb-2 size-5" />
                      no log records found
                      <div className="text-faint/70 mt-1 text-[10px]">try plain text or JSON lines</div>
                    </div>
                  )}
                  {paste.trim() && view !== 'unknown' &&
                    groups.map((g) => (
                      <GroupRow
                        key={g.key}
                        g={g}
                        data={data}
                        expanded={expanded.has(g.key)}
                        onToggle={() => toggle(g.key)}
                        onOpen={openHref}
                      />
                    ))}
                  {paste.trim() && view !== 'matched' &&
                    unknown.map((u) => (
                      <UnknownRow key={u.key} u={u} data={data} onOpen={openHref} />
                    ))}
                  {paste.trim() &&
                    !parsing &&
                    (view === 'matched'
                      ? groups.length === 0
                      : view === 'unknown'
                        ? unknown.length === 0
                        : groups.length + unknown.length === 0) && (
                      <div className="text-faint flex min-h-32 flex-col items-center justify-center px-6 text-center text-[11.5px]">
                        {view === 'matched'
                          ? 'matched messages are hiding here'
                          : view === 'unknown'
                            ? 'every record matched a source message'
                            : 'nothing matches this filter'}
                        <div className="text-faint/70 mt-1 text-[10px]">
                          {view === 'matched'
                            ? 'switch to unknown to see the records that did not match'
                            : view === 'unknown'
                              ? 'unknown records will appear here when a message has no source site'
                              : 'clear the filter or show all result types'}
                        </div>
                      </div>
                    )}
                </div>

                <div className="border-line text-faint flex shrink-0 flex-wrap items-center gap-2 border-t px-3 py-1.5 text-[9px]">
                  <span>ctrl ⇧ E opens this from anywhere</span>
                  <span className="text-faint/60">·</span>
                  <span>esc closes</span>
                  <span className="text-faint/60 ml-auto">{parsed.chars.toLocaleString()} chars</span>
                </div>
              </div>
            </div>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
