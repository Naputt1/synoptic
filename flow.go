package main

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"strconv"
	"strings"
)

// Step is one box in a function's code flow: a call that matters, a branch that
// changes where control goes, or the way out. Assignments and defer are not steps.
type Step struct {
	Kind    string `json:"k"` // call, exit, if, loop, switch, ret, note
	Text    string `json:"t"`
	Sub     string `json:"s,omitempty"`
	SubLine int    `json:"sl,omitempty"`
	Code    string `json:"cd,omitempty"`
	Line    int    `json:"l,omitempty"`
	Tag     string `json:"g,omitempty"`
	Key     string `json:"fk,omitempty"`
	Ik      string `json:"ik,omitempty"`
	Sk      string `json:"sk,omitempty"`
	File    string `json:"f,omitempty"`
	Count   int    `json:"c,omitempty"`
	Body    []Step `json:"b,omitempty"`
	Else    []Step `json:"e,omitempty"`
}

func buildFlow(f *Func) []Step {
	body := flowBody(f)
	if body == nil {
		return nil
	}
	return flowSteps(f, body)
}

// flowBody is the statement list that actually runs: middleware factories hand
// back their handler, so that closure's body is the flow.
func flowBody(f *Func) []ast.Stmt {
	if f == nil || f.Decl == nil || f.Decl.Body == nil {
		return nil
	}
	body := f.Decl.Body.List
	if len(body) == 1 {
		if ret, ok := body[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			if lit, isLit := ret.Results[0].(*ast.FuncLit); isLit {
				return lit.Body.List
			}
		}
	}
	return body
}

// handoff returns the line of a top-level c.Next(), when the function is a
// filter that hands the request on. A return after it ends this filter's own
// post-processing; the downstream chain has already run.
func handoff(f *Func) int {
	for _, s := range flowBody(f) {
		es, ok := s.(*ast.ExprStmt)
		if !ok {
			continue
		}
		ce := firstCall(es.X)
		if ce == nil {
			continue
		}
		c, ok := describeCall(ce)
		if !ok || (c.Base != "c" && c.Base != "ctx") || c.Sel != "Next" || c.Expr != c.Base+"."+c.Sel {
			continue
		}
		return line(ce.Pos())
	}
	return 0
}

func afterHandoff(f *Func, pos token.Pos) bool {
	h := handoff(f)
	return h > 0 && line(pos) > h
}

func flowSteps(f *Func, list []ast.Stmt) []Step {
	calls := map[string]Call{}
	for _, c := range f.Calls {
		calls[c.Expr] = c
	}

	var out []Step
	add := func(st Step) {
		if st.Text == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Kind == st.Kind && out[n-1].Text == st.Text {
			out[n-1].Count++
			return
		}
		if st.Count == 0 {
			st.Count = 1
		}
		out = append(out, st)
	}

	for i := 0; i < len(list); i++ {
		s := list[i]
		// c.Next(); return — the chain handoff is the exit, the bare return is
		// the framework's job after the downstream filters come back.
		if responseThenReturn(list, i) || nextThenReturn(list, i) {
			continue
		}
		switch x := s.(type) {
		case *ast.IfStmt:
			if ex, ok := exitOf(f, x, calls); ok {
				if afterHandoff(f, x.Pos()) {
					// past c.Next() the response is already written; the return
					// only stops this filter's own post-processing.
					add(Step{Kind: "if", Text: exprText(x.Cond), Sub: "return", Line: line(x.Pos())})
				} else {
					add(ex)
				}
				continue
			}
			st := Step{Kind: "if", Text: exprText(x.Cond), Line: line(x.Pos())}
			if x.Init != nil {
				if init, isAssign := x.Init.(*ast.AssignStmt); isAssign && len(init.Rhs) == 1 {
					// a fork reads as one card: the initializer call on top, the
					// condition below, and the card opens the call's function
					if cs := callStep(f, init.Rhs[0], calls); cs.Kind == "call" {
						st.Text = cs.Text
						st.Tag = cs.Tag
						st.Key = cs.Key
						st.File = cs.File
						st.Line = cs.Line
						st.Sub = "if " + exprText(x.Cond)
						st.SubLine = line(x.Pos())
					}
				} else {
					st.Sub = exprText(stmtExpr(x.Init))
				}
			}
			st.Body = flowSteps(f, x.Body.List)
			if x.Else != nil {
				switch e := x.Else.(type) {
				case *ast.BlockStmt:
					st.Else = flowSteps(f, e.List)
				default:
					st.Else = flowSteps(f, []ast.Stmt{e})
				}
			}
			add(st)
		case *ast.ForStmt:
			st := Step{Kind: "loop", Text: "for " + strings.TrimSpace(exprText(x.Cond)), Line: line(x.Pos())}
			if x.Cond == nil {
				st.Text = "for"
			}
			st.Body = flowSteps(f, x.Body.List)
			add(st)
		case *ast.RangeStmt:
			st := Step{Kind: "loop", Text: "for " + exprTexts(rangeKey(x)) + " := range " + exprText(x.X), Line: line(x.Pos())}
			st.Body = flowSteps(f, x.Body.List)
			add(st)
		case *ast.SwitchStmt:
			st := Step{Kind: "switch", Text: "switch " + exprText(x.Tag), Line: line(x.Pos())}
			if x.Body != nil {
				for _, c := range x.Body.List {
					if cc, ok := c.(*ast.CaseClause); ok {
						label := "default"
						if cc.List != nil {
							var parts []string
							for _, e := range cc.List {
								parts = append(parts, exprText(e))
							}
							label = strings.Join(parts, ", ")
						}
						st.Body = append(st.Body, Step{Kind: "note", Text: label, Body: flowSteps(f, cc.Body)})
					}
				}
			}
			add(st)
		case *ast.TypeSwitchStmt:
			add(Step{Kind: "switch", Text: "switch " + exprText(stmtExpr(x.Assign)), Line: line(x.Pos())})
		case *ast.BlockStmt:
			for _, inner := range flowSteps(f, x.List) {
				add(inner)
			}
		case *ast.LabeledStmt:
			add(Step{Kind: "note", Text: x.Label.Name + ":", Line: line(x.Pos())})
		case *ast.ReturnStmt:
			add(Step{Kind: "ret", Text: "return " + exprTexts(x.Results), Line: line(x.Pos())})
		case *ast.DeferStmt, *ast.GoStmt, *ast.EmptyStmt, *ast.BranchStmt:
			// defer, go, break: badges, not boxes
		case *ast.ExprStmt:
			add(callStep(f, x.X, calls))
		case *ast.AssignStmt:
			// x, err := call(); if err != nil { … } — one card: the call that
			// can fail on top, the guard below, and the card opens the call.
			if i+1 < len(list) && len(x.Rhs) == 1 {
				if next, isIf := list[i+1].(*ast.IfStmt); isIf {
					if ex, isExit := exitOf(f, next, calls); isExit && len(errorNames(x.Lhs)) > 0 && !afterHandoff(f, next.Pos()) {
						if cs := callStep(f, x.Rhs[0], calls); cs.Kind == "call" {
							ex.Text = cs.Text
							ex.Tag = cs.Tag
							ex.Key = cs.Key
							ex.File = cs.File
							ex.Line = cs.Line
							ex.Sub = "if " + exprText(next.Cond)
							ex.SubLine = line(next.Pos())
							add(ex)
							i++
							continue
						}
					}
					// a fork with more than one way out is still one card: keep
					// the call and the condition together, then show its body
					if refersTo(next.Cond, errorNames(x.Lhs)) {
						if cs := callStep(f, x.Rhs[0], calls); cs.Kind == "call" {
							st := Step{Kind: "if", Text: cs.Text, Sub: "if " + exprText(next.Cond), Tag: cs.Tag, Key: cs.Key, File: cs.File, Line: cs.Line, SubLine: line(next.Pos())}
							st.Body = flowSteps(f, next.Body.List)
							if next.Else != nil {
								switch e := next.Else.(type) {
								case *ast.BlockStmt:
									st.Else = flowSteps(f, e.List)
								default:
									st.Else = flowSteps(f, []ast.Stmt{e})
								}
							}
							add(st)
							i++
							continue
						}
					}
				}
			}
			for _, r := range x.Rhs {
				add(callStep(f, r, calls))
			}
		case *ast.DeclStmt:
			// var blocks read as declarations, not flow
		default:
			_ = x
		}
	}
	return out
}

// responseThenReturn reports whether list[i] is the bare return that closes a
// response call on the line before it. The call step already carries the status,
// so keeping the return would draw the same exit twice.
func responseThenReturn(list []ast.Stmt, i int) bool {
	if i == 0 {
		return false
	}
	ret, ok := list[i].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 0 {
		return false
	}
	es, ok := list[i-1].(*ast.ExprStmt)
	if !ok {
		return false
	}
	ce := firstCall(es.X)
	if ce == nil {
		return false
	}
	c, ok := describeCall(ce)
	return ok && isResponseCall(c)
}

// nextThenReturn reports whether list[i] is the bare return after c.Next().
// The chain handoff is already the last thing the filter does on that path.
func nextThenReturn(list []ast.Stmt, i int) bool {
	if i == 0 {
		return false
	}
	ret, ok := list[i].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 0 {
		return false
	}
	es, ok := list[i-1].(*ast.ExprStmt)
	if !ok {
		return false
	}
	ce := firstCall(es.X)
	if ce == nil {
		return false
	}
	c, ok := describeCall(ce)
	return ok && (c.Base == "c" || c.Base == "ctx") && c.Sel == "Next"
}

// callStep turns a statement's outermost call into a step, or into nothing when
// it is noise: logging, error formatting, decoding scaffolding.
func callStep(f *Func, e ast.Expr, calls map[string]Call) Step {
	ce := firstCall(e)
	if ce == nil {
		return Step{}
	}
	c, ok := describeCall(ce)
	if !ok {
		return Step{}
	}
	resolved, known := calls[c.Expr]
	if known {
		c = resolved
	}

	if isResponseCall(c) {
		return Step{Kind: "ret", Text: exprText(ce), Code: statusIn(ce), Line: line(ce.Pos())}
	}
	if (c.Base == "c" || c.Base == "ctx") && c.Sel == "Next" {
		return Step{Kind: "call", Text: exprText(ce), Sub: "continue chain", File: f.File.Rel, Line: line(ce.Pos())}
	}
	if isGinHeaderSet(ce, c) || isResponseHeaderWrite(c, f) {
		return Step{Kind: "call", Text: exprText(ce), Sub: "response header", File: f.File.Rel, Line: line(ce.Pos())}
	}
	if (c.Base == "c" || c.Base == "ctx") && (c.Sel == "Set" || c.Sel == "SetCookie") {
		return Step{Kind: "call", Text: exprText(ce), Sub: "context write", File: f.File.Rel, Line: line(ce.Pos())}
	}
	if !known && isNoise(c) {
		return Step{}
	}
	if !known && c.Target == nil && c.PkgPath == "" && tagOf(c) == "" {
		return Step{}
	}

	st := Step{Kind: "call", Text: exprText(ce), Tag: tagOf(c), Line: line(ce.Pos())}
	if c.Target != nil {
		st.Sub = altPkg(c.Target.PkgPath)
		st.File = f.File.Rel
		if c.Target.Key != "" {
			st.Key = c.Target.Key
		}
	} else if c.PkgPath != "" {
		st.Sub = altPkg(c.PkgPath)
	}
	if st.Tag == "" && st.Key == "" {
		// a library call with nothing to open: only worth a box if it is a service
		return Step{}
	}
	return st
}

func firstCall(e ast.Expr) *ast.CallExpr {
	switch x := e.(type) {
	case *ast.CallExpr:
		return x
	case *ast.ParenExpr:
		return firstCall(x.X)
	case *ast.UnaryExpr:
		return firstCall(x.X)
	case *ast.BinaryExpr:
		if c := firstCall(x.X); c != nil {
			return c
		}
		return firstCall(x.Y)
	case *ast.SelectorExpr:
		return firstCall(x.X)
	case *ast.IndexExpr:
		return firstCall(x.X)
	case *ast.TypeAssertExpr:
		return firstCall(x.X)
	case *ast.StarExpr:
		return firstCall(x.X)
	}
	return nil
}

func isResponseCall(c Call) bool {
	// c.Writer.Status() and c.Errors.String() share a method name with the
	// response API but only read from the context.
	if c.Base == "" || c.Expr != c.Base+"."+c.Sel {
		return false
	}
	switch c.Base {
	case "c", "ctx", "fiber", "gin":
		switch c.Sel {
		case "JSON", "XML", "String", "Send", "SendString", "SendStatus", "Status", "Render", "Redirect",
			"AbortWithStatusJSON", "AbortWithStatus":
			return true
		}
	}
	return false
}

func isNoise(c Call) bool {
	switch c.Base {
	case "log", "logger", "slog", "fmt", "errors", "time", "strings", "strconv", "os", "err":
		return true
	}
	switch c.Sel {
	case "Error", "String", "Unwrap", "Errorf", "Println", "Printf", "Sprintf", "Now":
		return true
	}
	return false
}

// exitOf reads an if whose body only leaves: that is an error path, not a fork.
func exitOf(f *Func, x *ast.IfStmt, calls map[string]Call) (Step, bool) {
	if x.Else != nil || len(x.Body.List) == 0 {
		return Step{}, false
	}
	for _, s := range x.Body.List {
		switch s.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.SelectStmt:
			return Step{}, false
		}
	}
	last := x.Body.List[len(x.Body.List)-1]
	ret, ok := last.(*ast.ReturnStmt)
	if !ok {
		return Step{}, false
	}
	// c.JSON(400, …); return — the status lives on the line before the return
	guardCode := ""
	// if !decode(c, &in) { return } — the helper writes the status itself
	ast.Inspect(x.Cond, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if code := callStatus(ce, calls, 0, nil); code != "" {
			guardCode = code
		}
		return true
	})
	for _, s := range x.Body.List {
		es, isExpr := s.(*ast.ExprStmt)
		if !isExpr {
			continue
		}
		ce := firstCall(es.X)
		if ce == nil {
			continue
		}
		if code := callStatus(ce, calls, 0, nil); code != "" {
			guardCode = code
		}
	}
	// the call that can fail is more useful than the condition that tests it
	st := Step{Kind: "exit", Text: exprText(x.Cond), Line: line(x.Pos())}
	if init, isAssign := x.Init.(*ast.AssignStmt); isAssign && len(init.Rhs) == 1 {
		if ce := firstCall(init.Rhs[0]); ce != nil {
			if code := callStatus(ce, calls, 0, nil); code != "" {
				guardCode = code
			}
			if c, okc := describeCall(ce); okc {
				if resolved, known := calls[c.Expr]; known {
					c = resolved
				}
				st.Text = exprText(ce)
				st.Tag = tagOf(c)
				if c.Target != nil && c.Target.Key != "" {
					st.Key = c.Target.Key
				}
				if c.Target != nil {
					st.File = f.File.Rel
					st.Line = line(ce.Pos())
				}
			}
		}
	}
	code := guardCode
	if len(ret.Results) > 0 {
		if ce := firstCall(ret.Results[0]); ce != nil {
			if c := callStatus(ce, calls, 0, nil); c != "" {
				code = c
			}
		}
	}
	st.Code = code
	if st.Text != exprText(x.Cond) {
		st.Sub = "if " + exprText(x.Cond)
		st.SubLine = line(x.Pos())
	}
	return st, true
}

// callStatus resolves a response helper such as utils.ReturnUnauth(c): the
// helper itself writes the status, so looking only at the caller's arguments
// would miss it.
func callStatus(ce *ast.CallExpr, calls map[string]Call, depth int, bind map[string]ast.Expr) string {
	set := callStatusSet(ce, calls, depth, bind)
	if len(set) != 1 {
		return ""
	}
	for code := range set {
		return code
	}
	return ""
}

// callStatusSet follows a helper and returns every status it can write. A
// helper with one branch (writeErrorMessage) gives one code; a dispatcher with
// many branches (writeError) gives several and the caller shows no single one.
func callStatusSet(ce *ast.CallExpr, calls map[string]Call, depth int, bind map[string]ast.Expr) map[string]bool {
	c, ok := describeCall(ce)
	if !ok {
		return nil
	}
	if resolved, known := calls[c.Expr]; known {
		c = resolved
	}
	if isResponseCall(c) {
		return statusSetIn(ce, bind)
	}
	if depth < 3 && c.Target != nil {
		return bodyStatusSet(c.Target, depth+1, bindArgs(c.Target, ce.Args, bind))
	}
	return nil
}

func bindArgs(fn *Func, args []ast.Expr, bind map[string]ast.Expr) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	if fn == nil || fn.Decl == nil || fn.Decl.Type.Params == nil {
		return out
	}
	i := 0
	for _, field := range fn.Decl.Type.Params.List {
		for _, name := range field.Names {
			if i >= len(args) {
				return out
			}
			e := args[i]
			if id, ok := e.(*ast.Ident); ok {
				if bound, found := bind[id.Name]; found {
					e = bound
				}
			}
			out[name.Name] = e
			i++
		}
	}
	return out
}

func bodyStatusSet(fn *Func, depth int, bind map[string]ast.Expr) map[string]bool {
	out := map[string]bool{}
	if fn == nil || fn.Decl == nil || fn.Decl.Body == nil {
		return out
	}
	calls := make(map[string]Call, len(fn.Calls))
	for _, c := range fn.Calls {
		calls[c.Expr] = c
	}
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for code := range callStatusSet(ce, calls, depth, bind) {
			out[code] = true
		}
		return true
	})
	return out
}

func statusIn(ce *ast.CallExpr) string {
	set := statusSetIn(ce, nil)
	if len(set) != 1 {
		return ""
	}
	for code := range set {
		return code
	}
	return ""
}

func statusSetIn(ce *ast.CallExpr, bind map[string]ast.Expr) map[string]bool {
	out := map[string]bool{}
	for _, a := range ce.Args {
		if code := statusExpr(a, bind); code != "" {
			out[code] = true
		}
	}
	return out
}

func statusExpr(e ast.Expr, bind map[string]ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		if n, err := strconv.Atoi(x.Value); err == nil && n >= 200 && n <= 599 {
			return strconv.Itoa(n)
		}
	case *ast.SelectorExpr:
		return statusNames[x.Sel.Name]
	case *ast.Ident:
		if n := statusNames[x.Name]; n != "" {
			return n
		}
		if bound, ok := bind[x.Name]; ok {
			return statusExpr(bound, bind)
		}
	case *ast.ParenExpr:
		return statusExpr(x.X, bind)
	}
	return ""
}

var statusNames = map[string]string{
	"StatusOK": "200", "StatusCreated": "201", "StatusAccepted": "202", "StatusNoContent": "204",
	"StatusMovedPermanently": "301", "StatusFound": "302", "StatusNotModified": "304",
	"StatusBadRequest": "400", "StatusUnauthorized": "401", "StatusPaymentRequired": "402",
	"StatusForbidden": "403", "StatusNotFound": "404", "StatusMethodNotAllowed": "405",
	"StatusConflict": "409", "StatusGone": "410", "StatusUnprocessableEntity": "422",
	"StatusTooManyRequests": "429", "StatusInternalServerError": "500", "StatusNotImplemented": "501",
	"StatusBadGateway": "502", "StatusServiceUnavailable": "503", "StatusGatewayTimeout": "504",
}

func stmtExpr(s ast.Stmt) ast.Expr {
	switch x := s.(type) {
	case *ast.AssignStmt:
		return nil
	case *ast.ExprStmt:
		return x.X
	}
	return nil
}

func assignNames(lhs []ast.Expr) []string {
	var names []string
	for _, e := range lhs {
		if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
			names = append(names, id.Name)
		}
	}
	return names
}

// errorNames only allows merging a call with a guard when the guard tests a
// result that is actually an error, not any argument that came from the call.
func errorNames(lhs []ast.Expr) []string {
	var names []string
	for _, e := range lhs {
		id, ok := e.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		if id.Name == "err" || strings.HasSuffix(id.Name, "Err") || strings.HasSuffix(id.Name, "Error") {
			names = append(names, id.Name)
		}
	}
	return names
}

func refersTo(e ast.Expr, names []string) bool {
	if e == nil || len(names) == 0 {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return !found
		}
		for _, name := range names {
			if id.Name == name {
				found = true
				break
			}
		}
		return !found
	})
	return found
}

func exprTexts(list []ast.Expr) string {
	var parts []string
	for _, e := range list {
		if s := exprText(e); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

func exprText(e ast.Expr) string {
	if e == nil {
		return ""
	}
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, e); err != nil {
		return ""
	}
	s := strings.Join(strings.Fields(strings.ReplaceAll(b.String(), "\n", " ")), " ")
	if len(s) > 88 {
		s = s[:87] + "…"
	}
	return s
}

func rangeKey(x *ast.RangeStmt) []ast.Expr {
	if x.Key == nil {
		return nil
	}
	if x.Value != nil {
		return []ast.Expr{x.Key, x.Value}
	}
	return []ast.Expr{x.Key}
}
