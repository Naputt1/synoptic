import type { ReactElement } from 'react'
import { Code2, Copy, ExternalLink, FileCode2, Link2, Route, Terminal, Type } from 'lucide-react'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from '@/components/ui/context-menu'
import { absoluteLink, copyValue, showSource } from '@/lib/actions'
import { goto } from '@/lib/nav'
import type { SData } from './scenes'

const KIND: Record<string, string> = {
  root: 'server',
  group: 'group',
  route: 'route',
  request: 'request',
  stage: 'middleware',
  call: 'call',
  exit: 'exit',
  loop: 'loop',
  response: 'response',
  return: 'return',
  input: 'input',
  note: 'control',
}

function openLabel(d: SData) {
  if (d.kind === 'root') return 'Back to router'
  if (d.kind === 'group') return 'Open group'
  if (d.kind === 'route') return 'Open route'
  if (d.kind === 'stage') return 'Open step'
  return 'Open function'
}

/** Right-click actions for one graph card. Rendered inside CardMenu's root. */
export function NodeMenu({ d }: { d: SData }) {
  const href = typeof d.href === 'string' ? d.href : undefined
  const line = typeof d.line === 'number' ? d.line : undefined
  const file = typeof d.file === 'string' ? d.file : undefined
  const path =
    typeof d.path === 'string'
      ? d.path
      : typeof d.sub === 'string' && d.sub.startsWith('/')
        ? d.sub
        : undefined
  const curl = typeof d.curl === 'string' ? d.curl : undefined
  const source = Boolean(d.source && line)
  const ref = file && line ? `${file}:${line}` : undefined
  const heading =
    d.kind === 'route' && d.method && d.full ? `${d.method} ${d.full}` : d.title

  return (
    <ContextMenuContent className="w-[224px]">
      <ContextMenuLabel className="truncate">
        {KIND[d.kind] ?? d.kind} <span className="text-faint">·</span>{' '}
        <span className="normal-case tracking-normal text-mut">{heading}</span>
      </ContextMenuLabel>
      <ContextMenuSeparator />
      {href && (
        <ContextMenuItem onSelect={() => goto(href)}>
          <ExternalLink /> {openLabel(d)}
        </ContextMenuItem>
      )}
      {source && (
        <ContextMenuItem
          onSelect={() => showSource(line!, d.lines as number[] | undefined, d.startLine as number | undefined)}
        >
          <Code2 /> View source
        </ContextMenuItem>
      )}
      {(href || source) && <ContextMenuSeparator />}
      <ContextMenuSub>
        <ContextMenuSubTrigger>
          <Copy /> Copy
        </ContextMenuSubTrigger>
        <ContextMenuSubContent className="w-[196px]">
          {href && (
            <ContextMenuItem onSelect={() => void copyValue(absoluteLink(href), 'link')}>
              <Link2 /> Link
            </ContextMenuItem>
          )}
          <ContextMenuItem onSelect={() => void copyValue(d.title, 'name')}>
            <Type /> Name
          </ContextMenuItem>
          {path && (
            <ContextMenuItem onSelect={() => void copyValue(path, 'path')}>
              <Route /> Path
            </ContextMenuItem>
          )}
          {ref && (
            <ContextMenuItem onSelect={() => void copyValue(ref, 'source ref')}>
              <FileCode2 /> Source ref
            </ContextMenuItem>
          )}
          {curl && (
            <ContextMenuItem onSelect={() => void copyValue(curl, 'cURL')}>
              <Terminal /> cURL
            </ContextMenuItem>
          )}
        </ContextMenuSubContent>
      </ContextMenuSub>
    </ContextMenuContent>
  )
}

/** Wrap a card so right-click opens its menu; the card keeps its own handlers. */
export function CardMenu({ data, children }: { data: SData; children: ReactElement }) {
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <NodeMenu d={data} />
    </ContextMenu>
  )
}
