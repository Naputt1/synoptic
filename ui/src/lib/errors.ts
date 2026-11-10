import type { JErr, Route } from './types'

/** One message read out of whatever the user pasted, best candidates first. */
export type ErrMatch = {
  msg: string
  kind: JErr['k']
  codes: string[]
  sites: JErr[]
  /** the site the pasted text points at: stack frame, route path, or the first */
  site: JErr
  score: number
  why: 'exact' | 'template' | 'partial'
}

const ANSI = /\u001b\[[0-9;]*m/g
const VERB = /%(?:\[[0-9]+\])?[-+# 0-9'.]*[sdvqtxXfFeEgGwp]/g
const PICKS = /^(error|err|message|msg|detail|reason|description)$/i

const norm = (s: string) => s.replace(ANSI, '').toLowerCase().replace(/\s+/g, ' ').trim()
const unquote = (s: string) => s.replace(/^["'`]+|["'`]+$/g, '').trim()
const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

function pickStrings(v: unknown, out: string[], depth: number) {
  if (depth > 4) return
  if (typeof v === 'string') {
    if (v.trim()) out.push(v)
    return
  }
  if (Array.isArray(v)) {
    for (const x of v.slice(0, 4)) pickStrings(x, out, depth + 1)
    return
  }
  if (v && typeof v === 'object') {
    for (const [k, val] of Object.entries(v)) if (PICKS.test(k)) pickStrings(val, out, depth + 1)
  }
}

/**
 * The lines worth matching: JSON log fields first, then the whole log line
 * minus stack frames, then the raw paste. A plain message is just one line.
 */
export function errorCandidates(text: string): string[] {
  const raw = text.trim()
  if (!raw) return []
  const out: string[] = []
  try {
    pickStrings(JSON.parse(raw), out, 0)
  } catch {
    // not JSON; the line pass below is the whole job
  }
  for (const line of raw.split(/\r?\n/)) {
    const t = line.trim()
    if (!t) continue
    if (/^(at |file "|created by |goroutine \d|stack trace|traceback|\^+$)/i.test(t)) continue
    if (/^[\w./-]+\.go:\d+(?::\d+)?$/.test(t)) continue
    out.push(t.replace(/^panic:\s*/i, '').replace(/^error:\s*/i, ''))
  }
  out.push(raw)
  const seen = new Set<string>()
  return out
    .map(unquote)
    .filter((s) => {
      const k = norm(s)
      if (!k || seen.has(k)) return false
      seen.add(k)
      return true
    })
    .slice(0, 40)
}

/** A printf-style message as a matcher; `%w` and `%d` become anything. */
function template(msg: string): RegExp | null {
  if (!msg.includes('%')) return null
  const parts = msg.split(VERB)
  if (parts.length < 2) return null
  try {
    return new RegExp(parts.map(esc).join('[\\s\\S]*?'))
  } catch {
    return null
  }
}

function scoreOne(candidate: string, e: JErr): number {
  const c = norm(candidate)
  const m = norm(e.m)
  if (m.length < 4) return 0
  if (c === m) return 130
  if (c.includes(m)) return 100 + Math.min(m.length, 40) / 100
  if (m.includes(c) && c.length >= 8) return 90
  const re = template(m)
  if (re?.test(c)) return 80
  const tokens = m.split(/[^a-z0-9_]+/).filter((t) => t.length >= 3)
  if (tokens.length < 3) return 0
  const hit = tokens.filter((t) => new RegExp(`(^|[^a-z0-9_])${esc(t)}([^a-z0-9_]|$)`).test(c)).length
  const share = hit / tokens.length
  if (hit >= 2 && share >= 0.8) return 50 + share * 10
  return 0
}

const severity = (k: JErr['k']) =>
  k === 'panic' ? 0 : k === 'response' ? 1 : k === 'error' ? 2 : 3

/** Pick the site the pasted log points at, not just the first write of the message. */
function chooseSite(sites: JErr[], text: string, ctx: MatchContext): JErr {
  if (sites.length === 1) return sites[0]
  const hay = text.replace(/\\/g, '/')
  const locs: { file: string; line: number }[] = []
  const re = /([\w./-]+\.go):(\d+)/g
  for (let m = re.exec(hay); m; m = re.exec(hay)) {
    locs.push({ file: m[1].replace(/^.*?\/(internal|app|pkg|cmd)\//, '$1/'), line: Number(m[2]) })
  }
  const names = new Set<string>()
  for (const line of text.split(/\r?\n/)) {
    if (!/^\s+/.test(line)) continue
    const call = line.match(/([A-Za-z_]\w*)\)?\(/)
    if (call) names.add(call[1])
  }

  let best = sites[0]
  let top = 0
  for (const site of sites) {
    let score = 0
    const file = site.f.replace(/\\/g, '/')
    for (const loc of locs) {
      if (file.endsWith(loc.file) || loc.file.endsWith(file))
        score = Math.max(score, 1000 - Math.min(400, Math.abs(site.l - loc.line)))
    }
    const short = site.fn.split('|').pop() ?? ''
    if (names.has(short)) score += 500
    else if (short.length >= 4) {
      // a log line often names the function in prose: "failed store login"
      const low = short.toLowerCase()
      const spaced = short.replace(/([a-z0-9])([A-Z])/g, '$1 $2').toLowerCase()
      if (hay.toLowerCase().includes(low) || (spaced !== low && hay.toLowerCase().includes(spaced)))
        score += 300 + Math.min(200, low.length * 12)
    }
    for (const ref of site.r ?? []) {
      const route = ref.k === 'rpc' ? undefined : ctx.routes?.[ref.ri]
      if (route && hay.includes(route.p)) score += 300
      if (route && ctx.svc && route.svc === ctx.svc) score += 20
    }
    if (score > top) {
      top = score
      best = site
    }
  }
  return best
}

export type MatchContext = { routes?: Route[]; svc?: string }

export function matchErrors(
  errs: JErr[] | undefined,
  text: string,
  ctx: MatchContext = {},
): ErrMatch[] {
  if (!errs?.length || !text.trim()) return []
  const candidates = errorCandidates(text)
  if (!candidates.length) return []

  const best = new Map<string, { score: number; why: ErrMatch['why'] }>()
  for (const e of errs) {
    let top = 0
    for (const c of candidates) {
      const s = scoreOne(c, e)
      if (s > top) top = s
    }
    if (!top) continue
    const why: ErrMatch['why'] = top >= 90 ? 'exact' : top >= 80 ? 'template' : 'partial'
    const prev = best.get(e.m)
    if (!prev || top > prev.score) best.set(e.m, { score: top, why })
  }

  const out: ErrMatch[] = []
  for (const [msg, b] of best) {
    const sites = errs.filter((e) => e.m === msg).sort((a, b2) => a.l - b2.l)
    out.push({
      msg,
      kind: sites[0].k,
      codes: [...new Set(sites.flatMap((e) => e.c ?? []))].sort(),
      sites,
      site: chooseSite(sites, text, ctx),
      score: b.score,
      why: b.why,
    })
  }
  return out
    .sort(
      (a, b) =>
        b.score - a.score ||
        severity(a.kind) - severity(b.kind) ||
        a.msg.length - b.msg.length,
    )
    .slice(0, 12)
}
