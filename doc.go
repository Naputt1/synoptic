package main

import (
	"go/ast"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// JField is one input: a path param, a query key, or a body field.
type JField struct {
	N   string `json:"n"`
	T   string `json:"t,omitempty"`
	Tag string `json:"tg,omitempty"`
	Req bool   `json:"rq,omitempty"`
}

// JResp is one way the route can answer.
type JResp struct {
	Code string   `json:"c"`
	Keys []string `json:"k,omitempty"`
}

// JDoc is what a route page can honestly say about an endpoint. Every field is
// read from the source; anything the reader could not prove stays empty rather
// than guessed.
type JDoc struct {
	Path     []JField `json:"path,omitempty"`
	Query    []JField `json:"query,omitempty"`
	Inputs   []JInput `json:"in,omitempty"`
	Body     []JField `json:"body,omitempty"`
	BodyType string   `json:"bt,omitempty"`
	Resp     []JResp  `json:"resp,omitempty"`
	Auth     bool     `json:"auth,omitempty"`
	AuthWhy  string   `json:"aw,omitempty"`
	Tables   []string `json:"tables,omitempty"`
	Owner    string   `json:"owner,omitempty"`
	Commit   string   `json:"commit,omitempty"`
	Age      string   `json:"age,omitempty"`
	Curl     string   `json:"curl,omitempty"`
}

var (
	tableRe   = regexp.MustCompile(`Table\("([^"]+)"\)`)
	modelRe   = regexp.MustCompile(`Model\(&?([A-Za-z_][\w.]*)\{`)
	jsonKeyRe = regexp.MustCompile(`"([A-Za-z_][\w]*)"\s*:`)
)

func buildDoc(m *Module, r *Route) *JDoc {
	var h *Func
	for _, ref := range r.Chain {
		if ref.Fn != nil && (ref.Role == "handler" || h == nil) {
			h = ref.Fn
		}
	}
	if h == nil {
		return nil
	}
	d := &JDoc{}

	d.Inputs = analyzeInputs(m, r, h)
	for _, in := range d.Inputs {
		switch in.Kind {
		case "path":
			if in.Access == "undeclared" {
				continue
			}
			t := in.Type
			if t == "" {
				t = "string"
			}
			d.Path = append(d.Path, JField{N: in.Name, T: t})
		case "query":
			if in.Access == "bound" || in.Name == "" {
				continue
			}
			d.Query = append(d.Query, JField{N: in.Name, T: in.Type, Req: in.Req})
		}
	}
	sort.Slice(d.Query, func(i, j int) bool { return d.Query[i].N < d.Query[j].N })

	// request body: the struct handed to a binder
	var bindVar string
	ast.Inspect(h.Decl.Body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch callName(ce) {
		case "ShouldBindJSON", "BindJSON", "ShouldBind", "BodyParser", "Decode", "Unmarshal":
			if len(ce.Args) > 0 {
				bindVar = identOf(ce.Args[0])
			}
		}
		return true
	})
	if bindVar != "" {
		d.Body, d.BodyType = bodyFields(h, bindVar)
	}

	// responses: exits and returns, with the keys of a literal JSON body
	seenResp := map[string]bool{}
	var walk func([]Step)
	walk = func(steps []Step) {
		for _, s := range steps {
			if s.Kind == "ret" || s.Kind == "exit" {
				code := s.Code
				if code == "" {
					code = "—"
				}
				keys := respKeys(s.Text)
				id := code + strings.Join(keys, ",")
				if !seenResp[id] {
					seenResp[id] = true
					d.Resp = append(d.Resp, JResp{Code: code, Keys: keys})
				}
			}
			walk(s.Body)
			walk(s.Else)
		}
	}
	walk(buildFlow(h))
	sort.Slice(d.Resp, func(i, j int) bool { return d.Resp[i].Code < d.Resp[j].Code })

	for _, ref := range r.Chain {
		n := strings.ToLower(ref.Name)
		if strings.Contains(n, "auth") || strings.Contains(n, "jwt") || strings.Contains(n, "session") ||
			strings.Contains(n, "login") || strings.Contains(n, "token") || strings.Contains(n, "guard") {
			d.Auth = true
			d.AuthWhy = ref.Name
			break
		}
	}

	d.Tables = tablesOf(m, h)
	if meta := gitMeta(m.Root, h.File.Rel); meta != nil {
		d.Owner, d.Commit, d.Age = meta[0], meta[1], meta[2]
	}
	d.Curl = curlFor(r, d)
	return d
}

func callName(ce *ast.CallExpr) string {
	switch f := ce.Fun.(type) {
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	}
	return ""
}

func firstString(ce *ast.CallExpr) string {
	for _, a := range ce.Args {
		if lit, ok := a.(*ast.BasicLit); ok && lit.Kind.String() == "STRING" {
			if v, err := strconv.Unquote(lit.Value); err == nil {
				return v
			}
		}
	}
	return ""
}

func identOf(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.UnaryExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// bodyFields finds the declaration of the bound variable. Only inline struct
// types are expanded; a named type is reported as a name, never guessed at.
func bodyFields(h *Func, name string) ([]JField, string) {
	var out []JField
	var named string
	add := func(st *ast.StructType) {
		for _, f := range st.Fields.List {
			typ := exprText(f.Type)
			var tag, jsonName string
			req := false
			if f.Tag != nil {
				tag = strings.Trim(f.Tag.Value, "`")
				if mm := regexp.MustCompile(`json:"([^",]+)`).FindStringSubmatch(tag); len(mm) > 1 {
					jsonName = mm[1]
				}
				if strings.Contains(tag, "required") || strings.Contains(tag, "binding:\"required") {
					req = true
				}
			}
			names := []string{}
			for _, n := range f.Names {
				names = append(names, n.Name)
			}
			if len(names) == 0 {
				names = []string{jsonName}
			}
			for _, n := range names {
				if jsonName != "" && len(names) == 1 {
					n = jsonName
				}
				out = append(out, JField{N: n, T: typ, Tag: tag, Req: req})
			}
		}
	}
	ast.Inspect(h.Decl.Body, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.DeclStmt:
			gd, ok := t.Decl.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, sp := range gd.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, nm := range vs.Names {
					if nm.Name != name || vs.Type == nil {
						continue
					}
					if st, ok := vs.Type.(*ast.StructType); ok {
						add(st)
					} else {
						named = exprText(vs.Type)
					}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range t.Lhs {
				if id, ok := lhs.(*ast.Ident); !ok || id.Name != name || i >= len(t.Rhs) {
					continue
				}
				if cl, ok := t.Rhs[i].(*ast.CompositeLit); ok {
					if st, ok := cl.Type.(*ast.StructType); ok {
						add(st)
					} else {
						named = exprText(cl.Type)
					}
				}
			}
		}
		return true
	})
	return out, named
}

func respKeys(text string) []string {
	var keys []string
	seen := map[string]bool{}
	i := strings.Index(text, "gin.H{")
	if i < 0 {
		i = strings.Index(text, "fiber.Map{")
	}
	if i < 0 {
		return nil
	}
	for _, mm := range jsonKeyRe.FindAllStringSubmatch(text[i:], 6) {
		if !seen[mm[1]] {
			seen[mm[1]] = true
			keys = append(keys, mm[1])
		}
	}
	return keys
}

// tablesOf reads literal table names and model types in the handler, plus one
// hop into the in-repo functions it calls.
func tablesOf(m *Module, h *Func) []string {
	seen := map[string]bool{}
	var out []string
	take := func(f *Func) {
		if f == nil || f.File == nil || f.Start == 0 || f.End == 0 {
			return
		}
		lines := f.File.Lines
		lo, hi := f.Start-1, f.End
		if lo < 0 {
			lo = 0
		}
		if hi > len(lines) {
			hi = len(lines)
		}
		src := strings.Join(lines[lo:hi], "\n")
		for _, mm := range tableRe.FindAllStringSubmatch(src, 8) {
			if !seen[mm[1]] {
				seen[mm[1]] = true
				out = append(out, mm[1])
			}
		}
		for _, mm := range modelRe.FindAllStringSubmatch(src, 8) {
			name := strings.TrimPrefix(mm[1], "model.")
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	take(h)
	for _, ref := range tableCalls(h) {
		take(ref)
	}
	sort.Strings(out)
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func tableCalls(h *Func) []*Func {
	var out []*Func
	for _, c := range h.Calls {
		if c.Target != nil {
			out = append(out, c.Target)
		}
	}
	return out
}

var gitCache = map[string][]string{}

func gitMeta(root, rel string) []string {
	if rel == "" {
		return nil
	}
	if v, ok := gitCache[rel]; ok {
		return v
	}
	cmd := exec.Command("git", "-C", root, "log", "-1", "--format=%an%x00%h%x00%ar", "--", rel)
	b, err := cmd.Output()
	if err != nil {
		gitCache[rel] = nil
		return nil
	}
	parts := strings.Split(strings.TrimSpace(string(b)), "\x00")
	if len(parts) != 3 {
		gitCache[rel] = nil
		return nil
	}
	gitCache[rel] = parts
	return parts
}

// curlFor writes an example request. Values are placeholders shaped by the
// proven fields, nothing more.
func curlFor(r *Route, d *JDoc) string {
	path := r.Path
	for _, p := range d.Path {
		path = strings.ReplaceAll(path, ":"+p.N, "<"+p.N+">")
		path = strings.ReplaceAll(path, "{"+p.N+"}", "<"+p.N+">")
	}
	if len(d.Query) > 0 {
		qs := make([]string, 0, len(d.Query))
		for _, q := range d.Query {
			qs = append(qs, q.N+"=<"+q.N+">")
		}
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + strings.Join(qs, "&")
	}
	var b strings.Builder
	b.WriteString("curl -X " + r.Method + " http://localhost:8080" + path)
	if d.Auth {
		b.WriteString(" \\\n  -H \"Authorization: Bearer $TOKEN\"")
	}
	if len(d.Body) > 0 {
		b.WriteString(" \\\n  -H \"Content-Type: application/json\" \\\n  -d '{")
		for i, f := range d.Body {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("\"" + f.N + "\": " + placeholder(f.T))
		}
		b.WriteString("}'")
	}
	return b.String()
}

func placeholder(t string) string {
	switch {
	case strings.HasPrefix(t, "int"), strings.HasPrefix(t, "uint"), strings.HasPrefix(t, "float"):
		return "0"
	case t == "bool":
		return "true"
	case strings.HasPrefix(t, "[]"):
		return "[]"
	case strings.HasPrefix(t, "map"):
		return "{}"
	}
	return `"string"`
}
