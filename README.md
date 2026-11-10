# synoptic

Reads a Go repo and renders its HTTP surface as one self-contained page: a **map of the router**, the **middleware pipeline of each route**, and the **code path of each handler** — all built from the source, no annotations required. WebSocket and server-sent-events routes are marked, and gRPC services get their own **RPC** tab.

```bash
cd ui && bun install && bun run build && cd ..

go run . -root /path/to/repo -out /path/to/repo/synoptic.html
open /path/to/repo/synoptic.html
```

The UI uses Bun 1.4: `bun run check` runs typecheck, lint and the format check; `bun fmt` writes formatting.

Flags: `-root` (default `.`), `-out` (default `<root>/synoptic.html`), `-json <file>` (dump the data for the dev UI).

## What you see

| level | what it shows |
| --- | --- |
| **router** | one column per path segment: server → groups → the routes that sit at that depth. Click a group to walk deeper, a route to open it. |
| **route** | the request walking its chain in execution order: middleware, then the handler, then the response. Click any step for its code. |
| **step** | the handler body as a flow: calls on the spine, `if err != nil` guards as exits with their status code, loops as `↺`, in-repo calls open their own flow. |
| **code** | the raw source, when you want the exact lines. |

Click a stage to open its flow. `⌘K` searches every route and stage, `Ctrl+Shift+E` opens the log trace workbench, and `esc` goes back. The URL holds the whole position (`#/r/0/s/3`), so a link to a middleware is a link.

Comments are the docs: a stage shows its Go doc comment, or `@Summary`/`@Description` from swaggo when present. With no comment, the page says so and lets the flow be the description.

## How it reads the code

- `load.go` parses the module (AST, imports, doc comments) and indexes every func with its calls. Nested `go.mod` files are respected, so a microservice workspace with one module per service resolves across them.
- `routes.go` walks the route registration: `gin`, `fiber`, `chi`, `gorilla/mux`, `net/http`, `echo`, including groups registered in other functions, middleware added with `Use`, handler factories called inline (`app.Get("/x", Activities(*u))`), factories stored in a variable, and `.PathPrefix(...).Subrouter()` / `.Methods(...)`.
- `flow.go` turns a function body into steps: calls, exits (an `if` whose body only returns), loops, switches, repeated calls folded to `×n`.
- `grpc.go` reads `Register<Service>Server` calls and the generated `_ServiceDesc`, then resolves the implementation and its interceptor chain for the RPC tab.
- `render.go` builds the JSON and inlines it into `ui/dist/index.html`.

The UI is React + TanStack Router + React Flow + shadcn-style primitives; the build is a single HTML file (no assets, no network).

## Multiple services in one checkout

A repo that holds several services — `jobs/`, `users/`, `projects/`, each with its own `package main` (with or without a `cmd/` inside) — gets a **SERVICE** switcher next to LEVEL: `all | jobs | users | projects`. There is no config file: entry points are found from `package main` + `func main`, and every route and gRPC service is attributed to the entry point whose directory contains the file that registers it. The topbar counts, the router map, route search and the RPC surface follow the choice; flows stay shared, because one function can be reached from more than one entry point.

The page stays unsplit when the split would lie: a route registered outside every entry point's directory keeps one surface, and an entry point that owns no route or RPC (a queue worker, say) is not offered.

## Limits

- Paths must be string literals; anything built at runtime is skipped rather than guessed (the tool exits 2 instead of writing an empty page).
- Flow depth stops at foreign packages. A call through an interface field resolves to its single concrete implementation (repository → store); an interface with several implementations stays unbound rather than guessed. Recursion stops at the breadcrumb trail.
- Stream schemas stop at dynamic values (`any`, channels, values crossing an interface). Socket.IO rooms are not treated as plain WebSockets.
- The service switcher is directory-based. A repo that wires every route from one central router, outside the entry point directories, keeps a single surface rather than a wrong split.
- gRPC needs the generated `_grpc.pb.go` registration and `_ServiceDesc` inside `-root`. The implementation is found even when its factory returns the service interface. Modules referenced by `go.work` from outside `-root`, gRPC-gateway, and Connect-Go registrations are not resolved yet.

## Timing overlay (`-trace`)

`-trace` takes a span export and attributes it back to each route's chain, so the
route view can answer "which middleware is the slow one".

```bash
go run . -root /path/to/repo -trace spans.json -out synoptic.html
# demo spans for Resm, generated from its own routes:
go run . -root ~/git/Resm -trace testdata/resm-spans.json -out out/resm.html
```

Two input shapes are accepted:

```json
[{ "method": "GET", "route": "/api/admin/flag", "stage": "cors.New", "ms": 0.42 }]
```

or an OTLP/JSON export (`resourceSpans[].scopeSpans[].spans[]`), where the route
comes from the `http.route` attribute and the duration from the span's start and
end times. Stage names are matched to chain entries by exact name, then by the
last dotted segment (`auth.AdminLoginCheck` ↔ `AdminLoginCheck`), then by
substring. Spans that match no chain entry are counted but not plotted.

With `-trace`, each stage shows its median, its p95, its share of the total (the
bar along the card's bottom edge), and the header names the slowest stage.

## Dev environment (`fog`)

`fog.json` has two scripts, so the dev loop is one command instead of three.

```bash
fog dev            # TUI
fog dev -d         # detached: better for agents and background work
fog ls             # instances + service state
fog logs <pid> -s ui --tail 40
fog kill <pid>
```

- **`dev`** — regenerates `ui/public/synoptic.json` from `$SYN_REPO` (default
  `~/git/Resm`), then runs Vite with HMR on a random free port. The generator runs
  inside the same service, so the data file always exists before the server answers.
- **`page`** — builds the UI, renders `out/index.html` for `$SYN_REPO`, and serves
  `out/` over HTTP, so every generated page is reachable next to it.

Point either script at another service with `SYN_REPO=/path/to/repo fog dev -d`.
Ports are randomised per instance (`ports.dev`, `ports.page`), and both use `tcp`
health checks — an `http` check against Vite reported `unhealthy` while the server
answered 200, so the probe is deliberately shallow.

## Documentation panel

Every route page carries a documentation band under the map, built from the source by `doc.go`:

| field | how it is read |
|---|---|
| path params | `:id`, `{id}`, `*rest` segments of the registered pattern |
| query | string literals in `Query`, `DefaultQuery`, `PostForm`, `Params`, `FormValue` |
| body | the struct behind `ShouldBindJSON` / `BodyParser` / `Decode`, with `json` names and `required` markers |
| responses | status codes and literal `gin.H{}` / `fiber.Map{}` keys per exit; net/http `WriteHeader` / `http.Error` and project helpers such as `utils.SendJSONResponse` count too, by signature or by the writer call inside them |
| auth | a middleware in the chain whose name looks like auth, jwt, session, token or guard (labelled as inferred) |
| data touched | `Table("…")` and `Model(&X{})` in the handler and its direct callees |
| owner | `git log -1` for the file: author, short sha, age |
| stream | the upgrade or `text/event-stream` proof, event names, and resolved frame shapes |
| example | a `curl`, `wscat -c …` for WS, or `curl -N …` for SSE, with the bearer header when auth is proven |

Anything the reader cannot prove stays empty and says so, rather than guessing.

## Error messages and tracing

The analyzer indexes every error message the code can emit: response bodies
(`gin.H{"error": …}`, `fiber.Map`, struct fields, `http.Error`, echo `e.Error`),
project helpers that write a response (messages resolved through their
parameters), `errors.New` / `fmt.Errorf` / `*.Errorf`, sentinel uses, structured
logs, and `panic` arguments. Format strings and concatenated messages are kept
as templates (`%s`, `%w`, `%v`), not evaluated.

- **Preview on the flow.** Status circles and exit badges in a handler's flow
  carry the messages that leave through them. Hover one to see the status code,
  the message, and its `file:line` (up to five; the rest show a count).
- **Trace real logs.** `Ctrl+Shift+E` opens a full-screen workbench. Paste or
  drop a log file; it reads JSON, NDJSON, logfmt, and plain lines with Go, zap,
  zerolog, logrus, or std-log prefixes, keeps panic stacks and stack frames
  attached to their record, and ignores timestamps and stack lines as match text.
- **Grouped results.** Every source message is one row with its occurrence count,
  levels, statuses, and sites. Expand a row to see each `file:line`, the best
  site, and sample log lines; filter by message or file, show only errors, sort
  by count/severity/recent order, or switch to **unknown** to inspect records that
  did not match.
- **Ambiguous messages.** The same text can be written in several places. The
  trace picks the site from a stack frame's `file.go:line`, then the status,
  method, or route path present in the paste; otherwise it opens the most
  frequent site and lists the rest.

Bodies that are only `err.Error()` or another dynamic value have no literal to
preview or match, and are left out rather than guessed. Paired with `-json`, the
index is the `errs` array in the data file.

## Streams

Streaming routes stay in the HTTP map and keep their method, but carry a `WS` or `SSE` badge. The router view has a protocol filter (all / http / ws / sse), and the search index includes protocol, event names, and frame fields.

- **WS** is proven by an upgrade call (`gorilla` `Upgrader.Upgrade`, `nhooyr`/`coder` `Accept`, `gobwas` `ws.Upgrade`, fiber `websocket.New`, melody `HandleRequest`), never by a client dial. The page lists the upgrade site, origin check, subprotocols, ping/pong, and frame shapes read with `ReadJSON` / `ReadMessage` + `json.Unmarshal` and written with `WriteJSON` / `WriteMessage`.
- **SSE** is proven by a `text/event-stream` content type or an `SSEvent` call; `event:` / `data:` lines plus `Flush` or `Stream` also count. Event names come from literals and from the named string type on the message's `Type` field, so constants such as `EventSyncStatus = "sync:status"` appear without hardcoding them.
- Payloads are expanded only when the value resolves to a named or inline struct in the module. A dynamic value stays as its source expression and is marked unresolved; the reader does not invent fields.
- Frames also come from `WriteMsgpack` / `ReadMsgpack` and generic `Unmarshal*` helpers, so encoders such as msgpack stay visible.
- When a handler dispatches on `switch msg.Type`, each case is followed to its handler function; the case constant becomes a received event and the handler's writes become sent frames with their resolved payloads.
- A WS route opens on the **events** view: a searchable event list on the left, then one selected event on the right. The inspector shows the client frame, the concrete case handler, a compact **processing** trace with branches, every reply the case emits (`query`, `ping`, `auth` with `auth_ok` / `auth_invalid`), and the side effects that follow. `preamble` and `unmapped sends` stay pinned as separate entries instead of mixing into the event list.
- **after effects** lists the reads, writes, and publishes in the selected handler. **deeper calls** keeps the database, cache, and outbound calls reached through helpers, without repeating the direct effects.
- Server-pushed events get a **triggered by** section. It follows `NewEvent`, `SSEvent`, and common publish/broadcast wrappers to the worker or route that produced the event, with `file:line` and call count. A client-triggered WS event has no such section because its client frame already shows the origin.
- Calls that cannot be tied to one event stay in a collapsed route-wide group.
- An SSE route uses the same list; because its events usually share one payload, the inspector shows that payload once and says it is sent for every event.
- Switch to the animated handler pipeline from the **flow** toggle. Clicking a frame in the inspector opens its full payload and source line.

The stream scan follows in-repo helpers up to depth 3, the same way the input scan does. A route with both an upgrade and an event-stream hint is reported as WS.

## RPC

gRPC services are not HTTP routes, so they get a separate **rpc** tab in the topbar. The list groups methods per service; opening one shows its unary or streaming kind, request and response types, interceptor chain, handler flow, and a `grpcurl` example.

- Detection starts at `Register<Service>Server` calls whose first parameter is a `grpc.ServiceRegistrar` (or a gRPC server type). The service name, methods, and stream directions come from the generated `<Service>_ServiceDesc`.
- The implementation is resolved from the second argument, including a factory such as `NewJobGRPCServer(...)`. Its method bodies feed the same flow renderer as HTTP handlers, so calls and exits stay clickable.
- Request, response, and stream types come from the generated service interface (`ServerStreamingServer[T]`, `ClientStreamingServer[T, R]`, `BidiStreamingServer[T, R]`), not from guessing the handler body.
- Interceptors are read from `grpc.NewServer(grpc.ChainUnaryInterceptor(...), grpc.StreamInterceptor(...))` and shown with the service.

Service and method counts also appear in the topbar stats and in the console report.
