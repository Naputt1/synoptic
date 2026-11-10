import { describe, expect, test } from 'bun:test'
import { matchLogs } from './errors'
import { parseLogs } from './logparse'
import type { JErr, Route } from './types'

const source = (over: Partial<JErr>): JErr => ({
  m: '',
  k: 'response',
  f: 'internal/auth.go',
  l: 10,
  fn: 'pkg|Login',
  ...over,
})

const route = (p: string): Route => ({
  m: 'POST',
  p,
  f: 'internal/login.go',
  l: 1,
  ch: [{ n: 'Login', k: 'handler', fk: 'pkg|Login' }],
})

describe('parseLogs', () => {
  test('reads a timestamped Go log and attaches its stack', () => {
    const parsed = parseLogs(
      [
        '2026/11/10 12:00:00 ERROR failed store login: invalid username or password',
        'main.(*StoreLogin)(0x1)',
        '\tinternal/login.go:83 +0x2',
        '\tmain.main()',
        '\tmain.go:10 +0x1',
      ].join('\n')
    )

    expect(parsed.records).toHaveLength(1)
    expect(parsed.records[0].msg).toBe('failed store login: invalid username or password')
    expect(parsed.records[0].level).toBe('error')
    expect(parsed.records[0].stack?.[0]).toMatchObject({
      fn: 'main.(*StoreLogin)(0x1)',
      file: 'internal/login.go',
      line: 83,
    })
  })

  test('reads ndjson, logfmt and short level names', () => {
    const parsed = parseLogs(
      [
        '{"level":"error","msg":"request failed","error":"Database error","status":500,"method":"POST","path":"/api/store/login"}',
        'level=warn msg="invalid store name" status=400 path=/api/store',
        '12:00:00 WRN cache miss key=session',
      ].join('\n')
    )

    expect(parsed.records).toHaveLength(3)
    expect(parsed.records[0].candidates.join(' ')).toContain('Database error')
    expect(parsed.records[0].status).toBe(500)
    expect(parsed.records[0].path).toBe('/api/store/login')
    expect(parsed.records[1].msg).toBe('invalid store name')
    expect(parsed.records[1].level).toBe('warn')
    expect(parsed.records[1].status).toBe(400)
    expect(parsed.records[2].level).toBe('warn')
  })

  test('keeps one record per line for very large pastes', () => {
    const line = '2026/11/10 12:00:00 ERROR failed store login: invalid username or password'
    const parsed = parseLogs(`${line}\n`.repeat(19_999))

    expect(parsed.records).toHaveLength(19_999)
    expect(parsed.truncated).toBe(false)
  })
})

describe('matchLogs', () => {
  const errs = [
    source({
      m: 'invalid username or password',
      c: ['401'],
      l: 83,
      r: [{ ri: 0, si: 0 }],
    }),
    source({
      m: 'Database error',
      c: ['500'],
      l: 88,
      k: 'log',
      r: [{ ri: 0, si: 0 }],
    }),
  ]

  test('uses a stack frame to pick the exact source site', () => {
    const parsed = parseLogs(
      [
        '2026/11/10 12:00:00 ERROR failed store login: invalid username or password',
        'main.(*StoreLogin)(0x1)',
        '\tinternal/auth.go:83 +0x2',
      ].join('\n')
    )
    const result = matchLogs(errs, parsed, {
      routes: [route('/api/store/login')],
    })

    expect(result.groups).toHaveLength(1)
    expect(result.groups[0].msg).toBe('invalid username or password')
    expect(result.groups[0].why).toBe('stack')
    expect(result.groups[0].site?.l).toBe(83)
    expect(result.unknown).toHaveLength(0)
  })

  test('matches errors nested in a JSON log line', () => {
    const parsed = parseLogs(
      '{"level":"error","msg":"request failed","status":500,"method":"POST","path":"/api/store/login","error":"Database error"}'
    )
    const result = matchLogs(errs, parsed, {
      routes: [route('/api/store/login')],
    })

    expect(result.groups).toHaveLength(1)
    expect(result.groups[0].msg).toBe('Database error')
    expect(result.groups[0].why).toBe('exact')
  })

  test('reports lines no source message explains', () => {
    const parsed = parseLogs('2026/11/10 12:00:00 ERROR something completely unknown happened')
    const result = matchLogs(errs, parsed, {})

    expect(result.groups).toHaveLength(0)
    expect(result.unknown).toHaveLength(1)
    expect(result.unknown[0].msg).toContain('something completely unknown')
  })
})
