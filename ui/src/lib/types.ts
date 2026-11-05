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
}

export type DocField = { n: string; t?: string; tg?: string; rq?: boolean }
export type DocResp = { c: string; k?: string[] }

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
  ch: Stage[]
  doc?: Doc
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
}

export type Step = {
  k: 'call' | 'exit' | 'if' | 'loop' | 'switch' | 'ret' | 'note'
  t: string
  s?: string
  sl?: number
  ls?: number[]
  cd?: string
  l?: number
  g?: string
  fk?: string
  ik?: string
  sk?: string
  f?: string
  c?: number
  b?: Step[]
  e?: Step[]
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

export type Data = {
  repo: string
  mod: string
  stats: Stats
  routes: Route[]
  mw: Middleware[]
  flows: Record<string, FuncFlow>
  types?: Record<string, SynType>
  timing?: Record<string, RouteTiming>
}

declare global {
  interface Window {
    __SYNOPTIC__?: Data | null
  }
}
