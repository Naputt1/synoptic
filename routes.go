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
}

type Walker struct {
	m            *Module
	states       map[string]map[string]State
	routes       []*Route
	seen         map[string]bool
	routerFields map[string]map[string]bool
	recording    bool
	methods      []string
	fnvars       map[string]*Func
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
	"Any": "ANY", "Handle": "ANY", "HandleFunc": "ANY",
}

func buildRoutes(m *Module) []*Route {
	w := &Walker{m: m, states: map[string]map[string]State{}, seen: map[string]bool{}, routerFields: map[string]map[string]bool{}, fnvars: map[string]*Func{}}
	w.seed()
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
					if ce, ok := rhs.(*ast.CallExpr); ok && isRootCtor(ce) {
						w.setState(f, name, State{})
					}
				}
			case *ast.ValueSpec:
				for i, v := range x.Values {
					if i >= len(x.Names) {
						break
					}
					if ce, ok := v.(*ast.CallExpr); ok && isRootCtor(ce) {
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
			w.composite(r, env)
		}
	case *ast.LabeledStmt:
		w.stmt(f, x.Stmt, env)
	}
}

// composite carries a router held in a local variable into the struct that
// stores it, so the type's methods start from the same chain.
func (w *Walker) composite(e ast.Expr, env map[string]State) {
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
	if _, ok := rhs.(*ast.CompositeLit); ok {
		w.composite(rhs, env)
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
	if isRootCtor(ce) {
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
			if p, known := env[keyOf(isel.X)]; known {
				st := State{Prefix: p.Prefix, Mws: append([]Ref(nil), p.Mws...)}
				if len(inner.Args) > 0 {
					st.Prefix = joinPath(p.Prefix, pathLit(inner.Args[0]))
				}
				env[name] = st
				return
			}
		}
		delete(env, name)
		return
	}
	base := keyOf(sel.X)
	parent, known := env[base]
	if !known {
		delete(env, name)
		return
	}
	switch sel.Sel.Name {
	case "Group", "PathPrefix", "Subrouter":
		st := State{Prefix: parent.Prefix, Mws: append([]Ref(nil), parent.Mws...)}
		if len(ce.Args) > 0 {
			st.Prefix = joinPath(parent.Prefix, pathLit(ce.Args[0]))
		}
		if sel.Sel.Name == "Group" && len(ce.Args) > 1 {
			st.Mws = append(st.Mws, w.mws(f, ce.Args[1:])...)
		}
		env[name] = st
	default:
		delete(env, name)
	}
}

// callStmt unwraps the gorilla/mux tail: r.HandleFunc(p, h).Methods("GET", "POST")
func (w *Walker) callStmt(f *Func, ce *ast.CallExpr, env map[string]State) {
	if sel, ok := ce.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Methods" {
		if inner, isCall := sel.X.(*ast.CallExpr); isCall {
			var ms []string
			for _, a := range ce.Args {
				if lit, ok := a.(*ast.BasicLit); ok {
					ms = append(ms, strings.Trim(lit.Value, "\""))
				}
			}
			w.methods = ms
			w.call(f, inner, env)
			w.methods = nil
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
	base := keyOf(sel.X)
	if st, known := env[base]; known {
		switch sel.Sel.Name {
		case "Use":
			st.Mws = append(st.Mws, w.mws(f, ce.Args)...)
			env[base] = st
			return
		case "Route", "Group":
			// chi: r.Route("/x", func(r chi.Router) { ... })
			if len(ce.Args) >= 2 {
				if lit, ok := ce.Args[1].(*ast.FuncLit); ok {
					child := State{Prefix: joinPath(st.Prefix, pathLit(ce.Args[0])), Mws: append([]Ref(nil), st.Mws...)}
					inner := cloneEnv(env)
					if lit.Type.Params != nil && len(lit.Type.Params.List) > 0 {
						if pn := identName(lit.Type.Params.List[0].Names[0]); pn != "" {
							inner[pn] = child
						}
					}
					w.body(f, lit.Body.List, inner)
					return
				}
			}
		}
		if method, isRoute := routeVerbs[sel.Sel.Name]; isRoute && len(ce.Args) > 0 {
			// a path held in a variable is a generic registration helper, not a route
			if _, literal := ce.Args[0].(*ast.BasicLit); !literal {
				return
			}
			path := joinPath(st.Prefix, pathLit(ce.Args[0]))
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
			chain := append(append([]Ref(nil), st.Mws...), tail...)
			if len(w.methods) > 0 && (method == "ANY" || sel.Sel.Name == "HandleFunc" || sel.Sel.Name == "Handle") {
				for _, mm := range w.methods {
					w.addRoute(f, mm, path, ce, chain)
				}
				return
			}
			w.addRoute(f, method, path, ce, chain)
			return
		}
	}

	// cross function route registration: RegisterRoute(group), RegisterGroup(engine)
	if target := w.resolveCallee(f, sel); target != nil && len(ce.Args) > 0 {
		if arg := keyOf(ce.Args[0]); arg != "" {
			if s, ok := env[arg]; ok {
				w.setState(target, paramName(target, 0), s)
			}
		}
	}
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

func (w *Walker) resolveCallee(f *Func, sel *ast.SelectorExpr) *Func {
	if base := identName(sel.X); base != "" {
		if p, ok := f.File.Imports[base]; ok {
			return w.m.PKG[p][sel.Sel.Name]
		}
	}
	if ms := w.m.Methods[sel.Sel.Name]; len(ms) > 0 {
		return ms[0]
	}
	return w.m.PKG[f.PkgPath][sel.Sel.Name]
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

func isRootCtor(ce *ast.CallExpr) bool {
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	base := identName(sel.X)
	switch sel.Sel.Name {
	case "Default", "New", "NewRouter", "NewServeMux":
		switch base {
		case "gin", "fiber", "chi", "mux", "http", "echo":
			return true
		}
	}
	return false
}

func pathLit(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		s := strings.Trim(x.Value, "`\"")
		return s
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
