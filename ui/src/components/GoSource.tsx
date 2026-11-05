import { useEffect, useRef } from 'react'
import { Fragment, type ReactNode } from 'react'

const KEYWORDS = new Set([
  'break','case','chan','const','continue','default','defer','else','fallthrough','for','func','go','goto','if','import','interface','map','package','range','return','select','struct','switch','type','var',
])

const LITERALS = new Set(['true', 'false', 'nil', 'iota'])

const TOKEN = /(\/\/[^\n]*)|("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`[^`]*`)|(\b\d+(?:\.\d+)?\b)|([A-Za-z_]\w*)/g

function highlight(line: string): ReactNode {
  const out: ReactNode[] = []
  let last = 0
  let key = 0
  for (const m of line.matchAll(TOKEN)) {
    const at = m.index ?? 0
    if (at > last) out.push(<Fragment key={key++}>{line.slice(last, at)}</Fragment>)
    const [text, comment, str, num, word] = m
    if (comment) out.push(<span key={key++} className="italic text-faint">{text}</span>)
    else if (str) out.push(<span key={key++} className="text-warn/90">{text}</span>)
    else if (num) out.push(<span key={key++} className="text-info">{text}</span>)
    else if (word && KEYWORDS.has(word)) out.push(<span key={key++} className="text-acc">{text}</span>)
    else if (word && LITERALS.has(word)) out.push(<span key={key++} className="text-info">{text}</span>)
    else out.push(<Fragment key={key++}>{text}</Fragment>)
    last = at + text.length
  }
  if (last < line.length) out.push(<Fragment key={key++}>{line.slice(last)}</Fragment>)
  return out
}

export function GoSource({ code, focus, focusLines }: { code: string; focus?: number; focusLines?: number[] }) {
  const hits = useRef<Record<number, HTMLDivElement | null>>({})
  const active = focusLines?.length ? focusLines : focus ? [focus] : []
  const activeKey = active.join(',')
  useEffect(() => {
    const el = activeKey ? hits.current[Number(activeKey.split(',')[0])] : undefined
    if (el) el.scrollIntoView({ block: 'center', behavior: 'smooth' })
  }, [activeKey])
  return (
    <pre className="overflow-auto rounded-[6px] border border-line bg-[#061114] p-3 text-[11.5px] leading-[1.65] text-ink/90">
      <code>
        {code.split('\n').map((line, i) => {
          const on = active.includes(i + 1)
          const above = active.includes(i)
          const below = active.includes(i + 2)
          const edge = on
            ? [
                'border-x border-acc/40 bg-acc/15',
                above ? '' : 'border-t',
                below ? '' : 'border-b',
                above && below ? '' : above ? 'rounded-b-[3px]' : below ? 'rounded-t-[3px]' : 'rounded-[3px]',
              ].join(' ')
            : ''
          return (
            <div
              key={i}
              ref={(el) => {
                hits.current[i + 1] = el
              }}
              className={'flex ' + edge}
            >
              <span className="mr-3 inline-block w-7 shrink-0 select-none text-right text-faint/60">{i + 1}</span>
              <span className="whitespace-pre">{highlight(line)}</span>
            </div>
          )
        })}
      </code>
    </pre>
  )
}
