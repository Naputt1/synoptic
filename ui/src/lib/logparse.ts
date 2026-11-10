/**
 * Turn pasted application logs into records the matcher can reason about.
 *
 * Real logs are rarely one clean message: Go's std logger prefixes a timestamp,
 * zap/zerolog write JSON, logrus writes logfmt, and a panic is a message plus a
 * stack. This parser understands those shapes, keeps stack frames attached to
 * their record, and never treats a stack trace as error text.
 */

export type StackFrame = {
  file: string
  line: number
  fn?: string
  raw: string
}

export type LogRecord = {
  /** 1-based line in the pasted text */
  line: number
  raw: string
  ts?: string
  level?: string
  /** primary human message, with timestamp and level removed */
  msg: string
  /** strings worth matching; usually msg and the structured error field */
  candidates: string[]
  status?: number
  method?: string
  path?: string
  stack: StackFrame[]
  errorish: boolean
}

export type ParsedLogs = {
  records: LogRecord[]
  /** input lines seen, including stack frames */
  lines: number
  /** input characters read after the cap */
  chars: number
  truncated: boolean
  errorRecords: number
}

export const LOG_MAX_CHARS = 4_000_000
export const LOG_MAX_LINES = 20_000
export const LOG_MAX_RECORDS = 20_000

export const emptyLogs = (): ParsedLogs => ({
  records: [],
  lines: 0,
  chars: 0,
  truncated: false,
  errorRecords: 0,
})

const ANSI = /\u001b\[[0-9;]*m/g
const TS_PREFIXES = [
  /^\d{4}\/\d{2}\/\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?\s+/,
  /^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?\s+/,
  /^\d{2}:\d{2}:\d{2}(?:[.,]\d+)?\s+/,
  /^\[[^\]]{8,}\]\s*/,
]
const LEVEL_PREFIX =
  /^(?:\[)?(trace|trc|debug|dbg|info|inf|warn|warning|wrn|error|err|fatal|ftl|panic|pnc|critical|crit|alert|emerg)(?:\])?(?:\s*[:|,-]\s*|\s+)/i
const ERROR_LEVEL = /^(warn|warning|wrn|error|err|fatal|ftl|panic|pnc|critical|crit|alert|emerg)$/i
const LEVEL_ALIAS: Record<string, string> = {
  trc: 'trace',
  dbg: 'debug',
  inf: 'info',
  wrn: 'warn',
  warning: 'warn',
  err: 'error',
  ftl: 'fatal',
  pnc: 'panic',
  crit: 'critical',
}

const normLevel = (s: string): string => {
  const v = low(s)
  return LEVEL_ALIAS[v] ?? v
}

const withoutTs = (s: string): string => {
  let rest = s
  for (const re of TS_PREFIXES) rest = rest.replace(re, '')
  return rest.trimStart()
}

const MSG_KEYS = new Set(['msg', 'message', 'event', 'error_message', 'log', '@message'])
const ERR_KEYS = new Set(['error', 'err', 'errors', 'reason', 'detail', 'details', 'exception'])
const LEVEL_KEYS = new Set(['level', 'lvl', 'severity', 'log_level'])
const TS_KEYS = new Set(['time', 'timestamp', 'ts', 'date', '@timestamp'])
const STATUS_KEYS = new Set(['status', 'status_code', 'statuscode', 'http_status', 'http_status_code'])
const METHOD_KEYS = new Set(['method', 'http_method', 'verb'])
const PATH_KEYS = new Set(['path', 'url', 'request_path', 'uri', 'route'])
const STACK_KEYS = new Set(['stack', 'stacktrace', 'stack_trace', 'trace', 'backtrace', 'caller'])

const clean = (s: string): string => s.replace(ANSI, '').replace(/\s+/g, ' ').trim()
const low = (s: string): string => s.toLowerCase()

function unquoteValue(s: string): string {
  if (s.length >= 2 && s[0] === s[s.length - 1] && (s[0] === '"' || s[0] === "'")) {
    const inner = s.slice(1, -1)
    try {
      return JSON.parse(`"${inner.replace(/\\'/g, "'")}"`) as string
    } catch {
      return inner.replace(/\\"/g, '"').replace(/\\\\/g, '\\')
    }
  }
  return s
}

/** Parse one logfmt line, keeping quoted values intact. */
function logfmt(line: string): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  const re = /([A-Za-z_][\w.]*)=("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\S+)/g
  for (let m = re.exec(line); m; m = re.exec(line)) {
    out[m[1]] = unquoteValue(m[2])
  }
  return Object.keys(out).length >= 2 ? out : undefined
}

function field(obj: Record<string, unknown>, keys: Set<string>, depth = 0): unknown {
  for (const [k, v] of Object.entries(obj)) {
    if (keys.has(low(k))) return v
  }
  if (depth >= 2) return undefined
  for (const v of Object.values(obj)) {
    if (!v || typeof v !== 'object' || Array.isArray(v)) continue
    const hit = field(v as Record<string, unknown>, keys, depth + 1)
    if (hit !== undefined) return hit
  }
  return undefined
}

/** Flatten an error value without turning a whole object into a message. */
function textOf(v: unknown, depth = 0): string {
  if (depth > 2 || v == null) return ''
  if (typeof v === 'string') return clean(v)
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  if (Array.isArray(v)) return v.map((x) => textOf(x, depth + 1)).filter(Boolean).join(': ')
  if (typeof v === 'object') {
    const o = v as Record<string, unknown>
    const hit = field(o, MSG_KEYS) ?? field(o, ERR_KEYS)
    return hit === undefined ? '' : textOf(hit, depth + 1)
  }
  return ''
}

function statusOf(v: unknown): number | undefined {
  if (typeof v === 'number' && v >= 100 && v <= 599) return v
  if (typeof v === 'string') {
    const m = v.match(/\b([1-5]\d\d)\b/)
    if (m) return Number(m[1])
  }
  return undefined
}

function methodOf(v: unknown): string | undefined {
  const s = typeof v === 'string' ? v.toUpperCase() : ''
  return /^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|CONNECT|TRACE)$/.test(s) ? s : undefined
}

function pathOf(v: unknown): string | undefined {
  if (typeof v !== 'string' || !v) return undefined
  try {
    if (/^https?:\/\//i.test(v)) {
      const u = new URL(v)
      return u.pathname + u.search
    }
  } catch {
    // not a URL
  }
  const m = v.match(/^[A-Z]+\s+(\/\S*)/)
  return m ? m[1] : v.startsWith('/') ? v : undefined
}

/** A line that is a stack frame, not text a human wrote. */
function parseFrame(line: string): StackFrame | null {
  const t = line.trim()
  let m = t.match(/^(.+?\.go):(\d+)(?::\d+)?(?:\s+\+0x[0-9a-f]+)?$/i)
  if (m) return { file: m[1], line: Number(m[2]), raw: line }
  m = t.match(/^(?:at\s+)?(?:.*?\()?([^()\s]+\.(?:go|js|mjs|cjs|ts|tsx|jsx|py|rb|php|java|kt)):(\d+)(?::\d+)?\)?$/i)
  if (m) return { file: m[1], line: Number(m[2]), raw: line }
  m = t.match(/^File "(.+)", line (\d+), in (.+)$/)
  if (m) return { file: m[1], line: Number(m[2]), fn: clean(m[3]), raw: line }
  return null
}

function looksLikeFn(line: string): string | undefined {
  const t = line.trim()
  if (!t || t.includes('.go:') || /^(goroutine|runtime\.|panic\(|\[signal)/.test(t)) return undefined
  if (t.startsWith('created by ')) return t.slice('created by '.length)
  if (t.includes(' ')) return undefined
  if (!t.includes('(') || !t.endsWith(')')) return undefined
  if (!/[./]/.test(t)) return undefined
  return t
}

function parseStackText(text: string): StackFrame[] {
  const frames: StackFrame[] = []
  let fn: string | undefined
  for (const line of text.split(/\r?\n/)) {
    const frame = parseFrame(line)
    if (frame) {
      frames.push(fn && !frame.fn ? { ...frame, fn } : frame)
      fn = undefined
      continue
    }
    const next = looksLikeFn(line)
    if (next) fn = next
  }
  return frames
}

function errorWords(s: string): boolean {
  const v = low(s)
  return /(?:error|fail|fatal|panic|warn|invalid|denied|unauthor|forbidden|missing|unable|refuse|timeout|could not|cannot|no such|not found|bad |expired|mismatch|unexpected|broken|reject)/.test(
    v,
  )
}

function candidatesOf(...values: string[]): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const v of values) {
    const s = clean(v).slice(0, 300)
    const k = low(s)
    if (s.length < 3 || seen.has(k)) continue
    seen.add(k)
    out.push(s)
    if (out.length >= 4) break
  }
  return out
}

function errorishOf(level: string | undefined, status: number | undefined, msg: string, err: string): boolean {
  return (
    (!!level && ERROR_LEVEL.test(level)) ||
    (status !== undefined && status >= 400) ||
    !!err ||
    errorWords(msg)
  )
}

function fromObject(obj: Record<string, unknown>, raw: string, line: number): LogRecord | undefined {
  const msg = textOf(field(obj, MSG_KEYS))
  const err = textOf(field(obj, ERR_KEYS))
  const levelRaw = textOf(field(obj, LEVEL_KEYS))
  let level = levelRaw ? normLevel(levelRaw) : undefined
  if (!level) {
    const lm = withoutTs(raw).match(LEVEL_PREFIX)
    if (lm) level = normLevel(lm[1])
  }
  const ts = textOf(field(obj, TS_KEYS)) || undefined
  const status = statusOf(field(obj, STATUS_KEYS))
  const method = methodOf(field(obj, METHOD_KEYS))
  const path = pathOf(field(obj, PATH_KEYS))
  if (!msg && !err) return undefined

  const stackValue = field(obj, STACK_KEYS)
  const stack =
    typeof stackValue === 'string'
      ? parseStackText(stackValue)
      : Array.isArray(stackValue)
        ? stackValue.flatMap((x) => (typeof x === 'string' ? parseStackText(x) : []))
        : []

  const primary = msg || err
  const pairs = msg && err && low(err) !== low(msg) ? [`${msg}: ${err}`] : []
  return {
    line,
    raw: raw.slice(0, 1200),
    ts,
    level,
    msg: primary,
    candidates: candidatesOf(primary, err, ...pairs),
    status,
    method,
    path,
    stack,
    errorish: errorishOf(level, status, primary, err),
  }
}

function fromPlain(input: string, raw: string, line: number): LogRecord {
  let text = clean(input)
  let ts: string | undefined
  let level: string | undefined
  let msg = text

  const fields = logfmt(text)
  if (fields) {
    const structured = fromObject(fields, raw, line)
    if (structured) return structured
  }

  for (const re of TS_PREFIXES) {
    const m = text.match(re)
    if (!m) continue
    ts = clean(m[0])
    text = text.slice(m[0].length)
    break
  }

  const lm = text.match(LEVEL_PREFIX)
  if (lm) {
    level = normLevel(lm[1])
    text = text.slice(lm[0].length)
  }
  const panic = text.match(/^(?:panic|fatal error):\s*(.*)$/i)
  if (panic) {
    level = 'panic'
    text = panic[1]
  }
  msg = clean(text) || clean(input)

  const statusMatch =
    text.match(/\b(?:status|status_code|statusCode|http_status|code)[=: ]+"?([1-5]\d\d)\b/) ||
    text.match(/\bHTTP\/[\d.]+\"?\s+([1-5]\d\d)\b/) ||
    text.match(/"(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+\S+\s+HTTP\/[\d.]+"\s+([1-5]\d\d)\b/)
  const methodMatch = text.match(/\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(\/\S*)/)
  const status = statusMatch ? Number(statusMatch[1]) : undefined
  const errorish = errorishOf(
    level,
    status,
    msg,
    /(?:^|\s)(?:error|err|exception)[=:]\s*\S+/i.test(text) ? 'error' : '',
  )

  return {
    line,
    raw: raw.slice(0, 1200),
    ts,
    level,
    msg,
    candidates: candidatesOf(msg, level && msg !== text ? text : ''),
    status,
    method: methodMatch?.[1],
    path: pathOf(methodMatch?.[2] ?? ''),
    stack: [],
    errorish,
  }
}

export function parseLogs(input: string): ParsedLogs {
  if (!input.trim()) return emptyLogs()
  const truncatedChars = input.length > LOG_MAX_CHARS
  const text = truncatedChars ? input.slice(0, LOG_MAX_CHARS) : input
  const allLines = text.split(/\r?\n/)
  const truncatedLines = allLines.length > LOG_MAX_LINES
  const lines = truncatedLines ? allLines.slice(0, LOG_MAX_LINES) : allLines

  const records: LogRecord[] = []
  let current: LogRecord | undefined
  let pendingFn: string | undefined
  let truncatedRecords = false

  const push = (rec: LogRecord) => {
    if (records.length >= LOG_MAX_RECORDS) {
      truncatedRecords = true
      return
    }
    records.push(rec)
    current = rec
  }

  const trimmed = text.trim()
  if (trimmed.startsWith('[')) {
    try {
      const arr = JSON.parse(trimmed) as unknown
      if (Array.isArray(arr)) {
        for (let i = 0; i < arr.length; i++) {
          const item = arr[i]
          if (!item || typeof item !== 'object' || Array.isArray(item)) continue
          const rec = fromObject(item as Record<string, unknown>, JSON.stringify(item), i + 1)
          if (rec) push(rec)
        }
        const errorRecords = records.filter((r) => r.errorish).length
        return {
          records,
          lines: allLines.length,
          chars: text.length,
          truncated: truncatedChars || truncatedLines || truncatedRecords,
          errorRecords,
        }
      }
    } catch {
      // not a JSON array; line parsing below handles it
    }
  }

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i]
    const t = raw.trim()
    if (!t) continue

    const frame = parseFrame(raw)
    if (frame) {
      if (current) current.stack.push(pendingFn && !frame.fn ? { ...frame, fn: pendingFn } : frame)
      pendingFn = undefined
      continue
    }
    if (/^(goroutine \d+|created by |runtime\.|\[signal |exit status )/.test(t)) continue

    const fn = looksLikeFn(t)
    if (fn) {
      pendingFn = fn
      continue
    }
    if (raw.startsWith('\t') || /^\s{2,}/.test(raw)) {
      // An indented continuation is usually a wrapped stack/message, not a new
      // event. Keep it out of the candidate set.
      continue
    }

    let rec: LogRecord | undefined
    if (t.startsWith('{') && t.endsWith('}')) {
      try {
        const obj = JSON.parse(t) as unknown
        if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
          rec = fromObject(obj as Record<string, unknown>, raw, i + 1)
        }
      } catch {
        // plain text that happens to start with a brace
      }
    }
    rec ??= fromPlain(raw, raw, i + 1)
    push(rec)
  }

  return {
    records,
    lines: allLines.length,
    chars: text.length,
    truncated: truncatedChars || truncatedLines || truncatedRecords,
    errorRecords: records.filter((r) => r.errorish).length,
  }
}
