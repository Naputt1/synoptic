package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"sort"
	"strings"
)

type JStage struct {
	Name string   `json:"n"`
	Kind string   `json:"k"`
	Role string   `json:"r,omitempty"`
	File string   `json:"f,omitempty"`
	Line int      `json:"l,omitempty"`
	Doc  string   `json:"d,omitempty"`
	Pkg  string   `json:"p,omitempty"`
	Ext  string   `json:"x,omitempty"`
	K    string   `json:"fk,omitempty"`
	In   []JInput `json:"in,omitempty"`
	Chg  bool     `json:"chg,omitempty"`
}

type JRoute struct {
	M     string   `json:"m"`
	P     string   `json:"p"`
	F     string   `json:"f"`
	L     int      `json:"l"`
	Pr    string   `json:"pr,omitempty"`
	Svc   string   `json:"svc,omitempty"`
	St    *JStream `json:"st,omitempty"`
	Chain []JStage `json:"ch"`
	Doc   *JDoc    `json:"doc,omitempty"`
	Chg   bool     `json:"chg,omitempty"`
}

// JChange describes the -base view: the ref compared against, how many files it
// touched, and how many routes have at least one changed file.
type JChange struct {
	Ref    string `json:"ref"`
	Files  int    `json:"files"`
	Routes int    `json:"routes"`
}

// JSvc is one entry point of a multi-service module, with what it owns.
type JSvc struct {
	N      string `json:"n"`
	D      string `json:"d,omitempty"`
	E      string `json:"e,omitempty"`
	Routes int    `json:"routes"`
	RPCs   int    `json:"rpcs"`
}

// JFunc is one node of the code graph: enough to draw its flow and its body.
type JFunc struct {
	Name  string    `json:"n"`
	Pkg   string    `json:"p,omitempty"`
	File  string    `json:"f,omitempty"`
	Line  int       `json:"l,omitempty"`
	Doc   string    `json:"d,omitempty"`
	Src   string    `json:"s,omitempty"`
	Steps []Step    `json:"flow,omitempty"`
	In    []JInput  `json:"in,omitempty"`
	Fx    []JEffect `json:"fx,omitempty"`
	Rpc   string    `json:"rpc,omitempty"`   // "si/mi" when this function implements a service method
	Trunc bool      `json:"trunc,omitempty"` // baked flow stopped at the depth or flow cap
}

type JStats struct {
	Routes     int `json:"routes"`
	Groups     int `json:"groups"`
	Middleware int `json:"middleware"`
	Documented int `json:"documented"`
	Packages   int `json:"packages"`
	Funcs      int `json:"funcs"`
	Streams    int `json:"streams,omitempty"`
	WS         int `json:"ws,omitempty"`
	SSE        int `json:"sse,omitempty"`
	Services   int `json:"services,omitempty"`
	RPCs       int `json:"rpcs,omitempty"`
}

type JData struct {
	Repo     string                  `json:"repo"`
	Mod      string                  `json:"mod"`
	Stats    JStats                  `json:"stats"`
	Routes   []JRoute                `json:"routes"`
	Svcs     []JSvc                  `json:"svcs,omitempty"`
	Services []JService              `json:"services,omitempty"`
	Flows    map[string]JFunc        `json:"flows"`
	Types    map[string]JType        `json:"types,omitempty"`
	Timing   map[string]JRouteTiming `json:"timing,omitempty"`
	Diag     Diag                    `json:"diag"`
	Change   *JChange                `json:"change,omitempty"`
	Redact   bool                    `json:"redact,omitempty"`
}

func buildData(m *Module, routes []*Route) JData {
	d := JData{Repo: baseName(m.Root), Mod: m.Path, Flows: map[string]JFunc{}}
	m.Diag.Truncated = 0
	groups := map[string]bool{}
	mw := map[string]bool{}

	if units := detectUnits(m); len(units) > 1 {
		d.Svcs = make([]JSvc, len(units))
		for i, u := range units {
			d.Svcs[i] = JSvc{N: u.name, D: u.dir, E: u.entry}
		}
	}

	types := buildTypeCatalog(m)
	resolveDispatchedHandlers(m, routes, types)

	for _, r := range routes {
		groups[r.Group] = true
		r.Stream = classifyStream(m, r)
		if r.Stream != nil {
			r.Proto = r.Stream.K
		}
		jr := JRoute{M: r.Method, P: r.Path, F: r.File.Rel, L: r.Line, Pr: r.Proto, St: r.Stream}
		if len(d.Svcs) > 0 {
			jr.Svc = svcForFile(d.Svcs, r.File.Rel)
		}
		if r.Stream != nil {
			d.Stats.Streams++
			switch r.Stream.K {
			case "ws":
				d.Stats.WS++
			case "sse":
				d.Stats.SSE++
			}
		}
		for _, ref := range r.Chain {
			st := JStage{Name: ref.Name, Kind: ref.Kind, Role: ref.Role, Line: ref.Line}
			if ref.File != nil {
				st.File = ref.File.Rel
			}
			if ref.Fn != nil {
				st.Doc = displayDoc(ref.Fn)
				st.Pkg = altPkg(ref.Fn.PkgPath)
				if ref.Fn.PkgPath != "" && !strings.HasPrefix(ref.Fn.PkgPath, m.Path) {
					st.Ext = ref.Fn.PkgPath
				}
				st.K = ref.Fn.Key
				st.In = analyzeStageInputs(m, ref.Fn, r.Path)
				indexFunc(m, d.Flows, ref.Fn, 0)
			}
			if ref.Role == "middleware" {
				mw[ref.Name] = true
			}
			jr.Chain = append(jr.Chain, st)
		}
		for _, st := range jr.Chain {
			if st.Role == "handler" && st.Doc != "" {
				d.Stats.Documented++
				break
			}
		}
		jr.Doc = buildDoc(m, r)
		d.Routes = append(d.Routes, jr)
	}
	sort.SliceStable(d.Routes, func(i, j int) bool {
		if d.Routes[i].P != d.Routes[j].P {
			return d.Routes[i].P < d.Routes[j].P
		}
		return d.Routes[i].M < d.Routes[j].M
	})
	indexStreamEvents(m, d.Flows, routes)
	attachStreamOrigins(m, routes, d.Routes)

	if len(d.Svcs) > 0 {
		at := map[string]int{}
		for i := range d.Svcs {
			at[d.Svcs[i].N] = i
		}
		orphan := false
		for _, jr := range d.Routes {
			if jr.Svc == "" {
				orphan = true
				break
			}
			if i, ok := at[jr.Svc]; ok {
				d.Svcs[i].Routes++
			}
		}
		// A route registered outside every entry point means the split does not
		// describe this repo; keep one surface rather than a misleading filter.
		if orphan {
			d.Svcs = nil
			for i := range d.Routes {
				d.Routes[i].Svc = ""
			}
		}
	}

	d.Stats.Routes = len(d.Routes)
	d.Stats.Groups = len(groups)
	d.Stats.Middleware = len(mw)
	d.Stats.Packages = len(m.PKG)
	d.Types = types
	for key, jf := range d.Flows {
		annotateSteps(jf.Steps, d.Types)
		d.Flows[key] = jf
	}
	d.Stats.Funcs = len(d.Flows)
	d.Diag = m.Diag
	return d
}

const flowLimit = 1400
const flowDepth = 3

// indexStreamEvents bakes the concrete switch-case handlers too, so the event
// inspector can show how each event is processed without making a second pass.
func indexStreamEvents(m *Module, flows map[string]JFunc, routes []*Route) {
	for _, r := range routes {
		if r.Stream == nil {
			continue
		}
		if r.Stream.K == "sse" && r.Stream.Hk == "" {
			// The registered handler is often a one-line closure around the
			// real stream loop. Point the inspector at that function.
			for i := len(r.Chain) - 1; i >= 0; i-- {
				ref := r.Chain[i]
				if ref.Fn == nil || (ref.Kind != "closure" && !strings.Contains(strings.ToLower(ref.Name), "inline")) {
					continue
				}
				for _, c := range ref.Fn.Calls {
					if c.Target != nil {
						r.Stream.Hk = c.Target.Key
						break
					}
				}
				if r.Stream.Hk != "" {
					break
				}
			}
		}
		for _, e := range r.Stream.Evs {
			if e.fn != nil {
				indexFunc(m, flows, e.fn, 0)
			}
		}
	}
}

// indexFunc bakes a function's flow and its callees, so every click is local.
func indexFunc(m *Module, flows map[string]JFunc, fn *Func, depth int) {
	if fn == nil || fn.Key == "" {
		return
	}
	if len(flows) >= flowLimit {
		if _, seen := flows[fn.Key]; !seen {
			m.Diag.Truncated++
		}
		return
	}
	file := ""
	if fn.File != nil {
		file = fn.File.Rel
	}
	if _, seen := flows[fn.Key]; seen {
		return
	}
	jf := JFunc{
		Name:  fnName(fn),
		Pkg:   altPkg(fn.PkgPath),
		File:  file,
		Line:  fn.Start,
		Doc:   displayDoc(fn),
		Src:   snippet(fn, 120),
		Steps: buildFlow(fn),
		In:    analyzeStageInputs(m, fn, ""),
		Fx:    buildEffects(fn),
	}
	// At the depth cap a flow with callees is cut short; say so rather than
	// ending the trail as if the function had nothing left to show.
	if depth >= flowDepth && hasTarget(fn) {
		jf.Trunc = true
		m.Diag.Truncated++
	}
	flows[fn.Key] = jf
	if depth >= flowDepth {
		return
	}
	for _, c := range fn.Calls {
		if c.Target != nil {
			indexFunc(m, flows, c.Target, depth+1)
		}
	}
}

// hasTarget reports whether a function calls something this module resolved.
func hasTarget(fn *Func) bool {
	for _, c := range fn.Calls {
		if c.Target != nil {
			return true
		}
	}
	return false
}

func fnName(fn *Func) string {
	if fn.Recv != "" {
		return fn.Recv + "." + fn.Name
	}
	return fn.Name
}

// plumbing is the framework chatter around a handler, not a service it uses.
func plumbing(c Call) bool {
	if c.Target != nil || c.PkgPath != "" {
		return false
	}
	switch c.Base {
	case "c", "ctx", "err", "ok", "w", "r", "req", "res", "fiber", "gin":
		segs := strings.Split(c.Expr, ".")
		return len(segs) == 2 || strings.Contains(c.Expr, "()")
	}
	switch c.Sel {
	case "Error", "String", "Unwrap", "Errorf":
		return true
	}
	return false
}

func displayDoc(f *Func) string {
	if f == nil || f.Doc == "" {
		return ""
	}
	d := f.Doc
	if !strings.Contains(d, "@") {
		return strings.Join(strings.Fields(d), " ")
	}
	sum := fieldOf(d, "@Summary ")
	desc := fieldOf(d, "@Description ")
	out := sum
	if desc != "" {
		if out != "" {
			out += " — "
		}
		out += desc
	}
	if out == "" {
		out = strings.Join(strings.Fields(d), " ")
	}
	return out
}

func fieldOf(doc, key string) string {
	i := strings.Index(doc, key)
	if i < 0 {
		return ""
	}
	rest := doc[i+len(key):]
	if j := strings.Index(rest, "\n@"); j >= 0 {
		rest = rest[:j]
	}
	if j := strings.Index(rest, " @"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(strings.Join(strings.Fields(rest), " "))
}

func isAuthReceiver(s string) bool {
	return strings.Contains(strings.ToLower(s), "authenticator")
}

func isServiceReceiver(s string) bool {
	s = strings.ToLower(s)
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '_' || r == '*' || r == ' ' }) {
		switch part {
		case "service", "services", "svc", "usecase", "usecases", "repo", "repository":
			return true
		}
	}
	return strings.Contains(s, "servicelayer")
}

func tagOf(c Call) string {
	if isServiceReceiver(c.Expr) {
		return "service"
	}
	if isAuthReceiver(c.Expr) {
		return "auth"
	}
	// A driver chain names the store in the middle, not the receiver:
	// r.db.Collection(t).FindOne(...).Decode(&v) starts at "r".
	parts := chainParts(c)
	switch {
	case hasPart(parts, dbWords):
		return "db"
	case hasPart(parts, cacheWords):
		return "cache"
	case hasPart(parts, netWords):
		return "net"
	}
	switch c.Base {
	case "DB", "db", "gorm", "Mongo", "mongo", "MongoClient":
		return "db"
	case "Redis", "redis", "Valkey", "valkey", "Cache", "cache", "Rdb", "rdb":
		return "cache"
	case "Client", "client", "HTTP", "http", "Grpc", "grpc":
		return "net"
	}
	switch c.Sel {
	case "Table", "Model", "Where", "Joins", "First", "Find", "Select", "Create", "Save", "Updates", "Delete", "Exec", "Raw", "Query":
		return "db"
	}
	return ""
}

// callParts lists the selectors of a chained call in lower case:
// r.db.Collection(t).FindOne(...).Decode -> [r db collection findone decode].
func chainParts(c Call) []string {
	parts := strings.Split(c.Qual+"."+c.Sel, ".")
	for i, p := range parts {
		parts[i] = strings.ToLower(strings.TrimSuffix(p, "()"))
	}
	return parts
}

func hasPart(parts []string, set map[string]bool) bool {
	for _, p := range parts {
		if set[p] {
			return true
		}
	}
	return false
}

var dbWords = map[string]bool{
	"db": true, "mongo": true, "mongodb": true, "database": true, "collection": true,
	"collections": true, "table": true, "tx": true, "gorm": true, "sql": true,
	"sqlx": true, "postgres": true, "mysql": true, "pgx": true,
}

var cacheWords = map[string]bool{
	"redis": true, "valkey": true, "cache": true, "rdb": true, "memcache": true, "memcached": true,
}

var netWords = map[string]bool{
	"http": true, "https": true, "grpc": true, "client": true, "httpclient": true,
}

var dbReadOps = map[string]bool{
	"findone": true, "find": true, "aggregate": true, "countdocuments": true,
	"count": true, "distinct": true, "estimateddocumentcount": true, "watch": true,
}

var dbWriteOps = map[string]bool{
	"insertone": true, "insertmany": true, "updateone": true, "updatemany": true,
	"replaceone": true, "deleteone": true, "deletemany": true, "findoneandupdate": true,
	"findoneanddelete": true, "bulkwrite": true, "exec": true,
}

func snippet(fn *Func, maxLines int) string {
	if fn == nil || fn.File == nil || fn.Start <= 0 {
		return ""
	}
	lines := fn.File.Lines
	start := fn.Start
	end := fn.End
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	truncated := false
	if end-start+1 > maxLines {
		end = start + maxLines - 1
		truncated = true
	}
	if start-1 >= len(lines) {
		return ""
	}
	out := strings.Join(lines[start-1:end], "\n")
	if truncated {
		out += "\n// …"
	}
	return out
}

func baseName(p string) string {
	parts := strings.Split(strings.TrimRight(p, "/"), "/")
	return parts[len(parts)-1]
}

// writeJSON writes the same data the page bakes in, for the dev UI.
func writeJSON(path string, d JData) error {
	blob, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, blob, 0o644)
}

func writeHTML(path string, d JData) error {
	blob, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if shellHTML == "" {
		return errors.New("ui/dist/index.html is missing: run `cd ui && bun install && bun run build`")
	}
	page := strings.Replace(shellHTML, "</body>",
		`<script id="synoptic-data" type="application/json">`+string(blob)+`</script></body>`, 1)
	page = strings.Replace(page, "<title>route synoptic</title>", "<title>"+html.EscapeString(d.Repo)+" route synoptic</title>", 1)
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(page), 0o644)
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return "."
}

func report(d JData, out string) {
	fmt.Printf("%s\n", d.Repo)
	fmt.Printf("  routes     %d\n", d.Stats.Routes)
	fmt.Printf("  groups     %d\n", d.Stats.Groups)
	fmt.Printf("  middleware %d\n", d.Stats.Middleware)
	if d.Stats.Streams > 0 {
		fmt.Printf("  streams    %d (%d ws, %d sse)\n", d.Stats.Streams, d.Stats.WS, d.Stats.SSE)
	}
	if d.Stats.Services > 0 {
		fmt.Printf("  rpc        %d services, %d methods\n", d.Stats.Services, d.Stats.RPCs)
	}
	fmt.Printf("  handlers with a doc comment  %d/%d\n", d.Stats.Documented, d.Stats.Routes)
	fmt.Printf("  flows baked                  %d functions\n", d.Stats.Funcs)
	g := d.Diag
	if g.Skipped > 0 {
		fmt.Printf("  files      %d parsed, %d skipped", g.Parsed, g.Skipped)
		var why []string
		for _, r := range []struct {
			n     int
			label string
		}{{g.Parse, "parse"}, {g.Read, "read"}, {g.Generated, "generated"}, {g.Build, "build"}} {
			if r.n > 0 {
				why = append(why, fmt.Sprintf("%s %d", r.label, r.n))
			}
		}
		if len(why) > 0 {
			fmt.Printf(" (%s)", strings.Join(why, ", "))
		}
		fmt.Println()
		for i, f := range g.Skip {
			if i == 5 {
				break
			}
			fmt.Printf("             - %s (%s)\n", f.Path, f.Reason)
		}
	}
	if g.Unresolved > 0 {
		fmt.Printf("  calls      %d/%d resolved (%d in-module unresolved)\n", g.Resolved, g.Calls, g.Unresolved)
		for i, c := range g.Unres {
			if i == 5 {
				break
			}
			fmt.Printf("             - %s: %s (%s:%d)\n", c.Func, c.Expr, c.File, c.Line)
		}
	}
	if g.Truncated > 0 {
		fmt.Printf("  flows      %d truncated at depth/limit\n", g.Truncated)
	}
	fmt.Printf("  written to %s\n", out)
}
