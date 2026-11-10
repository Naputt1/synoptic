package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

type Ref struct {
	Name string
	Kind string // func, method, closure, expr, unknown
	Role string // middleware, handler
	File *FileCtx
	Line int
	Fn   *Func
}

type State struct {
	Prefix string
	Mws    []Ref
}

type Route struct {
	Method string
	Path   string
	File   *FileCtx
	Line   int
	Chain  []Ref
	Group  string
	Proto  string
	Stream *JStream
	// Entry is the concrete stream handler behind a registration wrapper.
	// websocket.Register(router, path, frontsocket.NewHandler(...)) does not
	// put the handler in the route chain, but its OnMessage method is where
	// the frames live.
	Entry *Func
}

type Walker struct {
	m            *Module
	states       map[string]map[string]State
	routes       []*Route
	seen         map[string]bool
	routerFields map[string]map[string]bool
	recording    bool
	methods      []string
	extra        []Ref
	fnvars       map[string]*Func
	types        map[string]map[string]string
	zeros        map[string]map[string]string
	pathArgs     map[string]map[string]string
	entry        *Func
	changed      bool
}

var routeVerbs = map[string]string{
	"GET": "GET", "Get": "GET",
	"POST": "POST", "Post": "POST",
	"PUT": "PUT", "Put": "PUT",
	"PATCH": "PATCH", "Patch": "PATCH",
	"DELETE": "DELETE", "Delete": "DELETE",
	"HEAD": "HEAD", "Head": "HEAD",
	"OPTIONS": "OPTIONS", "Options": "OPTIONS",
	"CONNECT": "CONNECT", "Connect": "CONNECT",
	"TRACE": "TRACE", "Trace": "TRACE",
	"Any": "ANY", "All": "ANY", "Handle": "ANY", "HandleFunc": "ANY",
}

func buildRoutes(m *Module) []*Route {
	w := &Walker{m: m, states: map[string]map[string]State{}, seen: map[string]bool{}, routerFields: map[string]map[string]bool{}, fnvars: map[string]*Func{}, types: map[string]map[string]string{}, zeros: map[string]map[string]string{}, pathArgs: map[string]map[string]string{}}
	w.seed()
	// entrypoints often have no router typed parameter themselves; they create
	// or receive one and hand it to a Register helper. Walk them once so that
	// hand-off reaches the helper with the real prefix.
	for _, f := range m.Funcs {
		if _, ok := w.states[f.Key]; !ok {
			w.states[f.Key] = map[string]State{}
		}
	}
	// converge the router state first, then record what it settled on
	for pass := 0; pass < 12; pass++ {
		w.changed = false
		w.pass()
		if !w.changed {
			break
		}
	}
	w.recording = true
	w.pass()
	sort.SliceStable(w.routes, func(i, j int) bool {
		if w.routes[i].Path != w.routes[j].Path {
			return w.routes[i].Path < w.routes[j].Path
		}
		return w.routes[i].Method < w.routes[j].Method
	})
	return w.routes
}

func (w *Walker) pass() {
	for _, key := range sortedKeys(w.m.Funcs) {
		f := w.m.Funcs[key]
		st, ok := w.states[key]
		if !ok {
			continue
		}
		w.body(f, f.Decl.Body.List, cloneEnv(st))
	}
}

func (w *Walker) seed() {
	// local router variables: r := gin.Default(), app := fiber.New(), ...
	for _, f := range w.m.Funcs {
		ast.Inspect(f.Decl.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, rhs := range x.Rhs {
					if i >= len(x.Lhs) {
						break
					}
					name := keyOf(x.Lhs[i])
					if name == "" {
						continue
					}
					if ce, ok := rhs.(*ast.CallExpr); ok && isRootCtor(f, ce) {
						w.setState(f, name, State{})
					}
				}
			case *ast.ValueSpec:
				for i, v := range x.Values {
					if i >= len(x.Names) {
						break
					}
					if ce, ok := v.(*ast.CallExpr); ok && isRootCtor(f, ce) {
						w.setState(f, x.Names[i].Name, State{})
					}
				}
			}
			return true
		})
		// router typed parameters: func Setup(group fiber.Router)
		if f.Decl.Type.Params != nil {
			for _, p := range f.Decl.Type.Params.List {
				if isRouterType(p.Type) {
					for _, n := range p.Names {
						w.setState(f, n.Name, State{})
					}
				}
			}
		}
	}

	// routers held on a struct: type Server struct { app *fiber.App }
	for _, fc := range w.m.Files {
		for _, d := range fc.Ast.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}
				for _, fld := range st.Fields.List {
					if !isRouterType(fld.Type) {
						continue
					}
					for _, n := range fld.Names {
						if w.routerFields[ts.Name.Name] == nil {
							w.routerFields[ts.Name.Name] = map[string]bool{}
						}
						w.routerFields[ts.Name.Name][n.Name] = true
						for _, m := range w.m.ByRecv[ts.Name.Name] {
							key := m.RecvVar + "." + n.Name
							if _, set := w.states[m.Key][key]; !set {
								w.setState(m, key, State{})
							}
						}
					}
				}
			}
		}
	}
}

func (w *Walker) setState(f *Func, name string, s State) {
	if name == "" {
		return
	}
	if w.states[f.Key] == nil {
		w.states[f.Key] = map[string]State{}
	}
	old, ok := w.states[f.Key][name]
	if ok && old.Prefix == s.Prefix && sameRefs(old.Mws, s.Mws) {
		return
	}
	w.states[f.Key][name] = s
	w.changed = true
}

func sameRefs(a, b []Ref) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

func (w *Walker) body(f *Func, stmts []ast.Stmt, env map[string]State) {
	for _, s := range stmts {
		w.stmt(f, s, env)
	}
}

func (w *Walker) stmt(f *Func, s ast.Stmt, env map[string]State) {
	switch x := s.(type) {
	case *ast.AssignStmt:
		for i, rhs := range x.Rhs {
			if i >= len(x.Lhs) {
				break
			}
			w.assign(f, x.Lhs[i], rhs, env)
		}
	case *ast.DeclStmt:
		gd, ok := x.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, v := range vs.Values {
				if i < len(vs.Names) {
					w.assign(f, vs.Names[i], v, env)
				}
			}
		}
	case *ast.ExprStmt:
		if ce, ok := x.X.(*ast.CallExpr); ok {
			w.callStmt(f, ce, env)
		}
	case *ast.BlockStmt:
		w.body(f, x.List, env)
	case *ast.IfStmt:
		if x.Init != nil {
			w.stmt(f, x.Init, env)
		}
		w.stmt(f, x.Body, env)
		if x.Else != nil {
			w.stmt(f, x.Else, env)
		}
	case *ast.ForStmt:
		w.stmt(f, x.Body, env)
	case *ast.RangeStmt:
		w.stmt(f, x.Body, env)
	case *ast.SwitchStmt:
		w.stmt(f, x.Body, env)
	case *ast.TypeSwitchStmt:
		w.stmt(f, x.Body, env)
	case *ast.CaseClause:
		w.body(f, x.Body, env)
	case *ast.ReturnStmt:
		for _, r := range x.Results {
			w.composite(f, r, env)
		}
	case *ast.LabeledStmt:
		w.stmt(f, x.Stmt, env)
	}
}

// composite carries a router held in a local variable into the struct that
// stores it, so the type's methods start from the same chain.
func (w *Walker) composite(f *Func, e ast.Expr, env map[string]State) {
	for {
		u, ok := e.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			break
		}
		e = u.X
	}
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return
	}
	// routes are often registered inside a struct literal:
	// res := &Result{List: group.GET("/list", h).Bind(mw)...}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		switch v := kv.Value.(type) {
		case *ast.CallExpr:
			w.callStmt(f, v, env)
		case *ast.CompositeLit, *ast.UnaryExpr:
			w.composite(f, v, env)
		}
	}
	typeName := strings.TrimPrefix(typeString(lit.Type), "*")
	fields, ok := w.routerFields[typeName]
	if !ok {
		return
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		field := identName(kv.Key)
		if !fields[field] {
			continue
		}
		st, ok := env[keyOf(kv.Value)]
		if !ok {
			continue
		}
		for _, m := range w.m.ByRecv[typeName] {
			w.setState(m, m.RecvVar+"."+field, st)
		}
	}
}

func (w *Walker) assign(f *Func, lhs, rhs ast.Expr, env map[string]State) {
	name := keyOf(lhs)
	if name == "" || name == "_" {
		return
	}
	if lit := compositeLit(rhs); lit != nil {
		if len(lit.Elts) == 0 {
			w.setZero(f, name, shortType(lit.Type))
		}
		w.composite(f, rhs, env)
		delete(env, name)
		return
	}
	if id, ok := rhs.(*ast.Ident); ok {
		if s, ok := env[id.Name]; ok {
			env[name] = s
			return
		}
		delete(env, name)
		return
	}
	ce, ok := rhs.(*ast.CallExpr)
	if !ok {
		delete(env, name)
		return
	}
	// handler factories: userJwtSign := middleware.JwtSign(model.USER)
	if fsel, ok := ce.Fun.(*ast.SelectorExpr); ok {
		if target := w.resolveCallee(f, fsel); target != nil && returnsFunc(target) {
			w.fnvars[f.Key+"."+name] = target
		}
	}
	if isRootCtor(f, ce) {
		env[name] = State{}
		return
	}
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		delete(env, name)
		return
	}
	// chained subrouters: router.PathPrefix("/api").Subrouter(), e.Group("/v1").Group("/x")
	if inner, isCall := sel.X.(*ast.CallExpr); isCall {
		if isel, ok := inner.Fun.(*ast.SelectorExpr); ok {
			if p, known := w.stateOf(f, isel.X, env); known {
				st := State{Prefix: p.Prefix, Mws: append([]Ref(nil), p.Mws...)}
				if len(inner.Args) > 0 {
					st.Prefix = joinPath(p.Prefix, w.pathOf(f, inner.Args[0]))
				}
				env[name] = st
				return
			}
		}
		delete(env, name)
		return
	}
	parent, known := w.stateOf(f, sel.X, env)
	if !known {
		// registration helpers are often called as an assignment:
		// res := api.RegisterPB(h, option, mw, group)
		w.call(f, ce, env)
		delete(env, name)
		return
	}
	switch sel.Sel.Name {
	case "Group", "PathPrefix", "Subrouter":
		st := State{Prefix: parent.Prefix, Mws: append([]Ref(nil), parent.Mws...)}
		if len(ce.Args) > 0 {
			st.Prefix = joinPath(parent.Prefix, w.pathOf(f, ce.Args[0]))
		}
		if sel.Sel.Name == "Group" && len(ce.Args) > 1 {
			st.Mws = append(st.Mws, w.mws(f, ce.Args[1:])...)
		}
		env[name] = st
	case "With":
		st := State{Prefix: parent.Prefix, Mws: append([]Ref(nil), parent.Mws...)}
		st.Mws = append(st.Mws, w.mws(f, ce.Args)...)
		env[name] = st
	default:
		delete(env, name)
	}
}

// callStmt unwraps route builders: gorilla/mux .Methods, pocketbase/echo
// .Bind, and the other fluent tails that return the same route.
func (w *Walker) callStmt(f *Func, ce *ast.CallExpr, env map[string]State) {
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		w.call(f, ce, env)
		return
	}
	switch sel.Sel.Name {
	case "Methods":
		if inner, isCall := sel.X.(*ast.CallExpr); isCall {
			var ms []string
			for _, a := range ce.Args {
				if lit, ok := a.(*ast.BasicLit); ok {
					ms = append(ms, strings.Trim(lit.Value, "`\""))
				}
			}
			w.methods = ms
			w.call(f, inner, env)
			w.methods = nil
			return
		}
	case "Bind", "Middleware", "Use":
		// pocketbase: group.GET("/x", h).Bind(mw...)
		if inner, isCall := sel.X.(*ast.CallExpr); isCall {
			extra := w.extra
			w.extra = append(append([]Ref(nil), extra...), w.mws(f, ce.Args)...)
			w.call(f, inner, env)
			w.extra = extra
			return
		}
	}
	w.call(f, ce, env)
}

func (w *Walker) call(f *Func, ce *ast.CallExpr, env map[string]State) {
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	// framework lifecycle hooks run the router setup inside a callback:
	// e.OnServe().BindFunc(func(e *core.ServeEvent) error { ... })
	switch sel.Sel.Name {
	case "BindFunc", "AddFunc", "OnStartup", "OnShutdown":
		for _, arg := range ce.Args {
			if lit, ok := arg.(*ast.FuncLit); ok {
				w.body(f, lit.Body.List, cloneEnv(env))
			}
		}
	}
	if st, known := w.stateOf(f, sel.X, env); known {
		switch sel.Sel.Name {
		case "Use":
			st.Mws = append(st.Mws, w.mws(f, ce.Args)...)
			if key := keyOf(sel.X); key != "" {
				env[key] = st
			}
			return
		case "Route", "Group":
			// chi: r.Group(func(r chi.Router) {...}) keeps the same prefix
			if len(ce.Args) == 1 {
				if lit, ok := ce.Args[0].(*ast.FuncLit); ok {
					w.enterGroup(f, lit, st.Prefix, st.Mws, env)
					return
				}
			}
			// chi: r.Route("/x", func(r chi.Router) {...})
			if len(ce.Args) >= 2 {
				if lit, ok := ce.Args[1].(*ast.FuncLit); ok {
					w.enterGroup(f, lit, joinPath(st.Prefix, w.pathOf(f, ce.Args[0])), st.Mws, env)
					return
				}
			}
		}
		if method, isRoute := routeVerbs[sel.Sel.Name]; isRoute && len(ce.Args) > 0 {
			w.addCallRoute(f, ce, st, method, sel.Sel.Name)
			return
		}
	}

	// net/http package-level registration: http.HandleFunc("/x", h)
	if pkg, ok := f.File.Imports[identName(sel.X)]; ok && pkg == "net/http" {
		if sel.Sel.Name == "Handle" || sel.Sel.Name == "HandleFunc" {
			if len(ce.Args) > 0 {
				w.addCallRoute(f, ce, State{}, "ANY", sel.Sel.Name)
			}
			return
		}
	}

	// cross function route registration: RegisterRoute(group), RegisterGroup(engine)
	if target := w.resolveCallee(f, sel); target != nil && len(ce.Args) > 0 {
		for i, arg := range ce.Args {
			if id := keyOf(arg); id != "" {
				if typ := w.zeroOf(f, id); typ != "" {
					w.setZero(target, paramName(target, i), typ)
				}
			}
			s, ok := w.stateOf(f, arg, env)
			if !ok {
				continue
			}
			if pn := paramName(target, i); pn != "" {
				w.setState(target, pn, s)
			}
		}
		w.inlineBound(target, ce, f, env)
	}
}

// inlineBound runs a registration helper once for this call site so path
// arguments survive. A helper called twice with different literals
// (Register(router, "/ws", ...) and Register(router, "/ws-syspipe", ...))
// cannot be represented by one per-function state.
func (w *Walker) inlineBound(target *Func, ce *ast.CallExpr, caller *Func, callerEnv map[string]State) {
	if !w.bindable(target) {
		return
	}
	env := map[string]State{}
	for i, arg := range ce.Args {
		pn := paramName(target, i)
		if pn == "" {
			continue
		}
		if st, ok := w.stateOf(caller, arg, callerEnv); ok {
			env[pn] = st
		}
	}
	bind := map[string]string{}
	for k, v := range w.pathArgs[target.Key] {
		bind[k] = v
	}
	for i, arg := range ce.Args {
		pn := paramName(target, i)
		if pn == "" {
			continue
		}
		if pv := w.pathOf(caller, arg); pv != "" && pv != ":dynamic" && !strings.HasPrefix(pv, ":") {
			bind[pn] = pv
		}
	}
	var entry *Func
	for _, arg := range ce.Args {
		if fn := w.streamEntry(caller, arg); fn != nil {
			entry = fn
			break
		}
	}
	oldEntry := w.entry
	if entry != nil {
		w.entry = entry
	}
	old := w.pathArgs[target.Key]
	w.pathArgs[target.Key] = bind
	w.body(target, target.Decl.Body.List, env)
	if old == nil {
		delete(w.pathArgs, target.Key)
	} else {
		w.pathArgs[target.Key] = old
	}
	w.entry = oldEntry
}

// streamEntry follows a handler factory such as frontsocket.NewHandler to the
// concrete OnMessage method. Generated/factory returns are often interfaces,
// so package ownership of the factory is the reliable discriminator.
func (w *Walker) streamEntry(f *Func, e ast.Expr) *Func {
	if lit, ok := e.(*ast.CompositeLit); ok {
		typ := strings.TrimPrefix(typeString(lit.Type), "*")
		for _, m := range w.m.ByRecv[typ] {
			if m.Name == "OnMessage" {
				return m
			}
		}
	}
	target := w.resolveAny(f, e)
	if target == nil {
		return nil
	}
	for _, m := range w.m.Methods["OnMessage"] {
		if m.PkgPath == target.PkgPath && m.Recv != "" {
			return m
		}
	}
	return nil
}

func (w *Walker) bindable(f *Func) bool {
	if f == nil || f.Decl == nil || f.Decl.Body == nil || f.Decl.Type.Params == nil {
		return false
	}
	params := map[string]bool{}
	for _, p := range f.Decl.Type.Params.List {
		for _, n := range p.Names {
			params[n.Name] = true
		}
	}
	if len(params) == 0 {
		return false
	}
	found := false
	ast.Inspect(f.Decl.Body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok || len(ce.Args) == 0 {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if _, route := routeVerbs[sel.Sel.Name]; !route {
			return true
		}
		if id, ok := ce.Args[0].(*ast.Ident); ok && params[id.Name] {
			found = true
			return false
		}
		return true
	})
	return found
}

// addCallRoute records one verb call on a router state. The path can be a
// literal, a concatenation ("get"+id), or a stdlib method pattern
// ("GET /users/{id}").
func (w *Walker) addCallRoute(f *Func, ce *ast.CallExpr, st State, method, name string) {
	if len(ce.Args) == 0 {
		return
	}
	raw := w.pathOf(f, ce.Args[0])
	if raw == ":dynamic" || strings.HasPrefix(raw, ":") {
		return
	}
	if raw == "" && st.Prefix == "" {
		return
	}
	method, raw = splitMethodPath(method, strings.TrimSpace(raw))
	path := joinPath(st.Prefix, raw)
	var tail []Ref
	if len(ce.Args) > 1 {
		tail = w.refs(f, ce.Args[1:])
		for i := range tail {
			if tail[i].Kind == "closure" {
				tail[i].Name = "inline handler"
			}
			tail[i].Role = "handler"
			if mwNamePat.MatchString(tail[i].Name) {
				tail[i].Role = "middleware"
			}
		}
	}
	// Fluent middleware (.Bind/.Use) wraps the action, so it runs first.
	chain := append(append([]Ref(nil), st.Mws...), w.extra...)
	chain = append(chain, tail...)
	if len(w.methods) > 0 && (method == "ANY" || name == "HandleFunc" || name == "Handle") {
		for _, mm := range w.methods {
			w.addRoute(f, mm, path, ce, chain)
		}
		return
	}
	w.addRoute(f, method, path, ce, chain)
}

func (w *Walker) enterGroup(f *Func, lit *ast.FuncLit, prefix string, mws []Ref, env map[string]State) {
	child := State{Prefix: prefix, Mws: append([]Ref(nil), mws...)}
	inner := cloneEnv(env)
	if lit.Type.Params != nil && len(lit.Type.Params.List) > 0 && len(lit.Type.Params.List[0].Names) > 0 {
		if pn := identName(lit.Type.Params.List[0].Names[0]); pn != "" {
			inner[pn] = child
		}
	}
	w.body(f, lit.Body.List, inner)
}

// stateOf resolves a router expression: a tracked variable, a group call, or
// a framework field such as PocketBase's e.Router / app.Router.
func (w *Walker) stateOf(f *Func, e ast.Expr, env map[string]State) (State, bool) {
	if key := keyOf(e); key != "" {
		if st, ok := env[key]; ok {
			return st, true
		}
	}
	switch x := e.(type) {
	case *ast.SelectorExpr:
		if x.Sel.Name == "Router" || x.Sel.Name == "RouterGroup" {
			return State{}, true
		}
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return State{}, false
		}
		parent, ok := w.stateOf(f, sel.X, env)
		if !ok {
			return State{}, false
		}
		switch sel.Sel.Name {
		case "Group", "PathPrefix", "Subrouter":
			st := State{Prefix: parent.Prefix, Mws: append([]Ref(nil), parent.Mws...)}
			if len(x.Args) > 0 {
				st.Prefix = joinPath(parent.Prefix, w.pathOf(f, x.Args[0]))
			}
			return st, true
		case "With":
			st := State{Prefix: parent.Prefix, Mws: append([]Ref(nil), parent.Mws...)}
			st.Mws = append(st.Mws, w.mws(f, x.Args)...)
			return st, true
		}
	}
	return State{}, false
}

var methodPrefixes = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE"}

// splitMethodPath reads the Go 1.22 net/http pattern shape: "GET /x".
func splitMethodPath(method, path string) (string, string) {
	if method != "ANY" {
		return method, path
	}
	for _, m := range methodPrefixes {
		if strings.HasPrefix(path, m+" ") {
			return m, strings.TrimSpace(strings.TrimPrefix(path, m))
		}
	}
	return method, path
}

func (w *Walker) addRoute(f *Func, method, path string, at ast.Node, chain []Ref) {
	if !w.recording || strings.Contains(path, ":dynamic") {
		return
	}
	ln := line(at.Pos())
	key := method + " " + path + " " + f.File.Rel
	if w.seen[key] {
		return
	}
	w.seen[key] = true
	parts := strings.Split(strings.Trim(path, "/"), "/")
	group := "/"
	if len(parts) > 0 && parts[0] != "" {
		group = "/" + parts[0]
		// /api/user/... and /api/admin/... are different places, /api is not
		if group == "/api" && len(parts) > 1 && parts[1] != "" {
			group += "/" + parts[1]
		}
	}
	w.routes = append(w.routes, &Route{
		Method: method,
		Path:   path,
		File:   f.File,
		Line:   ln,
		Chain:  chain,
		Group:  group,
		Entry:  w.entry,
	})
}

// returnsFunc reports whether fn hands back a handler, as gin/fiber factories do.
func returnsFunc(fn *Func) bool {
	if fn.Decl == nil || fn.Decl.Type.Results == nil {
		return false
	}
	for _, r := range fn.Decl.Type.Results.List {
		switch t := r.Type.(type) {
		case *ast.FuncType:
			return true
		case *ast.Ident:
			if strings.Contains(t.Name, "Handler") || strings.Contains(t.Name, "Middleware") {
				return true
			}
		case *ast.SelectorExpr:
			if strings.Contains(t.Sel.Name, "Handler") || strings.Contains(t.Sel.Name, "Middleware") {
				return true
			}
		}
	}
	return false
}

var mwNamePat = regexp.MustCompile(`(?i)(middleware|check|guard|require|auth|verify|logger|recover|cors|limit|trace)`)

func (w *Walker) mws(f *Func, exprs []ast.Expr) []Ref {
	out := w.refs(f, exprs)
	for i := range out {
		if out[i].Kind == "closure" {
			out[i].Name = "anonymous middleware"
		}
		out[i].Role = "middleware"
	}
	return out
}

func (w *Walker) refs(f *Func, exprs []ast.Expr) []Ref {
	var out []Ref
	for _, e := range exprs {
		if r, ok := w.ref(f, e); ok {
			out = append(out, r)
		}
	}
	return out
}

func (w *Walker) ref(f *Func, e ast.Expr) (Ref, bool) {
	switch x := e.(type) {
	case *ast.Ident:
		if fn := w.fnvars[f.Key+"."+x.Name]; fn != nil {
			return Ref{Name: fn.Name, Kind: "func", File: fn.File, Line: fn.Start, Fn: fn}, true
		}
		if fn := w.m.PKG[f.PkgPath][x.Name]; fn != nil {
			return Ref{Name: x.Name, Kind: "func", File: fn.File, Line: fn.Start, Fn: fn}, true
		}
		return Ref{Name: x.Name, Kind: "unknown", File: f.File, Line: line(x.Pos())}, true
	case *ast.SelectorExpr:
		base := identName(x.X)
		if p, ok := f.File.Imports[base]; ok {
			if fn := w.m.PKG[p][x.Sel.Name]; fn != nil {
				return Ref{Name: altPkg(p) + "." + x.Sel.Name, Kind: "func", File: fn.File, Line: fn.Start, Fn: fn}, true
			}
			return Ref{Name: base + "." + x.Sel.Name, Kind: "expr", File: f.File, Line: line(x.Pos())}, true
		}
		if ms := w.m.Methods[x.Sel.Name]; len(ms) > 0 {
			if fn := w.methodFor(f, x); fn != nil {
				return Ref{Name: x.Sel.Name, Kind: "method", File: fn.File, Line: fn.Start, Fn: fn}, true
			}
			fn := ms[0]
			return Ref{Name: x.Sel.Name, Kind: "method", File: fn.File, Line: fn.Start, Fn: fn}, true
		}
		_, expr := chain(x)
		return Ref{Name: expr, Kind: "expr", File: f.File, Line: line(x.Pos())}, true
	case *ast.FuncLit:
		syn := &Func{PkgPath: f.PkgPath, Name: "closure", File: f.File,
			Decl: &ast.FuncDecl{Body: x.Body}, Start: line(x.Pos()), End: line(x.End())}
		syn.Key = fnKey(f.PkgPath, "", fmt.Sprintf("closure@%s:%d", f.File.Rel, syn.Start))
		syn.Calls = w.m.callsOf(syn)
		return Ref{Name: "inline closure", Kind: "closure", File: f.File, Line: line(x.Pos()), Fn: syn}, true
	case *ast.CallExpr:
		// a handler factory called inline: app.Get("/x", Activities(*usecase))
		if target := w.resolveAny(f, x.Fun); target != nil && returnsFunc(target) {
			return Ref{Name: altPkg(target.PkgPath) + "." + target.Name, Kind: "func",
				File: target.File, Line: target.Start, Fn: target}, true
		}
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			if base := identName(sel.X); base != "" {
				if _, ok := f.File.Imports[base]; ok {
					return Ref{Name: base + "." + sel.Sel.Name, Kind: "func", File: f.File, Line: line(x.Pos())}, true
				}
			}
			if ms := w.m.Methods[sel.Sel.Name]; len(ms) > 0 {
				if fn := w.methodFor(f, sel); fn != nil {
					return Ref{Name: sel.Sel.Name, Kind: "method", File: fn.File, Line: fn.Start, Fn: fn}, true
				}
				fn := ms[0]
				return Ref{Name: sel.Sel.Name, Kind: "method", File: fn.File, Line: fn.Start, Fn: fn}, true
			}
		}
	}
	return Ref{}, false
}

// resolveAny accepts a package function, a method value, or a call that returns one.
func (w *Walker) resolveAny(f *Func, e ast.Expr) *Func {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return w.resolveCallee(f, x)
	case *ast.Ident:
		return w.m.PKG[f.PkgPath][x.Name]
	case *ast.CallExpr:
		return w.resolveAny(f, x.Fun)
	case *ast.ParenExpr:
		return w.resolveAny(f, x.X)
	}
	return nil
}

// methodFor resolves a method value by the declared type of its receiver, so a
// handler received as a parameter (userHandler *JobHandler) lands on that type's
// method instead of the first method with the same name in the module.
func (w *Walker) methodFor(f *Func, sel *ast.SelectorExpr) *Func {
	typ := w.localType(f, identName(sel.X))
	if typ == "" {
		return nil
	}
	for _, fn := range w.m.Methods[sel.Sel.Name] {
		if fn.Recv == typ {
			return fn
		}
	}
	return nil
}

func (w *Walker) resolveCallee(f *Func, sel *ast.SelectorExpr) *Func {
	base := identName(sel.X)
	if base != "" {
		if p, ok := f.File.Imports[base]; ok {
			return w.m.PKG[p][sel.Sel.Name]
		}
	}
	if ms := w.m.Methods[sel.Sel.Name]; len(ms) > 0 {
		if typ := w.localType(f, base); typ != "" {
			for _, fn := range ms {
				if fn.Recv == typ {
					return fn
				}
			}
		}
		return ms[0]
	}
	return w.m.PKG[f.PkgPath][sel.Sel.Name]
}

// localType reads the concrete receiver type of a local variable so a method
// call like v1Handler.Register(...) lands on the right Register, not the first
// method with that name in the module.
func (w *Walker) localType(f *Func, name string) string {
	if name == "" {
		return ""
	}
	if m := w.types[f.Key]; m != nil {
		if t, ok := m[name]; ok {
			return t
		}
	}
	if f.Decl != nil && f.Decl.Type != nil && f.Decl.Type.Params != nil {
		for _, p := range f.Decl.Type.Params.List {
			for _, n := range p.Names {
				if n.Name == name {
					typ := shortType(p.Type)
					cache := w.types[f.Key]
					if cache == nil {
						cache = map[string]string{}
						w.types[f.Key] = cache
					}
					cache[name] = typ
					return typ
				}
			}
		}
	}
	cache := w.types[f.Key]
	if cache == nil {
		cache = map[string]string{}
		w.types[f.Key] = cache
	}
	cache[name] = ""
	typ := ""
	ast.Inspect(f.Decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				if i < len(x.Rhs) && identName(lhs) == name {
					if t := w.exprType(f, x.Rhs[i]); t != "" {
						typ = t
					}
				}
			}
		case *ast.ValueSpec:
			for i, nm := range x.Names {
				if nm.Name == name && i < len(x.Values) {
					if t := w.exprType(f, x.Values[i]); t != "" {
						typ = t
					}
				}
			}
		}
		return typ == ""
	})
	cache[name] = typ
	return typ
}

func (w *Walker) exprType(f *Func, e ast.Expr) string {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		return w.exprType(f, x.X)
	case *ast.ParenExpr:
		return w.exprType(f, x.X)
	case *ast.CompositeLit:
		return shortType(x.Type)
	case *ast.Ident:
		return w.localType(f, x.Name)
	case *ast.CallExpr:
		if t := w.resolveAny(f, x.Fun); t != nil && t.Decl.Type.Results != nil && len(t.Decl.Type.Results.List) > 0 {
			return shortType(t.Decl.Type.Results.List[0].Type)
		}
	}
	return ""
}

func shortType(e ast.Expr) string {
	s := strings.TrimPrefix(typeString(e), "*")
	if i := strings.Index(s, "["); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

func paramName(f *Func, i int) string {
	if f.Decl.Type.Params == nil || len(f.Decl.Type.Params.List) <= i {
		return ""
	}
	l := f.Decl.Type.Params.List[i]
	if len(l.Names) > 0 {
		return l.Names[0].Name
	}
	return ""
}

func isRootCtor(f *Func, ce *ast.CallExpr) bool {
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	base := identName(sel.X)
	pkg := ""
	if f != nil && f.File != nil {
		pkg = f.File.Imports[base]
	}
	name := sel.Sel.Name
	if pkg == "net/http" && name == "NewServeMux" {
		return true
	}
	if knownRouterPkg(pkg) {
		switch name {
		case "New", "Default", "NewRouter", "NewRouterGroup", "NewEngine", "Classic", "NewControllerRegister", "NewContainer":
			return true
		}
	}
	// fallback for files without import metadata / renamed packages
	switch base {
	case "gin":
		return name == "New" || name == "Default"
	case "fiber", "echo", "httprouter", "fasthttp", "goji", "pat", "bone", "iris":
		return name == "New"
	case "chi", "mux", "router":
		return name == "NewRouter" || name == "NewRouterGroup"
	case "http":
		return name == "NewServeMux"
	case "web":
		return name == "New" || name == "NewControllerRegister"
	}
	return false
}

func knownRouterPkg(pkg string) bool {
	if pkg == "" {
		return false
	}
	for _, s := range []string{
		"gin-gonic/gin", "labstack/echo", "gofiber/fiber", "go-chi/chi",
		"gorilla/mux", "julienschmidt/httprouter", "pocketbase/pocketbase/tools/router",
		"valyala/fasthttp/router", "cloudwego/hertz", "goji.io",
		"kataras/iris", "emicklei/go-restful", "go-macaron/macaron", "beego",
	} {
		if pkg == s || strings.HasSuffix(pkg, "/"+s) {
			return true
		}
	}
	return false
}

func compositeLit(e ast.Expr) *ast.CompositeLit {
	for {
		u, ok := e.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			break
		}
		e = u.X
	}
	lit, _ := e.(*ast.CompositeLit)
	return lit
}

func (w *Walker) setZero(f *Func, name, typ string) {
	if name == "" || typ == "" {
		return
	}
	m := w.zeros[f.Key]
	if m == nil {
		m = map[string]string{}
		w.zeros[f.Key] = m
	}
	if m[name] != typ {
		m[name] = typ
		w.changed = true
	}
}

func (w *Walker) zeroOf(f *Func, name string) string {
	if name == "" {
		return ""
	}
	return w.zeros[f.Key][name]
}

// pathOf resolves a route path, including fields of a zero-valued options
// struct: PBRegisterOption{} leaves BaseURL empty, so options.BaseURL+"..." is
// just "...". Unknown prefixes stay dynamic and are skipped.
func (w *Walker) pathOf(f *Func, e ast.Expr) string {
	if id := identName(e); id != "" {
		if v, ok := w.pathArgs[f.Key][id]; ok {
			return v
		}
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return w.pathOf(f, x.X)
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			l, r := w.pathOf(f, x.X), w.pathOf(f, x.Y)
			if l == "" {
				return r
			}
			if r == "" {
				return l
			}
			return l + r
		}
	case *ast.SelectorExpr:
		if w.zeroOf(f, keyOf(x.X)) != "" {
			return ""
		}
	}
	return pathLit(e)
}

func pathLit(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		s := strings.Trim(x.Value, "`\"")
		return s
	case *ast.ParenExpr:
		return pathLit(x.X)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return ":dynamic"
		}
		l, r := pathLit(x.X), pathLit(x.Y)
		if l == ":dynamic" {
			return r
		}
		if r == ":dynamic" {
			return l
		}
		return l + r
	case *ast.Ident:
		return ":" + x.Name
	}
	return ":dynamic"
}

func joinPath(prefix, p string) string {
	if p == "" {
		return prefix
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if prefix == "" {
		return p
	}
	return strings.TrimSuffix(prefix, "/") + p
}

func altPkg(path string) string {
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	if len(parts) > 1 && (strings.HasPrefix(name, "v") && len(name) <= 2) {
		name = parts[len(parts)-2]
	}
	return name
}

func cloneEnv(in map[string]State) map[string]State {
	out := make(map[string]State, len(in))
	for k, v := range in {
		out[k] = State{Prefix: v.Prefix, Mws: append([]Ref(nil), v.Mws...)}
	}
	return out
}

func sortedKeys(m map[string]*Func) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
