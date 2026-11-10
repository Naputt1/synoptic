package main

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// JErr is one error message written in the source: a response body, a
// constructed error, a sentinel used as an error, a log line or a panic.
type JErr struct {
	M  string    `json:"m"`            // message as written
	K  string    `json:"k,omitempty"`  // response, error, log, panic
	C  []string  `json:"c,omitempty"`  // statuses the site can answer with
	F  string    `json:"f"`            // file, with the module root stripped
	L  int       `json:"l"`            // line of the message itself
	Fn string    `json:"fn,omitempty"` // function key holding the site
	D  string    `json:"d,omitempty"`  // body key or parameter name
	X  bool      `json:"x,omitempty"`  // message is a format template
	R  []JErrRef `json:"r,omitempty"`  // routes and rpc methods that can reach it
	N  int       `json:"n,omitempty"`  // more refs than the capped list
}

// JErrRef points at one surface: an HTTP route stage, or an RPC method.
type JErrRef struct {
	K  string `json:"k,omitempty"` // empty for http, "rpc" for a service method
	Ri int    `json:"ri"`
	Si int    `json:"si,omitempty"`
	Mi int    `json:"mi,omitempty"`
}

const (
	maxErrEntries = 3000
	maxErrRefs    = 8
	maxErrRunes   = 200
	maxErrPerStep = 6
)

type errSentinel struct {
	msg  string
	tmpl bool
}

// errScan walks one function. It resolves string constants and local names so
// `gin.H{"error": ErrNotFound}` still reads as a message, but it never guesses
// about values that only exist at runtime.
type errScan struct {
	file      *FileCtx
	fn        *Func
	calls     map[string]Call
	consts    map[string]string
	sentinels map[string]errSentinel
	locals    map[string]string
	sites     map[string]JErr
}

// attachErrors extracts every error message, links each one to the routes and
// rpc methods that can reach it, and hangs the indexes on the matching flow
// steps. It runs after attachServices so gRPC flows are already baked.
func attachErrors(m *Module, routes []*Route, d *JData) {
	errs := extractErrors(m)
	if len(errs) == 0 {
		return
	}

	order := map[*Route]int{}
	for ri := range d.Routes {
		if r := d.Routes[ri].origin; r != nil {
			order[r] = ri
		}
	}

	refs := map[string]map[JErrRef]bool{}
	addWalk := func(fn *Func, ref JErrRef) {
		seen := map[*Func]bool{}
		var walk func(*Func)
		walk = func(f *Func) {
			if f == nil || f.Key == "" || seen[f] {
				return
			}
			seen[f] = true
			if refs[f.Key] == nil {
				refs[f.Key] = map[JErrRef]bool{}
			}
			refs[f.Key][ref] = true
			for _, c := range f.Calls {
				if c.Target != nil {
					walk(c.Target)
				}
			}
		}
		walk(fn)
	}

	for _, r := range routes {
		ri, ok := order[r]
		if !ok {
			continue
		}
		ref := JErrRef{Ri: ri}
		for _, ch := range r.Chain {
			addWalk(ch.Fn, ref)
		}
		addWalk(r.Entry, ref)
		if r.Stream != nil {
			for _, e := range r.Stream.Evs {
				addWalk(e.fn, ref)
			}
		}
	}
	for si := range d.Services {
		for mi := range d.Services[si].Methods {
			md := &d.Services[si].Methods[mi]
			fn := md.fn
			if fn == nil && md.Fk != "" {
				fn = m.Funcs[md.Fk]
			}
			addWalk(fn, JErrRef{K: "rpc", Si: si, Mi: mi})
		}
	}

	byFn := map[string][]int{}
	for i := range errs {
		set := refs[errs[i].Fn]
		list := make([]JErrRef, 0, len(set))
		for ref := range set {
			list = append(list, ref)
		}
		sort.Slice(list, func(a, b int) bool {
			x, y := list[a], list[b]
			if x.K != y.K {
				return x.K < y.K
			}
			if x.Ri != y.Ri {
				return x.Ri < y.Ri
			}
			if x.Si != y.Si {
				return x.Si < y.Si
			}
			return x.Mi < y.Mi
		})
		if len(list) > maxErrRefs {
			errs[i].N = len(list)
			list = list[:maxErrRefs]
		}
		errs[i].R = list
		byFn[errs[i].Fn] = append(byFn[errs[i].Fn], i)
	}

	for key, jf := range d.Flows {
		annotateStepErrors(jf.Steps, byFn[key], errs)
		d.Flows[key] = jf
	}
	propagateStepErrors(d, responseFns(m))
	d.Errs = errs
	d.Stats.Errors = len(errs)
}

// responseFns marks the helpers that write a response, so a call to one can
// contribute its messages without pulling in ordinary error constructors.
func responseFns(m *Module) map[string]bool {
	out := map[string]bool{}
	for key, fn := range m.Funcs {
		out[key] = writesResponse(Call{Target: fn})
	}
	return out
}

// propagateStepErrors lifts messages from response helpers onto the step that
// calls them, so a caller's status circle previews what the helper can answer.
// Helpers may call other helpers, so a few passes settle the nesting.
func propagateStepErrors(d *JData, responseFn map[string]bool) {
	for pass := 0; pass < 4; pass++ {
		changed := false
		for key, jf := range d.Flows {
			if mergeHelperErrors(jf.Steps, d.Flows, responseFn) {
				changed = true
			}
			d.Flows[key] = jf
		}
		if !changed {
			return
		}
	}
}

func mergeHelperErrors(steps []Step, flows map[string]JFunc, responseFn map[string]bool) bool {
	changed := false
	for i := range steps {
		st := &steps[i]
		if len(stepCodes(*st)) > 0 {
			keys := make([]string, 0, len(st.Writes)+1)
			keys = append(keys, st.Writes...)
			// an exit's key can be a call that fails, or a helper that writes the
			// response itself; only the latter contributes messages.
			if st.Key != "" && responseFn[st.Key] {
				keys = append(keys, st.Key)
			}
			for _, key := range keys {
				jf, ok := flows[key]
				if !ok {
					continue
				}
				var idxs []int
				seen := map[int]bool{}
				flowErrIndexes(jf.Steps, &idxs, seen)
				for _, ei := range idxs {
					if containsInt(st.Errs, ei) {
						continue
					}
					if len(st.Errs) >= maxErrPerStep*2 {
						break
					}
					st.Errs = append(st.Errs, ei)
					changed = true
				}
			}
		}
		if mergeHelperErrors(st.Body, flows, responseFn) {
			changed = true
		}
		if mergeHelperErrors(st.Else, flows, responseFn) {
			changed = true
		}
	}
	return changed
}

func flowErrIndexes(steps []Step, out *[]int, seen map[int]bool) {
	for _, st := range steps {
		for _, ei := range st.Errs {
			if !seen[ei] {
				seen[ei] = true
				*out = append(*out, ei)
			}
		}
		flowErrIndexes(st.Body, out, seen)
		flowErrIndexes(st.Else, out, seen)
	}
}

func containsInt(list []int, want int) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// annotateStepErrors marks the steps whose source span holds a message, so a
// status circle can preview what can be written on that exit.
func annotateStepErrors(steps []Step, idxs []int, errs []JErr) {
	for i := range steps {
		st := &steps[i]
		if st.Line > 0 && st.SpanEnd >= st.Line && len(idxs) > 0 {
			em := make([]int, 0, 2)
			for _, ei := range idxs {
				if l := errs[ei].L; l >= st.Line && l <= st.SpanEnd {
					em = append(em, ei)
					if len(em) >= maxErrPerStep {
						break
					}
				}
			}
			if len(em) > 0 {
				st.Errs = em
			}
		}
		annotateStepErrors(st.Body, idxs, errs)
		annotateStepErrors(st.Else, idxs, errs)
	}
}

func extractErrors(m *Module) []JErr {
	consts, sentinels := errorTables(m)
	keys := make([]string, 0, len(m.Funcs))
	for key := range m.Funcs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out []JErr
	for _, key := range keys {
		fn := m.Funcs[key]
		if fn == nil || fn.Decl == nil || fn.Decl.Body == nil || fn.File == nil {
			continue
		}
		s := &errScan{
			file:      fn.File,
			fn:        fn,
			calls:     map[string]Call{},
			consts:    consts,
			sentinels: sentinels,
			locals:    map[string]string{},
			sites:     map[string]JErr{},
		}
		for _, c := range fn.Calls {
			s.calls[c.Expr] = c
		}
		s.collectLocals()
		ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				s.callSite(x)
			case *ast.Ident:
				if sent, ok := s.sentinels[s.file.PkgPath+"|"+x.Name]; ok {
					s.addSite("error", sent.msg, sent.tmpl, "", nil, x.Pos())
				}
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				if !ok {
					return true
				}
				pkg, ok := s.file.Imports[id.Name]
				if !ok {
					return true
				}
				if sent, ok := s.sentinels[pkg+"|"+x.Sel.Name]; ok {
					s.addSite("error", sent.msg, sent.tmpl, "", nil, x.Pos())
				}
			}
			return true
		})
		for _, site := range s.sites {
			out = append(out, site)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].F != out[j].F {
			return out[i].F < out[j].F
		}
		if out[i].L != out[j].L {
			return out[i].L < out[j].L
		}
		if out[i].M != out[j].M {
			return out[i].M < out[j].M
		}
		return out[i].K < out[j].K
	})
	if len(out) > maxErrEntries {
		out = out[:maxErrEntries]
	}
	return out
}

// errorTables resolves package-level string constants and error sentinels.
// Constants may alias each other, so a few passes settle the graph.
func errorTables(m *Module) (map[string]string, map[string]errSentinel) {
	type decl struct {
		key  string
		expr ast.Expr
		file *FileCtx
	}
	consts := map[string]string{}
	sentinels := map[string]errSentinel{}
	var vals []decl
	for _, fc := range m.Files {
		for _, d := range fc.Ast.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, sp := range gd.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					vals = append(vals, decl{key: fc.PkgPath + "|" + name.Name, expr: vs.Values[i], file: fc})
				}
			}
		}
	}

	for pass := 0; pass < 4; pass++ {
		changed := false
		for _, v := range vals {
			if _, ok := consts[v.key]; ok {
				continue
			}
			if s, ok := literalString(v.expr, consts, v.file); ok {
				consts[v.key] = s
				changed = true
				continue
			}
			if _, ok := sentinels[v.key]; ok {
				continue
			}
			ce, ok := v.expr.(*ast.CallExpr)
			if !ok || len(ce.Args) == 0 || !isErrorCtor(exprText(ce.Fun)) {
				continue
			}
			msg, ok := literalString(ce.Args[0], consts, v.file)
			if !ok {
				continue
			}
			msg = cleanMessage(msg)
			if msg == "" {
				continue
			}
			name := exprText(ce.Fun)
			sentinels[v.key] = errSentinel{msg: msg, tmpl: name != "errors.New" && hasVerb(msg)}
			changed = true
		}
		if !changed {
			break
		}
	}
	return consts, sentinels
}

func (s *errScan) collectLocals() {
	seen := map[string][]string{}
	take := func(name string, e ast.Expr) {
		if name == "" || name == "_" {
			return
		}
		if v, ok := literalString(e, s.consts, s.file); ok {
			seen[name] = append(seen[name], v)
		}
	}
	ast.Inspect(s.fn.Decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl:
			if x.Tok != token.CONST && x.Tok != token.VAR {
				return true
			}
			for _, sp := range x.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						take(name.Name, vs.Values[i])
					}
				}
			}
		case *ast.AssignStmt:
			if len(x.Lhs) != len(x.Rhs) {
				return true
			}
			for i, l := range x.Lhs {
				id, ok := l.(*ast.Ident)
				if !ok {
					continue
				}
				take(id.Name, x.Rhs[i])
			}
		}
		return true
	})
	for name, list := range seen {
		if len(list) == 0 {
			continue
		}
		unique := true
		for _, v := range list[1:] {
			if v != list[0] {
				unique = false
				break
			}
		}
		if unique {
			s.locals[name] = list[0]
		}
	}
}

func (s *errScan) literal(e ast.Expr) (string, bool) {
	if v, ok := literalString(e, s.consts, s.file); ok {
		return v, true
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return s.literal(x.X)
	case *ast.Ident:
		v, ok := s.locals[x.Name]
		return v, ok
	}
	return "", false
}

// stringArg reads a message argument. A Sprintf call contributes its format
// string, so the preview and the paste trace both know the fixed part.
func (s *errScan) stringArg(e ast.Expr) (string, bool) {
	if call, ok := e.(*ast.CallExpr); ok {
		name := exprText(call.Fun)
		if name == "fmt.Sprintf" || strings.HasSuffix(name, ".Sprintf") {
			if len(call.Args) == 0 {
				return "", false
			}
			return s.stringArg(call.Args[0])
		}
		if name == "string" && len(call.Args) == 1 {
			return s.stringArg(call.Args[0])
		}
	}
	if v, ok := s.literal(e); ok {
		return v, true
	}
	// Values attached at runtime still leave the words around them searchable:
	// `"lookup failed: " + err.Error()` becomes `lookup failed: %v`.
	if v, ok := s.pattern(e); ok && patternHasText(v) {
		return v, true
	}
	return "", false
}

// pattern renders the fixed parts of a runtime-built string and replaces each
// unknown part with %v. It never invents words, so a bare err.Error() is not
// indexed as a message.
func (s *errScan) pattern(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			if v, err := strconv.Unquote(x.Value); err == nil {
				return v, true
			}
		}
	case *ast.ParenExpr:
		return s.pattern(x.X)
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			a, aok := s.pattern(x.X)
			b, bok := s.pattern(x.Y)
			if !aok && !bok {
				return "", false
			}
			if !aok {
				a = "%v"
			}
			if !bok {
				b = "%v"
			}
			return a + b, true
		}
	case *ast.CallExpr:
		name := exprText(x.Fun)
		if (name == "fmt.Sprintf" || strings.HasSuffix(name, ".Sprintf")) && len(x.Args) > 0 {
			return s.pattern(x.Args[0])
		}
	case *ast.Ident:
		if v, ok := s.consts[s.file.PkgPath+"|"+x.Name]; ok {
			return v, true
		}
		if v, ok := s.locals[x.Name]; ok {
			return v, true
		}
	case *ast.SelectorExpr:
		if v, ok := literalString(x, s.consts, s.file); ok {
			return v, true
		}
	}
	return "%v", true
}

// patternHasText keeps patterns that contain real words. A message that is
// only a placeholder (`%v`) cannot identify a source site.
func patternHasText(s string) bool {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		j := i + 1
		if j < len(s) && s[j] == '%' {
			b.WriteByte('%')
			i++
			continue
		}
		for j < len(s) && strings.ContainsRune("+-# 0.123456789*[]", rune(s[j])) {
			j++
		}
		if j < len(s) && ((s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z')) {
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return len([]rune(strings.TrimSpace(b.String()))) >= 3
}

func (s *errScan) addSite(kind, msg string, tmpl bool, key string, codes map[string]bool, at token.Pos) {
	msg = cleanMessage(msg)
	if msg == "" || len([]rune(msg)) < 3 {
		return
	}
	j := JErr{M: msg, K: kind, F: s.file.Rel, L: line(at), Fn: s.fn.Key, D: key, X: tmpl}
	j.C = sortedErrorCodes(codes)
	k := kind + "|" + strconv.Itoa(j.L) + "|" + msg + "|" + strings.Join(j.C, ",") + "|" + key
	if _, ok := s.sites[k]; !ok {
		s.sites[k] = j
	}
}

func (s *errScan) callSite(x *ast.CallExpr) {
	c, ok := describeCall(x)
	if !ok {
		return
	}
	if known, found := s.calls[c.Expr]; found {
		c = known
	}
	name := exprText(x.Fun)

	switch {
	case name == "panic":
		s.literalArg(x, 0, "panic", "", nil)
	case isErrorCtor(name):
		s.literalArg(x, 0, "error", "", nil)
	}
	if isErrorLogCall(c) {
		// A leveled logger already says this is an error. std log.Print* does
		// not, so only its messages that read like one are indexed.
		if !isStdLogPrint(c) {
			s.literalArg(x, 0, "log", "", nil)
		} else if len(x.Args) > 0 {
			if v, ok := s.stringArg(x.Args[0]); ok && errorWords(v) {
				s.addSite("log", v, hasVerb(v), "", nil, x.Args[0].Pos())
			}
		}
	}
	if isResponseCall(c) || isResponsePrimitive(c) || writesResponse(c) {
		codes := newStatusWalk().callStatuses(x, s.calls, s.fn, nil)
		s.responseArgs(x, c, codes)
	}
}

func (s *errScan) literalArg(x *ast.CallExpr, i int, kind, key string, codes map[string]bool) {
	if i >= len(x.Args) {
		return
	}
	v, ok := s.stringArg(x.Args[i])
	if !ok {
		return
	}
	s.addSite(kind, v, hasVerb(v), key, codes, x.Args[i].Pos())
}

func (s *errScan) responseArgs(x *ast.CallExpr, c Call, codes map[string]bool) {
	errish := anyErrorCode(codes)
	direct := isResponseCall(c) || isResponsePrimitive(c)
	for i, a := range x.Args {
		if lit, ok := compositeOf(a); ok {
			s.composite(lit, codes, errish)
			continue
		}
		param := errorArgName(c, i)
		// A framework call has no resolved parameter names, so only the known
		// message slot counts. A project helper is read through its signature.
		if direct {
			if !directMessageArg(c.Sel, i) {
				continue
			}
		} else if !isMessageKey(param) {
			continue
		}
		v, ok := s.stringArg(a)
		if !ok {
			continue
		}
		key := param
		if key == "" {
			key = "message"
		}
		s.addSite("response", v, hasVerb(v), key, codes, a.Pos())
	}
}

// directMessageArg is the message slot of a framework response method:
// c.JSON(code, body), c.String(code, body), http.Error(w, msg, code).
func directMessageArg(sel string, i int) bool {
	switch sel {
	case "Error", "JSON", "XML", "AbortWithStatusJSON", "String", "SendString":
		return i == 1
	}
	return false
}

func (s *errScan) composite(lit *ast.CompositeLit, codes map[string]bool, errish bool) {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key := ""
		switch k := kv.Key.(type) {
		case *ast.Ident:
			key = k.Name
		case *ast.SelectorExpr:
			key = k.Sel.Name
		case *ast.BasicLit:
			if k.Kind == token.STRING {
				key, _ = strconv.Unquote(k.Value)
			}
		}
		if !isErrorKey(key) && !(errish && isMessageKey(key)) {
			continue
		}
		v, ok := s.stringArg(kv.Value)
		if !ok {
			continue
		}
		s.addSite("response", v, hasVerb(v), key, codes, kv.Pos())
	}
}

func compositeOf(e ast.Expr) (*ast.CompositeLit, bool) {
	switch x := e.(type) {
	case *ast.CompositeLit:
		return x, true
	case *ast.UnaryExpr:
		if lit, ok := x.X.(*ast.CompositeLit); ok {
			return lit, true
		}
	case *ast.ParenExpr:
		return compositeOf(x.X)
	}
	return nil, false
}

func isErrorCtor(name string) bool {
	return name == "errors.New" || name == "fmt.Errorf" || strings.HasSuffix(name, ".Errorf")
}

func isErrorLogCall(c Call) bool {
	switch c.Sel {
	case "Error", "Errorf", "Fatal", "Fatalf", "Panic", "Panicf", "Warn", "Warnf", "Warning", "Warningf":
	case "Print", "Printf", "Println", "Panicln":
		// std log has no level; its Print* calls still carry the message a
		// pasted log line will contain
	default:
		return false
	}
	low := strings.ToLower(c.Expr)
	return strings.Contains(low, "log") || strings.Contains(low, "zap")
}

func isStdLogPrint(c Call) bool {
	switch c.Sel {
	case "Print", "Printf", "Println", "Panicln":
		return true
	}
	return false
}

func errorWords(s string) bool {
	low := strings.ToLower(s)
	for _, w := range []string{
		"error", "fail", "fatal", "panic", "warn", "invalid", "denied", "deny",
		"unauthor", "forbidden", "missing", "unable", "refuse", "timeout",
		"could not", "cannot", "no such", "not found", "bad ", "expired",
		"mismatch", "unexpected", "broken", "reject",
	} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

func errorArgName(c Call, i int) string {
	fn := c.Target
	if fn == nil || fn.Decl == nil || fn.Decl.Type.Params == nil {
		return ""
	}
	idx := 0
	for _, fld := range fn.Decl.Type.Params.List {
		names := fld.Names
		if len(names) == 0 {
			if idx == i {
				return ""
			}
			idx++
			continue
		}
		for _, nm := range names {
			if idx == i {
				return nm.Name
			}
			idx++
		}
	}
	return ""
}

func isErrorKey(key string) bool {
	switch strings.ToLower(key) {
	case "error", "err", "errors", "reason", "detail", "details":
		return true
	}
	return false
}

func isMessageKey(key string) bool {
	switch strings.ToLower(key) {
	case "message", "msg", "description", "error_description", "error_message", "body":
		return true
	}
	return false
}

func anyErrorCode(codes map[string]bool) bool {
	for code := range codes {
		if len(code) == 3 && (code[0] == '4' || code[0] == '5') {
			return true
		}
	}
	return false
}

func sortedErrorCodes(codes map[string]bool) []string {
	list := make([]string, 0, len(codes))
	for code := range codes {
		list = append(list, code)
	}
	sort.Strings(list)
	return list
}

// hasVerb reports a printf-style format, so the paste matcher can wildcard it.
func hasVerb(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		j := i + 1
		if j < len(s) && s[j] == '%' {
			i++
			continue
		}
		for j < len(s) && strings.ContainsRune("+-# 0.123456789*[]", rune(s[j])) {
			j++
		}
		if j < len(s) && ((s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z')) {
			return true
		}
	}
	return false
}

func cleanMessage(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > maxErrRunes {
		s = string([]rune(s)[:maxErrRunes])
	}
	return s
}

// literalString resolves a string literal, a concatenation of literals, or a
// package-level string constant. Anything else is runtime and returns false.
func literalString(e ast.Expr, consts map[string]string, fc *FileCtx) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			if v, err := strconv.Unquote(x.Value); err == nil {
				return v, true
			}
		}
	case *ast.ParenExpr:
		return literalString(x.X, consts, fc)
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			a, aok := literalString(x.X, consts, fc)
			b, bok := literalString(x.Y, consts, fc)
			if aok && bok {
				return a + b, true
			}
		}
	case *ast.Ident:
		if v, ok := consts[fc.PkgPath+"|"+x.Name]; ok {
			return v, true
		}
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		pkg, ok := fc.Imports[id.Name]
		if !ok {
			return "", false
		}
		if v, ok := consts[pkg+"|"+x.Sel.Name]; ok {
			return v, true
		}
	}
	return "", false
}
