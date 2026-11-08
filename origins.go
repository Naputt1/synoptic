package main

import (
	"go/ast"
	"sort"
	"strings"
)

// JOrigin is where a stream event was produced. For a server-pushed event such
// as an SSE notification this is usually a worker or a publishing call, not the
// HTTP route that holds the stream open.
type JOrigin struct {
	N   string `json:"n"`
	Fk  string `json:"fk,omitempty"`
	F   string `json:"f,omitempty"`
	L   int    `json:"l,omitempty"`
	K   string `json:"k,omitempty"` // route, worker, background
	Via string `json:"via,omitempty"`
	M   string `json:"m,omitempty"`
	P   string `json:"p,omitempty"`
	Ri  int    `json:"ri,omitempty"`
	C   int    `json:"c,omitempty"`
}

// JEvent is one event or message case. fn is the concrete case handler when
// the switch passes a function value; it is intentionally unexported.
type JEvent struct {
	N  string    `json:"n"`
	Fn string    `json:"fn,omitempty"`
	Fk string    `json:"fk,omitempty"`
	F  string    `json:"f,omitempty"`
	L  int       `json:"l,omitempty"`
	Or []JOrigin `json:"or,omitempty"`

	fn *Func
}

type originHit struct {
	fn   *Func
	line int
	via  string
}

type originIndex struct {
	byEvent map[string][]originHit
}

var originIndexes = map[*Module]*originIndex{}

func streamOrigins(m *Module) *originIndex {
	if o := originIndexes[m]; o != nil {
		return o
	}
	o := &originIndex{byEvent: map[string][]originHit{}}
	originIndexes[m] = o
	if m == nil {
		return o
	}

	// First caller of every function, used to label a publish made inside a
	// package-level helper as coming from the worker that called it.
	callers := map[string][]*Func{}
	for _, fn := range m.Funcs {
		for _, c := range fn.Calls {
			if c.Target != nil {
				callers[c.Target.Key] = append(callers[c.Target.Key], fn)
			}
		}
	}

	p := &protocolScan{
		idx:        streamsIndex(m),
		seen:       map[string]bool{},
		localTypes: map[string]string{},
		localSrc:   map[string]ast.Expr{},
		eventDefs:  map[string]*JEvent{},
	}
	for _, fn := range m.Funcs {
		if fn == nil || fn.Decl == nil || fn.Decl.Body == nil {
			continue
		}
		scope := &protoScope{fn: fn}
		ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			names := p.originEventNames(scope, ce)
			if len(names) == 0 {
				return true
			}
			via := ""
			if cs := callers[fn.Key]; len(cs) > 0 {
				via = fnName(cs[0])
			}
			for _, name := range names {
				o.byEvent[name] = append(o.byEvent[name], originHit{fn: fn, line: line(ce.Pos()), via: via})
			}
			return true
		})
	}
	for name := range o.byEvent {
		sort.SliceStable(o.byEvent[name], func(i, j int) bool {
			a, b := o.byEvent[name][i], o.byEvent[name][j]
			if a.fn.Key != b.fn.Key {
				return a.fn.Key < b.fn.Key
			}
			return a.line < b.line
		})
	}
	return o
}

func publisherCall(sel string) bool {
	switch sel {
	case "NewEvent", "SSEvent", "SSEventWithType",
		"Publish", "PublishEvent", "publish",
		"Broadcast", "BroadcastEvent", "BroadcastTo", "BroadcastUser",
		"SendEvent", "SendTo", "Notify":
		return true
	}
	return false
}

// originEventNames resolves the event an outbound call is producing. It knows
// the constructors that carry an event as their first argument and the common
// publish wrappers that pass a NewEvent value, map, or message literal.
func (p *protocolScan) originEventNames(scope *protoScope, ce *ast.CallExpr) []string {
	if ce == nil {
		return nil
	}
	sel := callName(ce)
	if !publisherCall(sel) {
		return nil
	}
	var out []string
	switch sel {
	case "NewEvent", "SSEvent", "SSEventWithType":
		if len(ce.Args) > 0 {
			out = append(out, p.eventNames(scope, ce.Args[0])...)
		}
		return uniqueStrings(out)
	}

	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		if e == nil {
			return
		}
		if call, ok := e.(*ast.CallExpr); ok {
			out = append(out, p.originEventNames(scope, call)...)
			return
		}
		if lit := compositeLit(e); lit != nil {
			if ev := p.literalEvent(scope, lit); ev != "" {
				out = append(out, ev)
			}
		}
		if ev := eventFromLiteral(exprText(e)); ev != "" {
			out = append(out, ev)
		}
	}
	for _, arg := range ce.Args {
		walk(arg)
	}
	return uniqueStrings(out)
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// attachStreamOrigins ties server-pushed events back to the code that produced
// them. The stream handler itself is skipped: the useful origin is elsewhere.
func attachStreamOrigins(m *Module, routes []*Route, jr []JRoute) {
	if m == nil || len(routes) == 0 {
		return
	}
	idx := streamOrigins(m)

	routeOf := map[string]*Route{}
	for _, r := range routes {
		for _, ref := range r.Chain {
			if ref.Fn != nil {
				routeOf[ref.Fn.Key] = r
			}
		}
		if r.Entry != nil {
			routeOf[r.Entry.Key] = r
		}
	}
	riOf := map[string]int{}
	for i, j := range jr {
		riOf[j.M+"\x00"+j.P] = i
	}

	for _, r := range routes {
		if r.Stream == nil || len(r.Stream.Evs) == 0 {
			continue
		}
		baseSkip := map[string]bool{}
		for _, ref := range r.Chain {
			if ref.Fn != nil {
				baseSkip[ref.Fn.Key] = true
			}
		}
		if r.Entry != nil {
			baseSkip[r.Entry.Key] = true
		}
		for i := range r.Stream.Evs {
			ev := &r.Stream.Evs[i]
			skip := map[string]bool{}
			for k := range baseSkip {
				skip[k] = true
			}
			if ev.fn != nil {
				skip[ev.fn.Key] = true
			}
			hits := idx.byEvent[ev.N]
			if len(hits) == 0 {
				continue
			}
			at := map[string]int{}
			var list []JOrigin
			for _, h := range hits {
				if h.fn == nil || skip[h.fn.Key] {
					continue
				}
				if pos, ok := at[h.fn.Key]; ok {
					list[pos].C++
					continue
				}
				o := JOrigin{
					N:   fnName(h.fn),
					Fk:  h.fn.Key,
					F:   h.fn.File.Rel,
					L:   h.line,
					K:   "background",
					Via: h.via,
					C:   1,
				}
				if rr := routeOf[h.fn.Key]; rr != nil {
					o.K = "route"
					o.M = rr.Method
					o.P = rr.Path
					o.Ri = riOf[rr.Method+"\x00"+rr.Path]
				} else if strings.Contains(h.fn.PkgPath, "/worker") || strings.Contains(h.fn.Name, "Worker") || strings.Contains(h.fn.Name, "Handler") {
					o.K = "worker"
				}
				at[h.fn.Key] = len(list)
				list = append(list, o)
				if len(list) >= 12 {
					break
				}
			}
			sort.SliceStable(list, func(a, b int) bool {
				if list[a].K != list[b].K {
					return originRank(list[a].K) < originRank(list[b].K)
				}
				if list[a].F != list[b].F {
					return list[a].F < list[b].F
				}
				if list[a].L != list[b].L {
					return list[a].L < list[b].L
				}
				return list[a].N < list[b].N
			})
			ev.Or = list
		}
	}
}

func originRank(k string) int {
	switch k {
	case "route":
		return 0
	case "worker":
		return 1
	}
	return 2
}
