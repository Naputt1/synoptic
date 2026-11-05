package main

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// JInput is one thing a route reads or writes on the request/response surface.
// It is deliberately evidence-based: a key is only listed when a call in the
// route chain (or a helper it calls) proves the access.
type JInput struct {
	Kind   string   `json:"k"` // path, query, ctx, header, cookie, form
	Name   string   `json:"n"`
	Type   string   `json:"t,omitempty"`
	Access string   `json:"a"`             // read, set, read+set, unused, undeclared, dynamic, bound
	SetBy  string   `json:"sb,omitempty"`  // stage that reaches the write
	SetIn  string   `json:"si,omitempty"`  // helper that performs the write, when different
	SetAt  string   `json:"sa,omitempty"`  // file:line of the write
	SetAts []string `json:"sas,omitempty"` // every write location, for repeated keys
	ReadBy string   `json:"rb,omitempty"`  // stage that reaches the read
	ReadIn string   `json:"ri,omitempty"`  // helper that performs the read, when different
	ReadAt string   `json:"ra,omitempty"`  // file:line of the read
	Reads  int      `json:"r,omitempty"`
	Sets   int      `json:"s,omitempty"`
	Req    bool     `json:"rq,omitempty"`
	Expr   string   `json:"e,omitempty"` // unresolved key expression
}

type constDecl struct {
	file *FileCtx
	expr ast.Expr
}

var ctxGetterTypes = map[string]string{
	"GetUint": "uint", "GetUint64": "uint64", "GetInt": "int", "GetInt64": "int64",
	"GetString": "string", "GetBool": "bool", "GetFloat64": "float64", "GetTime": "time.Time",
}

// inputIndex holds the two things the input scanner needs to look through:
// string constants (so config.CONTEXT_USER_ID reads as userID) and named
// struct types (so a bound query struct expands into its fields).
type inputIndex struct {
	consts map[string]map[string]string
	types  map[string]map[string]*ast.StructType
}

var inputIndexes = map[*Module]*inputIndex{}

func inputsIndex(m *Module) *inputIndex {
	if ix := inputIndexes[m]; ix != nil {
		return ix
	}
	ix := &inputIndex{
		consts: map[string]map[string]string{},
		types:  map[string]map[string]*ast.StructType{},
	}
	pending := map[string]map[string]constDecl{}
	for _, f := range m.Files {
		pkg := f.PkgPath
		if ix.consts[pkg] == nil {
			ix.consts[pkg] = map[string]string{}
		}
		if ix.types[pkg] == nil {
			ix.types[pkg] = map[string]*ast.StructType{}
		}
		if pending[pkg] == nil {
			pending[pkg] = map[string]constDecl{}
		}
		for _, d := range f.Ast.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			switch gd.Tok {
			case token.CONST:
				var prev []ast.Expr
				for _, sp := range gd.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok {
						continue
					}
					vals := vs.Values
					if len(vals) == 0 {
						vals = prev
					}
					prev = vals
					for i, nm := range vs.Names {
						if i < len(vals) {
							pending[pkg][nm.Name] = constDecl{file: f, expr: vals[i]}
						}
					}
				}
			case token.TYPE:
				for _, sp := range gd.Specs {
					ts, ok := sp.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if st, ok := ts.Type.(*ast.StructType); ok {
						ix.types[pkg][ts.Name.Name] = st
					}
				}
			}
		}
	}
	// Constants can point at other constants, so settle them in a few passes.
	for pass := 0; pass < 5; pass++ {
		changed := false
		for pkg, names := range pending {
			for name, d := range names {
				if _, ok := ix.consts[pkg][name]; ok {
					continue
				}
				if v, ok := ix.evalString(d.file, d.expr); ok {
					ix.consts[pkg][name] = v
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	inputIndexes[m] = ix
	return ix
}

func (ix *inputIndex) evalString(f *FileCtx, e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			s, err := strconv.Unquote(x.Value)
			return s, err == nil
		}
	case *ast.ParenExpr:
		return ix.evalString(f, x.X)
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			a, ok := ix.evalString(f, x.X)
			if !ok {
				return "", false
			}
			b, ok := ix.evalString(f, x.Y)
			if !ok {
				return "", false
			}
			return a + b, true
		}
	case *ast.Ident:
		v, ok := ix.consts[f.PkgPath][x.Name]
		return v, ok
	case *ast.SelectorExpr:
		if base := identName(x.X); base != "" {
			if p, ok := f.Imports[base]; ok {
				v, ok := ix.consts[p][x.Sel.Name]
				return v, ok
			}
		}
	}
	return "", false
}

func (ix *inputIndex) structOf(f *FileCtx, typ string) *ast.StructType {
	typ = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(typ), "*"))
	if strings.HasPrefix(typ, "[]") {
		typ = strings.TrimPrefix(typ, "[]")
	}
	if i := strings.Index(typ, "["); i > 0 {
		typ = typ[:i]
	}
	if dot := strings.LastIndex(typ, "."); dot >= 0 {
		base := typ[:dot]
		if p, ok := f.Imports[base]; ok {
			return ix.types[p][typ[dot+1:]]
		}
		return nil
	}
	return ix.types[f.PkgPath][typ]
}

type inputHit struct {
	kind   string
	name   string
	typ    string
	access string
}

type inputCollector struct {
	m     *Module
	r     *Route
	ix    *inputIndex
	items map[string]*JInput
	scans int
}

func analyzeInputs(m *Module, r *Route, h *Func) []JInput {
	c := &inputCollector{m: m, r: r, ix: inputsIndex(m), items: map[string]*JInput{}}
	seedPath(c, r.Path)

	seen := map[string]bool{}
	for _, ref := range r.Chain {
		if ref.Fn == nil || ref.Fn.Decl == nil || ref.Fn.Decl.Body == nil || seen[ref.Fn.Key] {
			continue
		}
		seen[ref.Fn.Key] = true
		c.scan(ref.Fn, nil, 0, map[string]bool{ref.Fn.Key: true}, fnName(ref.Fn))
	}
	if h != nil && !seen[h.Key] && h.Decl != nil && h.Decl.Body != nil {
		c.scan(h, nil, 0, map[string]bool{h.Key: true}, fnName(h))
	}
	return c.sorted()
}

// analyzeStageInputs reports what one chain step actually touches. Unlike the
// route document it does not list path parameters the step never reads.
func analyzeStageInputs(m *Module, fn *Func, path string) []JInput {
	if fn == nil || fn.Decl == nil || fn.Decl.Body == nil {
		return nil
	}
	c := &inputCollector{m: m, ix: inputsIndex(m), items: map[string]*JInput{}}
	seedPath(c, path)
	c.scan(fn, nil, 0, map[string]bool{fn.Key: true}, fnName(fn))
	all := c.sorted()
	out := make([]JInput, 0, len(all))
	for _, in := range all {
		if in.Kind == "path" && in.Access == "unused" {
			continue
		}
		out = append(out, in)
	}
	return out
}

func seedPath(c *inputCollector, path string) {
	for _, seg := range strings.Split(path, "/") {
		name, typ := "", "string"
		switch {
		case strings.HasPrefix(seg, ":"):
			name = strings.TrimSuffix(seg[1:], "?")
		case strings.HasPrefix(seg, "*"):
			name, typ = seg[1:], "path"
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
			name = strings.Trim(seg, "{}")
		}
		if name != "" {
			c.items[c.itemKey("path", name)] = &JInput{
				Kind: "path", Name: name, Type: typ, Access: "unused",
			}
		}
	}
}

func (c *inputCollector) itemKey(kind, name string) string { return kind + "\x00" + name }

func (c *inputCollector) read(kind, name, typ, by, via string, at int, f *FileCtx) {
	if name == "" {
		return
	}
	key := c.itemKey(kind, name)
	in := c.items[key]
	if in == nil {
		access := "read"
		if kind == "path" {
			access = "undeclared"
		}
		in = &JInput{Kind: kind, Name: name, Type: typ, Access: access}
		c.items[key] = in
	}
	in.Reads++
	if in.ReadBy == "" {
		in.ReadBy, in.ReadAt = by, locOf(f, at)
		if via != by {
			in.ReadIn = via
		}
	}
	if in.Sets > 0 {
		in.Access = "read+set"
	} else if in.Access == "unused" {
		in.Access = "read"
	}
	if in.Type == "" {
		in.Type = typ
	}
}

func (c *inputCollector) set(kind, name, typ, by, via string, at int, f *FileCtx) {
	if name == "" {
		return
	}
	key := c.itemKey(kind, name)
	in := c.items[key]
	if in == nil {
		in = &JInput{Kind: kind, Name: name, Type: typ, Access: "set"}
		c.items[key] = in
	}
	in.Sets++
	atStr := locOf(f, at)
	if in.SetBy == "" {
		in.SetBy, in.SetAt = by, atStr
		if via != by {
			in.SetIn = via
		}
	}
	found := false
	for _, old := range in.SetAts {
		if old == atStr {
			found = true
			break
		}
	}
	if !found {
		in.SetAts = append(in.SetAts, atStr)
	}
	if in.Reads > 0 {
		in.Access = "read+set"
	} else if in.Access != "read+set" {
		in.Access = "set"
	}
	if in.Type == "" {
		in.Type = typ
	}
}

func (c *inputCollector) bound(kind, name, typ, by, via string, at int, f *FileCtx, req bool) {
	if name == "" {
		return
	}
	key := c.itemKey(kind, name)
	in := c.items[key]
	if in == nil {
		in = &JInput{Kind: kind, Name: name, Type: typ, Access: "bound"}
		c.items[key] = in
	}
	in.Req = in.Req || req
	if in.ReadBy == "" {
		in.ReadBy, in.ReadAt = by, locOf(f, at)
		if via != by {
			in.ReadIn = via
		}
	}
}

func (c *inputCollector) dynamic(kind, expr, by string, at int, f *FileCtx) {
	if expr == "" {
		return
	}
	key := c.itemKey(kind, "~"+expr)
	if _, ok := c.items[key]; ok {
		return
	}
	c.items[key] = &JInput{
		Kind: kind, Name: expr, Access: "dynamic", Expr: expr,
		ReadBy: by, ReadAt: locOf(f, at),
	}
}

func (c *inputCollector) scan(fn *Func, args []string, depth int, stack map[string]bool, owner string) {
	if fn == nil || fn.Decl == nil || fn.Decl.Body == nil {
		return
	}
	c.scans++
	if c.scans > 400 {
		return
	}
	by := owner
	if by == "" {
		by = fnName(fn)
	}
	via := fnName(fn)
	params := funcParams(fn)
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		at := line(ce.Pos())
		if h, ok := c.hit(fn, ce, params, args); ok {
			switch h.access {
			case "read":
				c.read(h.kind, h.name, h.typ, by, via, at, fn.File)
			case "set":
				c.set(h.kind, h.name, h.typ, by, via, at, fn.File)
			case "bind":
				c.bindQuery(fn, ce, by, via, at)
			}
		}
		if depth >= 2 {
			return true
		}
		t := c.target(fn, ce)
		if t == nil || t.Decl == nil || t.Decl.Body == nil || stack[t.Key] {
			return true
		}
		keys := make([]string, len(ce.Args))
		for i, a := range ce.Args {
			keys[i], _ = c.resolveKey(fn, a, nil, nil)
		}
		next := map[string]bool{}
		for k := range stack {
			next[k] = true
		}
		next[t.Key] = true
		c.scan(t, keys, depth+1, next, by)
		return true
	})
}

func (c *inputCollector) hit(fn *Func, ce *ast.CallExpr, params, args []string) (inputHit, bool) {
	sel, base, expr := callParts(ce)
	if sel == "" {
		return inputHit{}, false
	}
	key := func(i int) (string, string) {
		if i < 0 || i >= len(ce.Args) {
			return "", ""
		}
		return c.resolveKey(fn, ce.Args[i], params, args)
	}
	ctxBase := base == "c" || base == "ctx" || base == "r" || base == "req" || base == "w"
	mk := func(kind, typ, access string, i int) (inputHit, bool) {
		n, ex := key(i)
		if n == "" {
			c.dynamic(kind, ex, fnName(fn), line(ce.Pos()), fn.File)
			return inputHit{}, false
		}
		return inputHit{kind: kind, name: n, typ: typ, access: access}, true
	}

	// response headers are written either through c.Header(name, value) or
	// through the header map itself (h.Set after h := c.Writer.Header()).
	// check this before the generic Set case, which would read the direct
	// c.Writer.Header().Set chain as a context write.
	if (base == "c" || base == "ctx") && sel == "Header" && len(ce.Args) >= 2 {
		return mk("header", "string", "set", 0)
	}
	if sel == "Set" || sel == "Add" {
		if c, ok := describeCall(ce); ok && isResponseHeaderWrite(c, fn) {
			return mk("header", "string", "set", 0)
		}
	}

	switch sel {
	case "Param", "Params":
		if !ctxBase || len(ce.Args) == 0 {
			return inputHit{}, false
		}
		return mk("path", "string", "read", 0)
	case "PathValue":
		if base != "r" && base != "req" {
			return inputHit{}, false
		}
		return mk("path", "string", "read", 0)
	case "URLParam": // chi.URLParam(r, "id")
		if base != "chi" {
			return inputHit{}, false
		}
		return mk("path", "string", "read", 1)
	case "Query":
		if (base != "c" && base != "ctx") || len(ce.Args) == 0 {
			return inputHit{}, false
		}
		return mk("query", "string", "read", 0)
	case "DefaultQuery", "GetQuery", "QueryParam":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("query", "string", "read", 0)
	case "QueryArray":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("query", "[]string", "read", 0)
	case "QueryMap":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("query", "map[string]string", "read", 0)
	case "QueryInt":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("query", "int", "read", 0)
	case "QueryBool":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("query", "bool", "read", 0)
	case "QueryFloat":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("query", "float64", "read", 0)
	case "ShouldBindQuery", "BindQuery", "QueryParser", "BindQueryString":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return inputHit{access: "bind"}, true
	case "GetHeader":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("header", "string", "read", 0)
	case "Header":
		return inputHit{}, false
	case "Get":
		switch {
		case (strings.Contains(expr, ".Request.Header") || base == "r" || base == "req") && strings.Contains(expr, ".Header"):
			return mk("header", "string", "read", 0)
		case strings.Contains(expr, "URL.Query()") || strings.HasSuffix(expr, ".Query"):
			return mk("query", "string", "read", 0)
		case base == "c" || base == "ctx":
			return mk("ctx", "", "read", 0)
		}
	case "MustGet":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("ctx", "", "read", 0)
	case "GetUint", "GetUint64", "GetInt", "GetInt64", "GetString", "GetBool", "GetFloat64", "GetTime":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("ctx", ctxGetterTypes[sel], "read", 0)
	case "Value":
		if base == "ctx" || base == "context" {
			return mk("ctx", "", "read", 0)
		}
	case "Set":
		switch {
		case strings.Contains(expr, ".Request.Header") || base == "r" || base == "req":
			return mk("header", "string", "set", 0)
		case base == "c" || base == "ctx":
			return mk("ctx", "", "set", 0)
		}
	case "Locals":
		if base != "c" {
			return inputHit{}, false
		}
		if len(ce.Args) >= 2 {
			return mk("ctx", "", "set", 0)
		}
		return mk("ctx", "", "read", 0)
	case "WithValue":
		if base == "context" {
			return mk("ctx", "", "set", 1)
		}
	case "Cookie":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("cookie", "string", "read", 0)
	case "SetCookie":
		if base != "c" && base != "ctx" {
			return inputHit{}, false
		}
		return mk("cookie", "string", "set", 0)
	case "FormValue", "PostForm", "GetFormValue":
		if !ctxBase {
			return inputHit{}, false
		}
		return mk("form", "string", "read", 0)
	}
	return inputHit{}, false
}

func (c *inputCollector) bindQuery(fn *Func, ce *ast.CallExpr, by, via string, at int) {
	if len(ce.Args) == 0 {
		return
	}
	name := identOf(ce.Args[0])
	typ := c.varType(fn, name)
	if typ == "" {
		c.dynamic("query", exprText(ce.Args[0]), by, at, fn.File)
		return
	}
	st := c.ix.structOf(fn.File, typ)
	if st == nil {
		c.bound("query", typ, typ, by, via, at, fn.File, false)
		return
	}
	for _, f := range st.Fields.List {
		tag := ""
		if f.Tag != nil {
			tag = strings.Trim(f.Tag.Value, "`")
		}
		key := tagValue(tag, "form")
		if key == "" {
			key = tagValue(tag, "query")
		}
		if key == "" {
			key = tagValue(tag, "json")
		}
		if key == "-" {
			continue
		}
		names := []string{}
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
		if len(names) == 0 {
			names = []string{key}
		}
		for _, n := range names {
			if key != "" && len(names) == 1 {
				n = key
			}
			c.bound("query", n, exprText(f.Type), by, via, at, fn.File,
				strings.Contains(tag, "required"))
		}
	}
}

func (c *inputCollector) varType(fn *Func, name string) string {
	if name == "" {
		return ""
	}
	var typ string
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		if typ != "" {
			return false
		}
		switch x := n.(type) {
		case *ast.DeclStmt:
			gd, ok := x.Decl.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, sp := range gd.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, nm := range vs.Names {
					if nm.Name == name && vs.Type != nil {
						typ = exprText(vs.Type)
					}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name != name || i >= len(x.Rhs) {
					continue
				}
				rhs := x.Rhs[i]
				if u, ok := rhs.(*ast.UnaryExpr); ok {
					rhs = u.X
				}
				if cl, ok := rhs.(*ast.CompositeLit); ok && cl.Type != nil {
					typ = exprText(cl.Type)
				}
				if ce, ok := rhs.(*ast.CallExpr); ok && callName(ce) == "new" && len(ce.Args) > 0 {
					typ = exprText(ce.Args[0])
				}
			}
		}
		return true
	})
	return typ
}

func (c *inputCollector) resolveKey(fn *Func, e ast.Expr, params, args []string) (string, string) {
	e = unwrapExpr(e)
	if id, ok := e.(*ast.Ident); ok {
		for i, p := range params {
			if p == id.Name && i < len(args) && args[i] != "" {
				return args[i], ""
			}
		}
	}
	if s, ok := c.ix.evalString(fn.File, e); ok {
		return s, ""
	}
	return "", exprText(e)
}

func (c *inputCollector) target(fn *Func, ce *ast.CallExpr) *Func {
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		if id, ok := ce.Fun.(*ast.Ident); ok {
			return c.m.PKG[fn.PkgPath][id.Name]
		}
		if call, ok := ce.Fun.(*ast.CallExpr); ok {
			return c.target(fn, call)
		}
		return nil
	}
	base := identName(sel.X)
	// framework receivers are accessors, not app code to follow
	switch base {
	case "c", "ctx", "r", "w", "req", "res", "context":
		return nil
	}
	if p, ok := fn.File.Imports[base]; ok {
		return c.m.PKG[p][sel.Sel.Name]
	}
	if ms := c.m.Methods[sel.Sel.Name]; len(ms) > 0 {
		return ms[0]
	}
	return c.m.PKG[fn.PkgPath][sel.Sel.Name]
}

func (c *inputCollector) sorted() []JInput {
	order := map[string]int{"path": 0, "query": 1, "ctx": 2, "header": 3, "cookie": 4, "form": 5}
	out := make([]JInput, 0, len(c.items))
	for _, in := range c.items {
		out = append(out, *in)
	}
	sort.Slice(out, func(i, j int) bool {
		if order[out[i].Kind] != order[out[j].Kind] {
			return order[out[i].Kind] < order[out[j].Kind]
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func callParts(ce *ast.CallExpr) (sel, base, expr string) {
	c, ok := describeCall(ce)
	if !ok {
		return "", "", ""
	}
	return c.Sel, c.Base, c.Expr
}

func funcParams(fn *Func) []string {
	if fn == nil || fn.Decl == nil || fn.Decl.Type == nil || fn.Decl.Type.Params == nil {
		return nil
	}
	var out []string
	for _, f := range fn.Decl.Type.Params.List {
		for _, n := range f.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

func unwrapExpr(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.UnaryExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		default:
			return e
		}
	}
}

func tagValue(tag, key string) string {
	needle := key + `:"`
	i := strings.Index(tag, needle)
	if i < 0 {
		return ""
	}
	rest := tag[i+len(needle):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return strings.Split(rest[:j], ",")[0]
}

func locOf(f *FileCtx, line int) string {
	if f == nil || line == 0 {
		return ""
	}
	return f.Rel + ":" + strconv.Itoa(line)
}
