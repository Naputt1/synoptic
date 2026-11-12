export type Call = {
  e: string
  p?: string
  d?: string
  f?: string
  l?: number
  lo?: boolean
  t?: string
  k?: string
}

export type Stage = {
  n: string
  k: string
  r?: string
  f?: string
  l?: number
  d?: string
  s?: string
  c?: Call[]
  p?: string
  x?: string
  fk?: string
  in?: Input[]
  chg?: boolean
}

export type DocField = { n: string; t?: string; tg?: string; rq?: boolean }
export type DocResp = { c: string; k?: string[] }

export type Evidence = { f: string; l: number; n?: string }

export type StreamMessage = {
  d: 'send' | 'recv'
  n?: string
  t?: string
  f?: DocField[]
  e?: string
  ex?: string
  ev?: string
  file?: string
  l?: number
}

export type StreamExt = {
  g: 'db' | 'cache' | 'net' | string
  n: string
  fk?: string
  f?: string
  l?: number
  ev?: string
}
export type EventOrigin = {
  n: string
  fk?: string
  f?: string
  l?: number
  k?: 'route' | 'worker' | 'background' | string
  via?: string
  m?: string
  p?: string
  ri?: number
  c?: number
}
export type StreamEvent = {
  n: string
  fn?: string
  fk?: string
  f?: string
  l?: number
  or?: EventOrigin[]
}
export type Effect = {
  k: 'read' | 'write' | 'publish' | string
  n: string
  b?: string
  s?: string
  l?: number
  fk?: string
  c?: number
}
export type StreamInfo = {
  k: 'ws' | 'sse'
  ev?: Evidence[]
  events?: string[]
  evs?: StreamEvent[]
  hk?: string
  send?: StreamMessage[]
  recv?: StreamMessage[]
  ext?: StreamExt[]
  sp?: string[]
  oc?: boolean
  hb?: string
  rt?: string
}

export type Input = {
  k: 'path' | 'query' | 'ctx' | 'header' | 'cookie' | 'form'
  n: string
  t?: string
  a: 'read' | 'set' | 'read+set' | 'unused' | 'undeclared' | 'dynamic' | 'bound'
  sb?: string
  si?: string
  sa?: string
  sas?: string[]
  rb?: string
  ri?: string
  ra?: string
  r?: number
  s?: number
  rq?: boolean
  e?: string
}

export type Doc = {
  path?: DocField[]
  query?: DocField[]
  in?: Input[]
  body?: DocField[]
  bt?: string
  resp?: DocResp[]
  auth?: boolean
  aw?: string
  tables?: string[]
  owner?: string
  commit?: string
  age?: string
  curl?: string
}

export type Route = {
  m: string
  p: string
  f: string
  l: number
  pr?: 'ws' | 'sse'
  svc?: string
  st?: StreamInfo
  ch: Stage[]
  doc?: Doc
  chg?: boolean
}

export type Middleware = {
  n: string
  p?: string
  f?: string
  l?: number
  d?: string
  s?: string
  c?: number
  ca?: number[]
  x?: string
}

export type Stats = {
  routes: number
  groups: number
  middleware: number
  documented: number
  packages: number
  funcs: number
  streams?: number
  ws?: number
  sse?: number
  services?: number
  rpcs?: number
}

export type ServiceMethod = {
  n: string
  p: string
  k: string
  rq?: string
  rs?: string
  d?: string
  f?: string
  l?: number
  fk?: string
  curl?: string
}

export type Service = {
  n: string
  p?: string
  f?: string
  l?: number
  d?: string
  svc?: string
  mw?: Stage[]
  methods: ServiceMethod[]
}

export type Step = {
  k: 'call' | 'exit' | 'if' | 'loop' | 'switch' | 'ret' | 'note' | 'continue'
  t: string
  rk?: 'nil' | 'error' | 'void'
  s?: string
  sl?: number
  ls?: number[]
  cd?: string
  cds?: string[]
  l?: number
  g?: string
  fk?: string
  ik?: string
  sk?: string
  f?: string
  c?: number
  b?: Step[]
  e?: Step[]
  rpc?: string
}

export type FuncFlow = {
  n: string
  p?: string
  f?: string
  l?: number
  d?: string
  s?: string
  flow?: Step[]
  in?: Input[]
  fx?: Effect[]
  rpc?: string
  trunc?: boolean
}

export type StageTiming = {
  i: number
  c: number
  m: number
  p95: number
  mx: number
  sh: number
}

export type RouteTiming = {
  spans: number
  total: number
  worst: number
  slow: number
  st: StageTiming[]
}

export type TypeMethod = {
  n: string
  k?: string
  sig?: string
  l?: number
}

export type TypeField = {
  n: string
  t: string
  tg?: string
  e?: boolean
}

export type SynType = {
  k: 'interface' | 'struct'
  n: string
  key: string
  pkg: string
  f?: string
  l?: number
  fields?: TypeField[]
  methods?: TypeMethod[]
  impls?: string[]
}

/** One entry point of a multi-service module, with what it owns. */
export type Svc = { n: string; d?: string; e?: string; routes: number; rpcs: number }

/** What the loader could not read or resolve, baked into the page. */
export type Diag = {
  parsed: number
  skipped: number
  read?: number
  parse?: number
  generated?: number
  build?: number
  skip?: SkippedFile[]
  calls: number
  resolved: number
  unresolved: number
  unres?: UnresolvedCall[]
  truncated?: number
}

export type SkippedFile = {
  p: string
  r: 'read' | 'parse' | 'generated' | 'build' | string
}

export type UnresolvedCall = {
  fn: string
  e: string
  f?: string
  l?: number
}

/** The -base view: the ref compared against, and what it touched. */
export type Change = {
  ref: string
  files: number
  routes: number
}

export type Data = {
  repo: string
  mod: string
  stats: Stats
  routes: Route[]
  svcs?: Svc[]
  services?: Service[]
  mw: Middleware[]
  flows: Record<string, FuncFlow>
  types?: Record<string, SynType>
  timing?: Record<string, RouteTiming>
  diag: Diag
  change?: Change
  redact?: boolean
}

declare global {
  interface Window {
    __SYNOPTIC__?: Data | null
  }
}
