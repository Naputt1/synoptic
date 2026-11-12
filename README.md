# synoptic

Reads a Go repo and renders its HTTP surface as one self-contained page: a **map of the router**, the **middleware pipeline of each route**, and the **code path of each handler** — all built from the source, no annotations required. WebSocket and server-sent-events routes are marked, and gRPC services get their own **RPC** tab.

```bash
cd ui && bun install && bun run build && cd ..

go run . -root /path/to/repo -out /path/to/repo/synoptic.html
open /path/to/repo/synoptic.html
```

The UI uses Bun 1.4: `bun run check` runs typecheck, lint and the format check; `bun fmt` writes formatting.

`ui/dist/index.html` is a Bun build artifact, not a committed file, and `main.go` embeds it with `//go:embed`. It must exist before `go build` or `go run`, which is why a fresh clone runs the UI build first. Without it the build fails, or the binary carries an empty page.

Flags: `-root` (default `.`), `-out` (default `<root>/synoptic.html`), `-json <file>` (dump the data for the dev UI), `-html` (default `true`; `-html=false` prints the console report only), `-trace <file>` (overlay timings from a span export), `-typed` (default `false`; type-check the repo to resolve calls the syntax pass misses — slower, and the repo must type-check), `-base <ref>` (mark routes and stages whose files changed since a git ref; best-effort, never fails the run), `-no-source` (omit raw source from the page, keeping docs, files and line numbers), `-version` (print `synoptic <version>` and exit).

The default analysis path uses only the Go standard library. `-typed` is the one
third-party Go dependency, `golang.org/x/tools`, and only that mode calls it.

## Checks

CI runs the same commands:

```bash
# ui
cd ui && bun install --frozen-lockfile
bun run typecheck
bun run lint

# go (from the repo root, after the UI build)
go build ./...
pkgs=$(go list ./... | grep -v '/node_modules/')
go vet $pkgs
go test $pkgs
./scripts/check-floor.sh
```

`make check` runs both halves plus the Go floor check, and `make build` builds the UI and then the binary.

Go 1.26 or newer is required: `go.mod` declares `go 1.26.0` because the optional
`-typed` mode depends on `golang.org/x/tools` v0.52.0, whose own module requires
Go 1.26. `go.floor` records the supported floor (`1.26`) and
`scripts/check-floor.sh` fails when the `go` directive no longer starts with it,
so a future bump is a reviewed change instead of a silent Renovate side effect.
CI runs the same check.

## Corpus

The corpus is a pinned set of real repositories the analyzer is measured
against. `testdata/corpus.json` lists them, and `testdata/corpus/baseline.json`
is the committed floor. The runner lives behind the `corpus` build tag, so
`go test ./...` stays offline and the nightly workflow is the only automatic
run:

```bash
# Build the UI first: the harness builds the analyzer, which embeds ui/dist.
make ui
make corpus
```

The runner shallow-fetches each public repo at its full commit SHA into
`.corpus/<name>` (gitignored), reuses a valid clone, and runs the analyzer with
`-html=false`. It prints one table row per repo and writes the same table to
`.corpus/report.txt`. Local repos are read from `SYN_CORPUS_LOCAL` (default
`$HOME/git`); a missing local repo, or one with no Go routes, is logged and
skipped, so a laptop checkout never needs the private set. CI measures the
public repos only.

**Adding a repo.** Add an entry with `name`, `url`, a full 40-character `ref`,
the module directory in `root` (empty at the clone root), `minRoutes`, and
`framework`. Run `make corpus`: a repo with no baseline entry is reported as a
failure rather than silently accepted. Check that the measured numbers make
sense, then record them deliberately:

```bash
UPDATE_CORPUS=1 go test -tags corpus -run '^TestCorpus$' -v .
git diff testdata/corpus/baseline.json   # review the new floor
```

**Updating the baseline.** Never hand-edit it. After an intentional analyzer
change, run the command above and commit the baseline with the code change.
A new repo has no baseline until that run.

**Regressions.** A route or service count below the baseline fails, as do more
skipped files or more blind (`unresolved`) calls. Resolution may fall at most
5% before it fails. More routes or flows is fine. The failure output names the
repo and the exact baseline-to-current numbers. A gRPC-gateway repo counts its
gateway routes in `routes` and its services separately; `minRoutes` is 4 for
that entry because generated gateway registration exposes four literal paths.

## Performance

The loader reads and parses `.go` files concurrently, and methods are indexed by
`pkgPath|receiver`, so a receiver lookup costs one map hit instead of a scan
over every method in the module. `bench_test.go` generates its fixture (200
handlers, five router groups, eight services) in a temp dir, so the numbers do
not depend on this checkout.

```bash
go test . -run '^$' -bench . -benchtime=3x
go test . -run '^$' -bench BenchmarkLoadModule -benchtime=200x -cpuprofile cpu.out
go tool pprof -top cpu.out
```

On an Apple M3 (darwin/arm64), alternating `-benchtime=200x -count=3` runs,
minimum of nine samples each:

| benchmark | before | after |
| --- | --- | --- |
| `BenchmarkLoadModule` | 1.10 ms | 0.77 ms |
| `BenchmarkBuildRoutes` | 1.08 ms | 1.11 ms |

`BenchmarkBuildRoutes` is unchanged within noise: the 12-pass convergence loop
still dominates route walking, and the fixture has too few receiver methods for
the index to move it.

## What you see

| level | what it shows |
| --- | --- |
| **router** | one column per path segment: server → groups → the routes that sit at that depth. Click a group to walk deeper, a route to open it. |
| **route** | the request walking its chain in execution order: middleware, then the handler, then the response. Click any step for its code. |
| **step** | the handler body as a flow: calls on the spine, `if err != nil` guards as exits with their status code, loops as `↺`, in-repo calls open their own flow. |
| **code** | the raw source, when you want the exact lines. |

Click a stage to open its flow. `⌘K` searches every route and stage and `esc` goes back. The URL holds the whole position (`#/r/0/s/3`), so a link to a middleware is a link.

Comments are the docs: a stage shows its Go doc comment, or `@Summary`/`@Description` from swaggo when present. With no comment, the page says so and lets the flow be the description.

## How it reads the code

- `load.go` parses the module (AST, imports, doc comments) and indexes every func with its calls. Nested `go.mod` files are respected, so a microservice workspace with one module per service resolves across them.
- `typed.go` is optional (`-typed`): it type-checks the module with `golang.org/x/tools/go/packages` and fills call targets the syntax pass could not name.
- `routes.go` walks the route registration: `gin`, `fiber`, `chi`, `gorilla/mux`, `net/http`, `echo`, including groups registered in other functions, middleware added with `Use`, handler factories called inline (`app.Get("/x", Activities(*u))`), factories stored in a variable, and `.PathPrefix(...).Subrouter()` / `.Methods(...)`.
- `flow.go` turns a function body into steps: calls, exits (an `if` whose body only returns), loops, switches, repeated calls folded to `×n`.
- `grpc.go` reads `Register<Service>Server` calls and the generated `_ServiceDesc`, then resolves the implementation and its interceptor chain for the RPC tab.
- `render.go` builds the JSON and inlines it into `ui/dist/index.html`.

The UI is React + TanStack Router + React Flow + shadcn-style primitives; the build is a single HTML file (no assets, no network).

## Multiple services in one checkout

A repo that holds several services — `jobs/`, `users/`, `projects/`, each with its own `package main` (with or without a `cmd/` inside) — gets a **SERVICE** switcher next to LEVEL: `all | jobs | users | projects`. There is no config file: entry points are found from `package main` + `func main`, and every route and gRPC service is attributed to the entry point whose directory contains the file that registers it. The topbar counts, the router map, route search and the RPC surface follow the choice; flows stay shared, because one function can be reached from more than one entry point.

The page stays unsplit when the split would lie: a route registered outside every entry point's directory keeps one surface, and an entry point that owns no route or RPC (a queue worker, say) is not offered.

## Coverage and blind spots

The console report (`-html=false`, or any run) also counts what the analyzer
could not see, and only prints that block when there is something to say:

```
  files      124 parsed, 0 skipped
  calls      1457 total: 141 resolved, 998 external, 24 convert, 34 builtin, 0 blind, 0 dynamic, 0 ambiguous, 260 unknown
             honest 141/401 = 35.2% (external, convert and builtin are not blind spots)
  flows      2 truncated at depth/limit
```

- **files** — every `.go` file that failed to read or parse, that was skipped
  as generated, or that is inactive for the current `GOOS`/`GOARCH` is listed
  with its reason. Inactive files (`//go:build` lines and filename suffixes
  such as `_windows.go`) are never parsed, so a route declared there cannot
  appear as a phantom route on the page. The page omits all skipped files
  entirely, so anything declared there is invisible by design.
- **calls** — every call gets one reason. `resolved` has a target inside this
  module; `external` targets a package outside it; `convert` is a type
  conversion (`string(x)`, `UserID(x)`); `builtin` is a language function
  (`len`, `append`); `blind` is a bare or package-qualified in-module function,
  or a method on a declared in-module value (parameter, result or package
  variable), that the index does not hold; `dynamic` goes through a function
  value (parameter, local, package variable or func-typed field); `ambiguous` is
  an interface value with more than one indexed implementation; `unknown` is a
  method chain whose receiver type is not declared (`tx := db.Begin();
  tx.Exec()`) or a chain that continues past the declared value. Declared
  parameter, result and package-variable types are indexed, so `c.JSON(...)`
  with `c *gin.Context` is `external` and `config.DB.Exec` is `external`, not
  blind. Only `blind` is a real in-module gap. The honest percentage is
  `resolved / (resolved + blind + dynamic + ambiguous + unknown)`; external
  calls, conversions and builtins are excluded because their targets are not in
  this module. Calls are counted per function and deduplicated by expression;
  up to 20 blind expressions are listed with file and line.
- **flows** — flow expansion stops at depth 3 and at 1400 baked functions.
  Baked flows cut by the depth cap carry `trunc` in the JSON data, and the
  report counts every flow skipped at the cap.

The same counters and bounded lists are in the baked JSON under `diag`
(`parsed`, `skipped`, `read`, `parse`, `generated`, `build`, `skip`, `calls`,
`resolved`, `unresolved`, `ext`, `conv`, `dyn`, `amb`, `unk`, `why`, `unres`,
`truncated`). `unresolved` is the blind count; `unres` lists only blind calls,
each with its reason.

## Diagnostics page (`#/diag`)

The counters above have a page: `#/diag`, also linked from the topbar as
`N blind spots` when either counter is non-zero. It renders the baked `diag`
data:

- **files** — parsed and skipped counts with the reason breakdown (`read`,
  `parse`, `generated`, `build`) and up to 20 skipped paths;
- **calls** — resolved/total, the honest percentage, the reason breakdown
  (`resolved`, `blind`, `dynamic`, `ambiguous`, `unknown`, `external`,
  `convert`, `builtin`), and up to 20 blind calls with their function,
  expression and `file:line`;
- **flows** — the truncated count and a link to every baked flow whose expansion
  stopped at the depth or flow cap.

## Change view (`-base`)

`-base main` marks the routes and stages whose file changed since `main`:

```bash
go run . -root . -base main -out synoptic.html
```

The committed part is `git diff --name-only <ref>...HEAD -- '*.go'`, a merge-base
diff, so a base branch that moved after the fork does not light up every file it
touched. Uncommitted edits are included too, which is what makes `-base HEAD`
describe the working tree. Paths stay root-relative.

**Semantics: a badge means "touches a file changed since `<ref>`", not "is a new
route".** A route is marked when its registration file or any stage file
changed; a stage is marked when its own file changed. The base tree is never
analyzed — only file names are read, so routes and flows always describe the
current tree. The router, route and step views show a `changed` badge, and the
JSON carries `change: {ref, files, routes}` plus `chg` on each marked route and
stage.

The feature is best-effort: a missing git, an unknown ref, or a 10-second
timeout prints a warning and leaves the page without change data. It never fails
the run.

## Source redaction (`-no-source`)

`-no-source` drops the raw source from the page while keeping everything that
describes it:

```bash
go run . -root . -no-source -out synoptic.html
```

It clears every function body (`s`) and every step's code (`cd`, `cds`) at every
nesting depth, including `if`/`else` bodies. Docs, file names and line numbers
stay, so flows, the router and the documentation panel render as usual. The
source sheet says `source omitted (built with -no-source)` instead of showing an
empty viewer, and the JSON carries `redact: true`.

## Typed mode (`-typed`)

The default loader is syntax-only: it reads the AST, imports and doc comments,
and never runs the type checker. `-typed` adds an optional pass that runs
`golang.org/x/tools/go/packages` over `-root` and fills call targets the syntax
heuristics cannot name:

- methods promoted from an embedded type — `api.Ping()` where `Ping` is declared
  on the embedded `Base`;
- package calls whose package name differs from its directory name, and calls
  through aliases or generic instantiations;
- package-level functions and variables whose type the checker infers;
- receiver types named indirectly (aliases, promoted fields).

It only fills calls that are still unresolved, so typed output is the AST output
plus those targets; the default run is unchanged. The console prints
`typed: N calls resolved`. A repo that does not type-check is not fatal: the
pass reports `typed: M package errors: <first three>` and keeps whatever it
could resolve, so the page still renders.

What it still cannot see:

- **interface dispatch stays static.** A call on an interface value resolves to
  the interface method, not an implementation; the existing
  single-implementation fallback still applies. Typed mode does not claim to
  resolve implementations.
- code behind another `GOOS`/`GOARCH`, files the loader skipped, and modules
  outside `-root`.
- dynamic behavior: reflection, function values passed as data, and routes built
  at runtime.

Cost and dependency: typed mode type-checks the repo, so it is slower and the
repo must resolve its own imports. The pass sets `GOWORK=off`, so a parent
workspace does not pull in packages outside the checkout. `golang.org/x/tools`
is the project's only Go dependency; only this mode calls it, and the default
analysis path remains standard-library-only.

## Limits

- Paths must be string literals; anything built at runtime is skipped rather than guessed (the tool exits 2 instead of writing an empty page).
- Flow depth stops at foreign packages. A call through an interface field resolves to its single concrete implementation (repository → store); an interface with several implementations stays unbound rather than guessed. Recursion stops at the breadcrumb trail.
- Stream schemas stop at dynamic values (`any`, channels, values crossing an interface). Socket.IO rooms are not treated as plain WebSockets.
- The service switcher is directory-based. A repo that wires every route from one central router, outside the entry point directories, keeps a single surface rather than a wrong split.
- Build constraints use this machine's `GOOS`/`GOARCH` defaults, including `//go:build` lines and filename suffixes such as `_windows.go`. Files that do not match are skipped and listed with reason `build`, so a cross-compiled surface is not shown.
- `-typed` is opt-in and best-effort: it can be slow on large repos, reports the first type errors without failing the run, and never changes a target the syntax pass already found.
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
