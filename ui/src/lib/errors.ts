import type { LogRecord, ParsedLogs } from './logparse'
import type { JErr, Route } from './types'

/** Why a site was picked; stack is the strongest signal a log can carry. */
export type MatchWhy = 'stack' | 'exact' | 'template' | 'partial'

export type SiteHit = { err: JErr; score: number; why: MatchWhy }

export type GroupSample = {
  ts?: string
  level?: string
  msg: string
  line: number
}

/** One source message, with every place it is written and every log occurrence. */
export type ErrGroup = {
  key: string
  msg: string
  kind: JErr['k']
  codes: string[]
  hits: SiteHit[]
  site: JErr
  score: number
  why: MatchWhy
  count: number
  errorCount: number
  levels: string[]
  statuses: string[]
  samples: GroupSample[]
}

export type UnknownGroup = {
  key: string
  msg: string
  count: number
  levels: string[]
  suggestion?: SiteHit
}

export type LogMatchResult = {
  groups: ErrGroup[]
  unknown: UnknownGroup[]
  matchedRecords: number
  errorRecords: number
}

export type MatchContext = { routes?: Route[]; svc?: string }

const VERB = /%(?:\[[0-9]+\])?[-+# 0-9'.]*[sdvqtxXfFeEgGwp]/g
const ANSI = /\u001b\[[0-9;]*m/g
const THRESHOLD = 300

const norm = (s: string) =>
  s
    .replace(ANSI, '')
    .toLowerCase()
    .replace(/\s+/g, ' ')
    .replace(/^["'`]+|["'`]+$/g, '')
    .trim()
const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

type Source = {
  e: JErr
  text: string
  tokens: string[]
  re: RegExp | null
}

function templateRe(msg: string): RegExp | null {
  if (!msg.includes('%')) return null
  const parts = msg.split(VERB)
  if (parts.length < 2) return null
  const words = parts.join('').replace(/[^a-z0-9]/g, '')
  if (words.length < 3) return null
  const body = parts.map((p) => esc(norm(p))).join('[\\s\\S]{0,160}?')
  const anchored = norm(parts[0]).length > 0
  try {
    return new RegExp((anchored ? '^' : '') + body)
  } catch {
    return null
  }
}

function sourceOf(e: JErr): Source {
  const text = norm(e.m)
  return {
    e,
    text,
    tokens: text.split(/[^a-z0-9_]+/).filter((t) => t.length >= 3),
    re: templateRe(e.m),
  }
}

function tokenHit(candidate: string, token: string): boolean {
  return new RegExp(`(^|[^a-z0-9_])${esc(token)}([^a-z0-9_]|$)`).test(candidate)
}

function scoreText(src: Source, c: string): number {
  const m = src.text
  if (c.length < 3 || m.length < 3) return 0
  if (c === m) return 1000
  if (m.length >= 6 && c.includes(m)) return 760 + Math.min(m.length, 60)
  if (c.length >= 8 && m.includes(c)) return 620
  if (src.re?.test(c)) return 520
  if (src.tokens.length >= 3) {
    const hit = src.tokens.filter((t) => tokenHit(c, t)).length
    const share = hit / src.tokens.length
    if (hit >= 3 && share >= 0.8) return 360 + share * 40
  }
  return 0
}

function fileMatch(a: string, b: string): boolean {
  const x = a.replace(/\\/g, '/')
  const y = b.replace(/\\/g, '/')
  if (x === y) return true
  if (x.endsWith('/' + y) || y.endsWith('/' + x)) return true
  const bx = x.slice(x.lastIndexOf('/') + 1)
  const by = y.slice(y.lastIndexOf('/') + 1)
  return bx === by && bx.includes('.')
}

function fnName(frame: string): string {
  const part = frame.split('|').pop() ?? frame
  return part.split(/[./]/).pop() ?? part
}

function stackScore(src: Source, rec: LogRecord): number {
  const siteFn = fnName(src.e.fn)
  let best = 0
  for (const frame of rec.stack) {
    if (!fileMatch(frame.file, src.e.f)) continue
    const delta = Math.abs(frame.line - src.e.l)
    const sameFn = !!frame.fn && !!siteFn && frame.fn.includes(siteFn)
    if (delta <= 2) best = Math.max(best, 2600 - delta * 30 + (sameFn ? 50 : 0))
  }
  return best
}

function pathMatch(pattern: string, actual: string): boolean {
  const p = pattern.replace(/\/+$/, '').split('/').filter(Boolean)
  const a = actual.split('?')[0].replace(/\/+$/, '').split('/').filter(Boolean)
  if (p.length !== a.length) return false
  return p.every((part, i) => part === a[i] || part.startsWith(':') || part === '*')
}

function contextScore(src: Source, rec: LogRecord, ctx: MatchContext): number {
  let score = 0
  if (rec.status !== undefined && src.e.c?.includes(String(rec.status))) score += 200
  if (rec.path) {
    for (const ref of src.e.r ?? []) {
      if (ref.k === 'rpc') continue
      const route = ctx.routes?.[ref.ri]
      if (route && pathMatch(route.p, rec.path)) {
        score += 240
        if (rec.method && route.m.toUpperCase() === rec.method) score += 40
        if (ctx.svc && route.svc === ctx.svc) score += 20
      }
    }
  }
  return score
}

function scoreSource(
  src: Source,
  rec: LogRecord,
  ctx: MatchContext,
  candidates: string[],
): { score: number; why: MatchWhy } {
  const stack = stackScore(src, rec)
  let best = 0
  for (const candidate of candidates) {
    best = Math.max(best, scoreText(src, candidate))
  }
  if (stack > 0) return { score: stack + contextScore(src, rec, ctx), why: 'stack' }
  if (best <= 0) return { score: 0, why: 'partial' }
  const score = best + contextScore(src, rec, ctx)
  return {
    score,
    why: best >= 950 ? 'exact' : best >= 500 ? 'template' : 'partial',
  }
}

function siteKey(e: JErr): string {
  return `${e.f}:${e.l}:${e.k}:${e.m}`
}

type RecordMatch = { hits: SiteHit[]; suggestion?: SiteHit }

function matchRecord(
  sources: Source[],
  rec: LogRecord,
  ctx: MatchContext,
): RecordMatch {
  const candidates = (rec.candidates.length ? rec.candidates : [rec.msg]).map(norm)
  let top: SiteHit | undefined
  for (const src of sources) {
    const { score, why } = scoreSource(src, rec, ctx, candidates)
    if (score <= 0) continue
    if (!top || score > top.score) top = { err: src.e, score, why }
  }
  if (!top) return { hits: [] }

  // The same message is often written at several sites. Return all of them so
  // the panel can show alternatives instead of hiding them behind "+N sites".
  const same = sources.filter((s) => s.e.m === top.err.m)
  const hits: SiteHit[] = []
  for (const src of same) {
    const { score, why } = scoreSource(src, rec, ctx, candidates)
    if (score <= 0) continue
    hits.push({ err: src.e, score, why })
  }
  hits.sort((a, b) => b.score - a.score || a.err.l - b.err.l)
  return { hits, suggestion: top }
}

function severity(kind: JErr['k']): number {
  return kind === 'panic' ? 0 : kind === 'response' ? 1 : kind === 'error' ? 2 : 3
}

function whyRank(why: MatchWhy): number {
  return why === 'stack' ? 0 : why === 'exact' ? 1 : why === 'template' ? 2 : 3
}

function recKey(rec: LogRecord): string {
  return [
    rec.candidates.join('\u0001'),
    rec.status ?? '',
    rec.path ?? '',
    rec.method ?? '',
    rec.stack.map((f) => `${f.file}:${f.line}`).join(','),
  ].join('\u0000')
}

export function matchLogs(
  errs: JErr[] | undefined,
  parsed: ParsedLogs,
  ctx: MatchContext = {},
): LogMatchResult {
  const records = parsed.records
  if (!records.length) return { groups: [], unknown: [], matchedRecords: 0, errorRecords: parsed.errorRecords }
  const sources = (errs ?? []).filter((e) => e.m.trim().length >= 3).map(sourceOf)

  const cache = new Map<string, RecordMatch>()
  const groups = new Map<
    string,
    {
      msg: string
      kind: JErr['k']
      codes: Set<string>
      hits: Map<string, SiteHit>
      siteCount: Map<string, { hit: SiteHit; count: number }>
      count: number
      errorCount: number
      levels: Set<string>
      statuses: Set<string>
      samples: GroupSample[]
      score: number
      why: MatchWhy
    }
  >()
  const unknown = new Map<string, { msg: string; count: number; levels: Set<string>; suggestion?: SiteHit }>()
  let matchedRecords = 0

  for (const rec of records) {
    const ck = recKey(rec)
    let result = cache.get(ck)
    if (!result) {
      result = matchRecord(sources, rec, ctx)
      cache.set(ck, result)
    }
    const top = result.hits[0]
    if (!top || top.score < THRESHOLD) {
      const msg = (rec.msg || rec.candidates[0] || rec.raw).slice(0, 220)
      const key = norm(msg).slice(0, 160) || 'unknown'
      const u = unknown.get(key) ?? { msg, count: 0, levels: new Set<string>() }
      u.count++
      if (rec.level) u.levels.add(rec.level)
      if (result.suggestion && (!u.suggestion || result.suggestion.score > u.suggestion.score)) {
        u.suggestion = result.suggestion
      }
      unknown.set(key, u)
      continue
    }

    matchedRecords++
    const msg = top.err.m
    const key = `${top.err.k}|${norm(msg)}`
    const g =
      groups.get(key) ??
      {
        msg,
        kind: top.err.k,
        codes: new Set<string>(),
        hits: new Map<string, SiteHit>(),
        siteCount: new Map<string, { hit: SiteHit; count: number }>(),
        count: 0,
        errorCount: 0,
        levels: new Set<string>(),
        statuses: new Set<string>(),
        samples: [],
        score: 0,
        why: 'partial',
      }

    g.count++
    if (rec.errorish) g.errorCount++
    if (rec.level) g.levels.add(rec.level)
    if (rec.status) g.statuses.add(String(rec.status))
    if (g.samples.length < 8) {
      g.samples.push({ ts: rec.ts, level: rec.level, msg: rec.msg, line: rec.line })
    }
    for (const hit of result.hits) {
      const prev = g.hits.get(siteKey(hit.err))
      if (!prev || hit.score > prev.score) g.hits.set(siteKey(hit.err), hit)
      const count = g.siteCount.get(siteKey(hit.err)) ?? { hit, count: 0 }
      count.count++
      g.siteCount.set(siteKey(hit.err), count)
      for (const code of hit.err.c ?? []) g.codes.add(code)
    }
    if (top.score > g.score) {
      g.score = top.score
      g.why = top.why
    }
    groups.set(key, g)
  }

  const out: ErrGroup[] = []
  for (const g of groups.values()) {
    const hits = [...g.hits.values()].sort(
      (a, b) => b.score - a.score || whyRank(a.why) - whyRank(b.why) || a.err.l - b.err.l,
    )
    const site =
      [...g.siteCount.values()].sort(
        (a, b) =>
          b.count - a.count ||
          b.hit.score - a.hit.score ||
          whyRank(a.hit.why) - whyRank(b.hit.why),
      )[0]?.hit.err ?? hits[0]?.err
    if (!site) continue
    out.push({
      key: `${g.kind}|${norm(g.msg)}`,
      msg: g.msg,
      kind: g.kind,
      codes: [...g.codes].sort(),
      hits,
      site,
      score: g.score,
      why: g.why,
      count: g.count,
      errorCount: g.errorCount,
      levels: [...g.levels],
      statuses: [...g.statuses].sort(),
      samples: g.samples,
    })
  }

  out.sort(
    (a, b) =>
      b.count - a.count ||
      severity(a.kind) - severity(b.kind) ||
      whyRank(a.why) - whyRank(b.why) ||
      a.msg.length - b.msg.length,
  )

  const unknowns = [...unknown.values()]
    .map((u) => ({ key: norm(u.msg), msg: u.msg, count: u.count, levels: [...u.levels], suggestion: u.suggestion }))
    .sort((a, b) => b.count - a.count || a.msg.length - b.msg.length)
    .slice(0, 300)

  return { groups: out, unknown: unknowns, matchedRecords, errorRecords: parsed.errorRecords }
}
