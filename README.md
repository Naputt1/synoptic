# synoptic

Reads a Go repo and renders its HTTP surface as one self-contained page: a **map of the router**, the **middleware pipeline of each route**, and the **code path of each handler** — all built from the source, no annotations required.

```bash
cd ui && npm install && npm run build && cd ..

go run . -root /path/to/repo -out /path/to/repo/synoptic.html
open /path/to/repo/synoptic.html
```

Flags: `-root` (default `.`), `-out` (default `<root>/synoptic.html`), `-json <file>` (dump the data for the dev UI).

## What you see

| level | what it shows |
| --- | --- |
| **router** | one column per path segment: server → groups → the routes that sit at that depth. Click a group to walk deeper, a route to open it. |
| **route** | the request walking its chain in execution order: middleware, then the handler, then the response. Click any step for its code. |
| **step** | the handler body as a flow: calls on the spine, `if err != nil` guards as exits with their status code, loops as `↺`, in-repo calls open their own flow. |
| **code** | the raw source, when you want the exact lines. |

`MODE` switches between **static** (everything at once) and **replay** (walk the chain step by step; the edge lights up as the request moves). `⌘K` searches every route and stage, `esc` goes back, `←`/`→` step, space plays. The URL holds the whole position (`#/r/0/s/3`), so a link to a middleware is a link.

Comments are the docs: a stage shows its Go doc comment, or `@Summary`/`@Description` from swaggo when present. With no comment, the page says so and lets the flow be the description.

## How it reads the code

- `load.go` parses the module (AST, imports, doc comments) and indexes every func with its calls.
- `routes.go` walks the route registration: `gin`, `fiber`, `chi`, `gorilla/mux`, `net/http`, `echo`, including groups registered in other functions, middleware added with `Use`, handler factories called inline (`app.Get("/x", Activities(*u))`), factories stored in a variable, and `.PathPrefix(...).Subrouter()` / `.Methods(...)`.
- `flow.go` turns a function body into steps: calls, exits (an `if` whose body only returns), loops, switches, repeated calls folded to `×n`.
- `render.go` builds the JSON and inlines it into `ui/dist/index.html`.

The UI is React + TanStack Router + React Flow + shadcn-style primitives; the build is a single HTML file (no assets, no network).

## Limits

- Paths must be string literals; anything built at runtime is skipped rather than guessed (the tool exits 2 instead of writing an empty page).
- Flow depth stops at foreign packages and unbound interface methods; recursion stops at the breadcrumb trail.
- Replay is synthetic — it follows the chain, it does not replay a recorded trace.

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

With a trace in the page, `MODE → trace` shows each stage's median, its p95, its
share of the total (the bar along the card's bottom edge), and names the slowest
stage in the header. Without `-trace`, the mode says so instead of pretending.

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
| responses | status codes and literal `gin.H{}` / `fiber.Map{}` keys per exit |
| auth | a middleware in the chain whose name looks like auth, jwt, session, token or guard (labelled as inferred) |
| data touched | `Table("…")` and `Model(&X{})` in the handler and its direct callees |
| owner | `git log -1` for the file: author, short sha, age |
| example | a `curl` with the bearer header and a JSON body from the fields above |

Anything the reader cannot prove stays empty and says so, rather than guessing.
