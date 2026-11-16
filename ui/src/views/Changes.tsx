import type { ReactNode } from 'react'

import { ScopeCard } from '@/components/ScopeCard'
import { Badge } from '@/components/ui/badge'
import { AppLink } from '@/components/ui/link'
import { changeCounts, changeMode, changeTitle, headLabel, refLabel } from '@/lib/change'
import { useData } from '@/lib/context'
import { routeHref } from '@/lib/data'
import type { ChangeMod, ChangeRoute } from '@/lib/types'

function RouteRow({
  route,
  ri,
  tone,
  why,
}: {
  route: ChangeRoute
  ri: number
  tone: 'accent' | 'bad' | 'warn'
  why?: string[]
}) {
  const body = (
    <>
      <code
        className={`w-12 shrink-0 text-[10px] font-semibold ${
          tone === 'accent' ? 'text-acc' : tone === 'bad' ? 'text-bad' : 'text-warn'
        }`}
      >
        {route.m}
      </code>
      <span className="text-ink min-w-0 flex-1 truncate">{route.p}</span>
      {route.svc && <span className="text-faint shrink-0 text-[10px]">{route.svc}</span>}
      {why && why.length > 0 && (
        <span className="text-faint shrink-0 text-[10px]">{why.join(', ')}</span>
      )}
      <Badge variant={tone}>
        {tone === 'accent' ? 'added' : tone === 'bad' ? 'removed' : 'changed'}
      </Badge>
      {ri >= 0 && <span className="text-faint shrink-0 text-[9px]">open ›</span>}
    </>
  )
  if (ri >= 0) {
    return (
      <AppLink
        href={routeHref(ri)}
        className="hover:bg-panel2 flex items-baseline gap-2 px-3 py-1.5 text-[11px]"
      >
        {body}
      </AppLink>
    )
  }
  return <div className="flex items-baseline gap-2 px-3 py-1.5 text-[11px]">{body}</div>
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

const fileTone = (status: string): 'accent' | 'bad' | 'warn' | 'default' => {
  if (status === 'A') return 'accent'
  if (status === 'D') return 'bad'
  if (status === 'R' || status === 'C') return 'warn'
  return 'default'
}

export function ChangesView() {
  const data = useData()
  const change = data.change
  if (!change) {
    return (
      <div className="flex h-full flex-col overflow-auto">
        <ScopeCard
          title="changes"
          badge="git"
          meta="no comparison baked into this page"
          note="run synoptic with -base <ref>, -base <from> -to <to>, or -pr <number> to compare two sides."
        />
        <div className="p-4">
          <div className="border-line bg-panel/40 text-faint rounded-[6px] border px-3 py-6 text-center text-[11px]">
            this report describes one tree. rerun with a comparison to see commits, files and the
            route diff here.
          </div>
        </div>
      </div>
    )
  }

  const counts = changeCounts(change)
  const added = change.added ?? []
  const removed = change.removed ?? []
  const modified = change.modified ?? []
  const commits = change.commits ?? []
  const files = change.fileList ?? []
  const routeIndex = (r: ChangeRoute) =>
    data.routes.findIndex((x) => x.m === r.m && x.p === r.p && (x.svc ?? '') === (r.svc ?? ''))

  return (
    <div className="flex h-full flex-col overflow-auto">
      <ScopeCard
        title={`changes · ${changeTitle(change)}`}
        badge={change.mode ?? 'worktree'}
        meta={`${counts.added} added · ${counts.removed} removed · ${counts.modified} changed · ${change.files} ${change.files === 1 ? 'file' : 'files'} touched · ${counts.commits} ${counts.commits === 1 ? 'commit' : 'commits'}`}
        note={
          change.mode === 'pr'
            ? 'the base side is the merge-base of the pull request, so routes added on the base branch after the fork do not appear as removals.'
            : change.mode === 'range'
              ? 'both sides are commits; uncommitted edits in the checkout are not part of this comparison.'
              : 'the base side is read-only; file-touch marks and the route diff are best-effort.'
        }
      />

      <div className="grid gap-3 p-4 lg:grid-cols-2">
        <section className="border-line bg-panel/40 rounded-[6px] border lg:col-span-2">
          <div className="border-line text-mut flex items-baseline justify-between border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
            <span>routes</span>
            <span className="text-faint tracking-normal normal-case">
              {counts.added} added · {counts.removed} removed · {counts.modified} changed
            </span>
          </div>
          <div className="border-line/60 flex flex-wrap items-baseline gap-2 border-b px-3 py-2 text-[11px]">
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

          <Group label="added" count={added.length} empty="no route was added">
            {added.map((r) => (
              <RouteRow
                key={`add:${r.m}:${r.p}:${r.svc}`}
                route={r}
                ri={routeIndex(r)}
                tone="accent"
              />
            ))}
          </Group>
          <Group label="removed" count={removed.length} empty="no route was removed">
            {removed.map((r) => (
              <RouteRow key={`rm:${r.m}:${r.p}:${r.svc}`} route={r} ri={routeIndex(r)} tone="bad" />
            ))}
          </Group>
          <Group
            label="changed"
            count={modified.length}
            empty="no route kept its identity but changed"
          >
            {modified.map((r: ChangeMod) => (
              <RouteRow
                key={`mod:${r.m}:${r.p}:${r.svc}`}
                route={r}
                ri={routeIndex(r)}
                tone="warn"
                why={r.why}
              />
            ))}
          </Group>
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
              <div key={c.sha} className="flex items-baseline gap-2 px-3 py-1.5 text-[11px]">
                <code className="text-acc shrink-0 text-[10px]" title={c.sha}>
                  {refLabel(c.sha)}
                </code>
                <span className="text-ink min-w-0 flex-1 truncate" title={c.subject}>
                  {c.subject}
                </span>
                <span className="text-faint shrink-0 text-[10px]">{c.author}</span>
                <span className="text-faint shrink-0 text-[10px]">{c.date.slice(0, 10)}</span>
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
              <div
                key={`${f.status}:${f.path}`}
                className="flex items-baseline gap-2 px-3 py-1.5 text-[11px]"
              >
                <Badge variant={fileTone(f.status)}>{f.status}</Badge>
                <code className="text-ink min-w-0 flex-1 truncate" title={f.path}>
                  {f.path}
                </code>
              </div>
            ))}
          </div>
          {(change.filesMore ?? 0) > 0 && (
            <div className="text-faint border-line/60 border-t px-3 py-1.5 text-[10px]">
              showing {files.length} of {files.length + (change.filesMore ?? 0)}
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
