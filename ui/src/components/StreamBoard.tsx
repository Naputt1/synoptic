import { Code2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'

import { buttonVariants } from '@/components/ui/button'
import { AppLink } from '@/components/ui/link'
import { externalCalls } from '@/graph/scenes'
import { fnHref } from '@/lib/data'
import type { FuncFlow, Route, StreamMessage } from '@/lib/types'

import { EffectRows } from './EffectRows'
import { ExternalCalls } from './ExternalCalls'
import { MessageCard } from './MessageCard'
import { MessageSheet } from './MessageSheet'
import { TraceRows } from './TraceRows'
import { TriggeredBy } from './TriggeredBy'

const EMPTY_MESSAGES: StreamMessage[] = []

export function StreamBoard({
  route,
  flows,
  ri,
}: {
  route: Route
  flows?: Record<string, FuncFlow>
  ri: number
}) {
  const st = route.st
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState('')
  const [picked, setPicked] = useState<{ m: StreamMessage; event?: string } | null>(null)
  useEffect(() => {
    setQuery('')
    setSelected('')
    setPicked(null)
  }, [route.p, route.m])

  const isSse = st?.k === 'sse'
  const recv = st?.recv ?? EMPTY_MESSAGES
  const send = st?.send ?? EMPTY_MESSAGES
  const ext = externalCalls(route, flows)
  const known = useMemo(() => {
    const names = [
      ...new Set(
        [
          ...(st?.events ?? []),
          ...recv.map((m) => m.ev ?? ''),
          ...send.map((m) => m.ev ?? ''),
        ].filter(Boolean),
      ),
    ]
    return names.length || !isSse ? names : ['event stream']
  }, [isSse, st, recv, send])
  const eventOf = useCallback(
    (m: StreamMessage) => m.ev || (m.n && known.includes(m.n) ? m.n : ''),
    [known],
  )
  const uniq = (list: StreamMessage[]) => [
    ...new Map(list.map((m) => [`${m.t}|${m.n}|${m.ev}`, m])).values(),
  ]
  const exchanges = useMemo(
    () =>
      known.map((event) => ({
        event,
        inbound: recv.filter((m) => eventOf(m) === event),
        outbound: isSse ? send : send.filter((m) => eventOf(m) === event),
      })),
    [eventOf, isSse, known, recv, send],
  )
  const preamble = uniq(recv.filter((m) => !eventOf(m) && !/^[A-Z]$/.test(m.t ?? '')))
  const unmapped = uniq(send.filter((m) => !eventOf(m)))
  const q = query.trim().toLowerCase()
  const matches = (x: (typeof exchanges)[number]) => {
    if (!q) return true
    return [
      x.event,
      ...x.inbound.flatMap((m) => [m.n, m.t, m.e, ...(m.f ?? []).map((f) => f.n)]),
      ...x.outbound.flatMap((m) => [m.n, m.t, m.e, ...(m.f ?? []).map((f) => f.n)]),
      ...ext.filter((c) => c.ev === x.event).map((c) => c.n),
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase()
      .includes(q)
  }
  const filtered = exchanges.filter(matches)
  const defaultKey =
    exchanges[0]?.event ?? (preamble.length ? '__preamble' : unmapped.length ? '__unmapped' : '')
  const validSelected =
    selected === '__preamble'
      ? preamble.length > 0
      : selected === '__unmapped'
        ? unmapped.length > 0
        : exchanges.some((x) => x.event === selected)
  const active = validSelected ? selected : defaultKey
  const exchange = exchanges.find((x) => x.event === active)
  const selectedEvent = st?.evs?.find((e) => e.n === active)
  const sharedKey = !selectedEvent?.fk && isSse ? st?.hk : undefined
  const flowKey = selectedEvent?.fk ?? sharedKey
  const eventFlow = flowKey ? flows?.[flowKey] : undefined
  const sharedFlow = !selectedEvent?.fk && !!sharedKey && !!eventFlow
  const effects = eventFlow?.fx ?? []
  const eventCalls = active.startsWith('__') ? [] : ext.filter((x) => x.ev === active)
  const deeperCalls = eventCalls.filter(
    (x) => !effects.some((e) => e.l === x.l && (x.n === e.n || x.n.includes(e.n))),
  )
  const routeCalls = ext.filter((x) => !x.ev)
  const firstFrame = exchange
    ? [...exchange.inbound, ...exchange.outbound].find((m) => m.file)
    : undefined
  const handlerStage = route.ch[route.ch.length - 1]
  const handlerHref = flowKey
    ? fnHref(flowKey, { ri, si: route.ch.length - 1 })
    : handlerStage?.fk
      ? fnHref(handlerStage.fk, { ri, si: route.ch.length - 1 })
      : undefined
  const openFn = (key: string) => fnHref(key, { ri, si: Math.max(0, route.ch.length - 1) })
  const frameCount = exchange
    ? exchange.inbound.length + exchange.outbound.length
    : active === '__preamble'
      ? preamble.length
      : unmapped.length

  const listRows = (
    <>
      {preamble.length > 0 && (
        <button
          type="button"
          onClick={() => setSelected('__preamble')}
          className={`block w-full border-l-2 px-3 py-2 text-left transition-colors ${
            active === '__preamble'
              ? 'border-acc bg-acc/10'
              : 'hover:bg-panel2/60 border-transparent'
          }`}
        >
          <div className="text-ink font-mono text-[11px]">preamble</div>
          <div className="text-faint mt-0.5 text-[9.5px]">
            {preamble.length} frame{preamble.length === 1 ? '' : 's'} before the switch
          </div>
        </button>
      )}
      {preamble.length > 0 && <div className="border-line/50 border-t" />}
      {filtered.map((x) => {
        const calls = ext.filter((c) => c.ev === x.event).length
        return (
          <button
            key={x.event}
            type="button"
            onClick={() => setSelected(x.event)}
            className={`block w-full border-l-2 px-3 py-2 text-left transition-colors ${
              active === x.event ? 'border-acc bg-acc/10' : 'hover:bg-panel2/60 border-transparent'
            }`}
          >
            <div className="text-ink font-mono text-[11px] break-words">{x.event}</div>
            {(!isSse || calls > 0) && (
              <div className="text-faint mt-0.5 text-[9.5px]">
                {isSse ? '' : `${x.inbound.length} in · ${x.outbound.length} out`}
                {calls > 0 ? `${isSse ? '' : ' · '}${calls} call${calls === 1 ? '' : 's'}` : ''}
              </div>
            )}
          </button>
        )
      })}
      {unmapped.length > 0 && <div className="border-line/50 border-t" />}
      {unmapped.length > 0 && (
        <button
          type="button"
          onClick={() => setSelected('__unmapped')}
          className={`block w-full border-l-2 px-3 py-2 text-left transition-colors ${
            active === '__unmapped'
              ? 'border-acc bg-acc/10'
              : 'hover:bg-panel2/60 border-transparent'
          }`}
        >
          <div className="text-ink font-mono text-[11px]">unmapped sends</div>
          <div className="text-faint mt-0.5 text-[9.5px]">
            {unmapped.length} frame{unmapped.length === 1 ? '' : 's'} outside an event
          </div>
        </button>
      )}
      {!filtered.length && q && (
        <p className="text-faint px-3 py-6 text-center text-[10.5px] italic">
          no event matches “{query}”.
        </p>
      )}
      {!exchanges.length && !preamble.length && !unmapped.length && (
        <p className="text-faint px-3 py-6 text-[10.5px] italic">no event dispatch resolved.</p>
      )}
    </>
  )

  return (
    <div className="stream-board h-full">
      <aside className="stream-list border-line bg-panel/20 border-r">
        <div className="border-line sticky top-0 z-10 border-b bg-[var(--color-panel)] p-2">
          <div className="mb-2 flex items-baseline gap-2">
            <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">
              {exchanges.length} events
            </span>
            <span className="text-faint ml-auto text-[9.5px]">
              {isSse
                ? `${send.length} payload${send.length === 1 ? '' : 's'}`
                : `${recv.length} in · ${send.length} out`}
            </span>
          </div>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="filter event, type or field"
            className="border-line bg-canvas text-ink placeholder:text-faint focus:border-line2 w-full rounded-[4px] border px-2 py-1 text-[11px] outline-none"
          />
        </div>
        <div>{listRows}</div>
      </aside>

      <section className="stream-detail min-w-0">
        {active ? (
          <div className="mx-auto max-w-[760px] p-4">
            <div className="border-line flex flex-wrap items-start gap-3 border-b pb-3">
              <div className="min-w-0 flex-1">
                <div className="text-faint text-[9px] tracking-[0.14em] uppercase">
                  {active.startsWith('__') ? 'outside the event switch' : 'selected event'}
                </div>
                <div className="text-ink mt-0.5 font-mono text-[18px] leading-tight break-words">
                  {active === '__preamble'
                    ? 'preamble'
                    : active === '__unmapped'
                      ? 'unmapped sends'
                      : active}
                </div>
                <div className="text-faint mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[10px]">
                  <span>
                    {frameCount} frame{frameCount === 1 ? '' : 's'}
                  </span>
                  {exchange && !isSse && (
                    <span>
                      {exchange.inbound.length} client · {exchange.outbound.length} server
                    </span>
                  )}
                  {eventCalls.length > 0 && (
                    <span>
                      {eventCalls.length} external call{eventCalls.length === 1 ? '' : 's'}
                    </span>
                  )}
                  {firstFrame?.file && (
                    <span className="font-mono">
                      {firstFrame.file}:{firstFrame.l ?? 0}
                    </span>
                  )}
                </div>
              </div>
              {handlerHref && (
                <AppLink
                  href={handlerHref}
                  className={buttonVariants({ variant: 'outline', size: 'sm' })}
                >
                  <Code2 className="size-3" /> handler
                </AppLink>
              )}
            </div>

            {active === '__preamble' ? (
              <div className="mt-4">
                <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">
                  received before the switch
                </div>
                <div className="space-y-1">
                  {preamble.map((m, i) => (
                    <MessageCard key={`pre:${i}`} m={m} onOpen={() => setPicked({ m })} />
                  ))}
                </div>
              </div>
            ) : active === '__unmapped' ? (
              <div className="mt-4">
                <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">
                  sent outside a resolved event
                </div>
                <div className="space-y-1">
                  {unmapped.map((m, i) => (
                    <MessageCard key={`un:${i}`} m={m} onOpen={() => setPicked({ m })} />
                  ))}
                </div>
              </div>
            ) : exchange ? (
              <>
                <TriggeredBy event={selectedEvent} isSse={isSse} onFn={openFn} />

                {!isSse && (
                  <div className="mt-4">
                    <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">
                      client sends
                    </div>
                    {exchange.inbound.length ? (
                      <div className="space-y-1">
                        {exchange.inbound.map((m, i) => (
                          <MessageCard
                            key={`in:${i}`}
                            m={m}
                            event={exchange.event}
                            onOpen={() => setPicked({ m, event: exchange.event })}
                          />
                        ))}
                      </div>
                    ) : (
                      <p className="border-line text-faint rounded-[4px] border border-dashed px-3 py-2 text-[10.5px]">
                        server-initiated · no client frame for this event
                      </p>
                    )}
                  </div>
                )}

                {eventFlow?.flow?.length ? (
                  <div className="mt-4">
                    <div className="text-faint mb-1.5 flex items-center gap-2 text-[9.5px] tracking-[0.14em] uppercase">
                      <span>processing</span>
                      {sharedFlow ? (
                        <span className="text-faint text-[9px] tracking-normal normal-case">
                          shared stream loop
                        </span>
                      ) : null}
                      {flowKey ? (
                        <AppLink
                          href={fnHref(flowKey, { ri, si: Math.max(0, route.ch.length - 1) })}
                          className="text-acc ml-auto text-[9.5px] tracking-normal normal-case hover:underline"
                        >
                          full flow ›
                        </AppLink>
                      ) : null}
                    </div>
                    <TraceRows steps={eventFlow.flow} onFn={openFn} limit={sharedFlow ? 9 : 3} />
                  </div>
                ) : null}

                <div className="text-faint my-4 flex items-center gap-2 text-[9.5px] tracking-[0.14em] uppercase">
                  <span className="bg-line h-px flex-1" />
                  {isSse ? 'handler writes' : 'handler dispatches'}
                  <span className="bg-line h-px flex-1" />
                </div>

                <div>
                  <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">
                    {isSse ? 'payload sent' : 'server replies'}
                  </div>
                  {exchange.outbound.length ? (
                    <div className="space-y-1">
                      {exchange.outbound.map((m, i) => (
                        <MessageCard
                          key={`out:${i}`}
                          m={m}
                          event={exchange.event}
                          onOpen={() => setPicked({ m, event: exchange.event })}
                        />
                      ))}
                    </div>
                  ) : (
                    <p className="border-line text-faint rounded-[4px] border border-dashed px-3 py-2 text-[10.5px]">
                      no reply resolved.
                    </p>
                  )}
                  {isSse &&
                    exchange.outbound.length > 0 &&
                    exchange.outbound.every((m) => !m.ev) && (
                      <p className="text-faint mt-1.5 text-[10px]">
                        this payload shape is sent for every event.
                      </p>
                    )}
                </div>

                {effects.length > 0 && (
                  <div className="mt-6">
                    <div className="text-faint mb-2 text-[9.5px] tracking-[0.14em] uppercase">
                      after effects
                    </div>
                    <EffectRows items={effects} onFn={openFn} />
                  </div>
                )}
              </>
            ) : null}

            <div className="border-line mt-6 border-t pt-3">
              <div className="text-faint mb-2 text-[9.5px] tracking-[0.14em] uppercase">
                deeper calls
              </div>
              {deeperCalls.length ? (
                <ExternalCalls items={deeperCalls} onFn={openFn} />
              ) : (
                <p className="text-faint text-[10.5px] italic">
                  no deeper database, cache or outbound call is tied to this event.
                </p>
              )}
              {routeCalls.length > 0 && (
                <details className="border-line/50 mt-3 border-t pt-2">
                  <summary className="text-faint hover:text-mut cursor-pointer text-[10px]">
                    route-wide calls · {routeCalls.length}
                  </summary>
                  <div className="mt-2">
                    <ExternalCalls items={routeCalls} onFn={openFn} />
                  </div>
                </details>
              )}
            </div>
          </div>
        ) : (
          <p className="text-faint p-6 text-[11px] italic">
            no event dispatch resolved — open the flow to follow the handler.
          </p>
        )}
      </section>

      <MessageSheet
        message={picked?.m}
        event={picked?.event}
        open={picked !== null}
        onOpenChange={(v) => !v && setPicked(null)}
      />
    </div>
  )
}
