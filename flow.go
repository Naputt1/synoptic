package main

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// Step is one box in a function's code flow: a call that matters, a branch that
// changes where control goes, or the way out. Assignments and defer are not steps.
type Step struct {
	Kind    string   `json:"k"` // call, exit, if, loop, switch, ret, note
	Text    string   `json:"t"`
	Rk      string   `json:"rk,omitempty"` // nil, error, void on return steps
	Sub     string   `json:"s,omitempty"`
	SubLine int      `json:"sl,omitempty"`
	Code    string   `json:"cd,omitempty"`
	Codes   []string `json:"cds,omitempty"`
	Line    int      `json:"l,omitempty"`
	Tag     string   `json:"g,omitempty"`
	Key     string   `json:"fk,omitempty"`
	Ik      string   `json:"ik,omitempty"`
	Sk      string   `json:"sk,omitempty"`
	File    string   `json:"f,omitempty"`
	Count   int      `json:"c,omitempty"`
	Body    []Step   `json:"b,omitempty"`
	Else    []Step   `json:"e,omitempty"`
	// Errs indexes into JData.Errs for every message written on this step's
	// source span, so a status circle can preview them on hover.
	Errs    []int `json:"em,omitempty"`
	SpanEnd int   `json:"se,omitempty"` // last source line the step covers

	Rpc     string   `json:"rpc,omitempty"` // "si/mi" into the service list, when the call leaves over gRPC
	Writes  []string `json:"-"`             // response helpers called on this step's span
	Iface   string   `json:"-"`             // declared type of the field the call goes through
	Sel     string   `json:"-"`             // method name on that field
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
			if st.SpanEnd > out[n-1].SpanEnd {
				out[n-1].SpanEnd = st.SpanEnd
			}
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
						st.Iface = cs.Iface
						st.Sel = cs.Sel
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
		case *ast.SelectStmt:
			st := Step{Kind: "switch", Text: "select", File: f.File.Rel, Line: line(x.Pos())}
			if x.Body != nil {
				for _, cl := range x.Body.List {
					if cc, ok := cl.(*ast.CommClause); ok {
						label := "default"
						if cc.Comm != nil {
							label = commText(cc.Comm)
						}
						st.Body = append(st.Body, Step{Kind: "note", Text: label, Body: flowSteps(f, cc.Body)})
					}
				}
			}
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
			st := Step{Kind: "ret", Text: "return " + exprTexts(x.Results), Rk: returnKind(x.Results), Line: line(x.Pos()), SpanEnd: line(x.End())}
			if len(x.Results) > 0 {
				if ce := firstCall(x.Results[0]); ce != nil {
					setCodes(&st, newStatusWalk().callStatuses(ce, calls, f, nil))
					if c, ok := describeCall(ce); ok {
						if resolved, known := calls[c.Expr]; known {
							c = resolved
						}
						st.Iface = c.Iface
						st.Sel = c.Sel
						if strings.HasSuffix(c.Iface, "Client") {
							st.Tag = "rpc"
						}
						if c.Target != nil {
							st.File = f.File.Rel
							st.Key = c.Target.Key
						}
					}
				}
			} else if i > 0 {
				// writeStoreError(...); return — the writer above already gave
				// the status, even when the flow did not fold the two together.
				switch prev := list[i-1].(type) {
				case *ast.ExprStmt:
					if ce := firstCall(prev.X); ce != nil {
						setCodes(&st, newStatusWalk().callStatuses(ce, calls, f, nil))
					}
				case *ast.SwitchStmt, *ast.TypeSwitchStmt:
					// a switch whose cases write the answer, then one return
					setCodes(&st, newStatusWalk().stmtStatuses(prev, calls, f))
				}
			}
			add(st)
		case *ast.BranchStmt:
			if x.Tok == token.CONTINUE {
				add(Step{Kind: "continue", Text: "continue", Line: line(x.Pos())})
			}
		case *ast.DeferStmt, *ast.GoStmt, *ast.EmptyStmt:
			// defer, go, break: badges, not boxes
		case *ast.ExprStmt:
			if st, ok := streamStep(f, x.X, calls); ok {
				add(st)
			} else {
				add(callStep(f, x.X, calls))
			}
		case *ast.AssignStmt:
			// x, err := call(); if err != nil { … } — one card: the call that
			// can fail on top, the guard below, and the card opens the call.
			if i+1 < len(list) && len(x.Rhs) == 1 {
				if next, isIf := list[i+1].(*ast.IfStmt); isIf {
					if ex, isExit := exitOf(f, next, calls); isExit && len(guardNames(x.Lhs, next.Cond)) > 0 && !afterHandoff(f, next.Pos()) {
						if cs := callStep(f, x.Rhs[0], calls); cs.Kind == "call" {
							ex.Text = cs.Text
							ex.Tag = cs.Tag
							ex.Key = cs.Key
							ex.Iface = cs.Iface
							ex.Sel = cs.Sel
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
					if refersTo(next.Cond, guardNames(x.Lhs, next.Cond)) {
						if cs := callStep(f, x.Rhs[0], calls); cs.Kind == "call" {
							st := Step{Kind: "if", Text: cs.Text, Sub: "if " + exprText(next.Cond), Tag: cs.Tag, Key: cs.Key, Iface: cs.Iface, Sel: cs.Sel, File: cs.File, Line: cs.Line, SubLine: line(next.Pos())}
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

// callStep classifies one expression into a step, or into nothing when it is
// noise (logging, error formatting, decoding scaffolding), and records the full
// source span so a message inside a multi-line call can be attached to it.
func callStep(f *Func, e ast.Expr, calls map[string]Call) Step {
	st := callStepInner(f, e, calls)
	if ce := firstCall(e); ce != nil {
		st.SpanEnd = line(ce.End())
	}
	return st
}

func callStepInner(f *Func, e ast.Expr, calls map[string]Call) Step {
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

	// A project helper such as utils.SendJSONResponse stays a named card: it
	// carries a status and still opens its own flow.
	if writesResponse(c) {
		st := Step{Kind: "call", Text: exprText(ce), Line: line(ce.Pos())}
		setCodes(&st, newStatusWalk().callStatuses(ce, calls, f, nil))
		st.Sub = altPkg(c.Target.PkgPath)
		st.File = f.File.Rel
		st.Key = c.Target.Key
		return st
	}
	if isResponseCall(c) || isResponsePrimitive(c) {
		st := Step{Kind: "ret", Text: exprText(ce), Line: line(ce.Pos())}
		setCodes(&st, newStatusWalk().callStatuses(ce, calls, f, nil))
		return st
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
	if isStreamWrite(c) {
		return Step{Kind: "call", Text: exprText(ce), Sub: "stream write", File: f.File.Rel, Line: line(ce.Pos())}
	}
	if c.Sel == "Flush" || strings.HasSuffix(c.Expr, ".Flush()") {
		return Step{Kind: "call", Text: exprText(ce), Sub: "stream flush", File: f.File.Rel, Line: line(ce.Pos())}
	}
	tag := tagOf(c)
	if strings.HasSuffix(c.Iface, "Client") {
		tag = "rpc"
	}
	if !known && isNoise(c) {
		return Step{}
	}
	if !known && c.Target == nil && c.PkgPath == "" && tag == "" {
		return Step{}
	}

	st := Step{Kind: "call", Text: exprText(ce), Tag: tag, Line: line(ce.Pos()), Iface: c.Iface, Sel: c.Sel}
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

// streamStep keeps the callback body of c.Stream visible. Without this the
// SSE event loop and its writes disappear behind one unclassified call.
func streamStep(f *Func, e ast.Expr, calls map[string]Call) (Step, bool) {
	ce := firstCall(e)
	if ce == nil {
		return Step{}, false
	}
	c, ok := describeCall(ce)
	if !ok {
		return Step{}, false
	}
	if c.Sel != "Stream" && c.Sel != "SendStream" {
		return Step{}, false
	}
	st := Step{Kind: "loop", Text: exprText(ce), Sub: "stream loop", File: f.File.Rel, Line: line(ce.Pos())}
	for _, a := range ce.Args {
		switch x := a.(type) {
		case *ast.FuncLit:
			st.Body = flowSteps(f, x.Body.List)
		case *ast.ParenExpr:
			if lit, ok := x.X.(*ast.FuncLit); ok {
				st.Body = flowSteps(f, lit.Body.List)
			}
		}
		if st.Body != nil {
			break
		}
	}
	return st, true
}

func isStreamWrite(c Call) bool {
	sel := strings.ToLower(c.Sel)
	base := strings.ToLower(c.Base)
	if sel != "writestring" && sel != "fprintf" && sel != "fprint" && sel != "write" {
		return false
	}
	return base == "io" || base == "fmt" || strings.Contains(c.Expr, "Writer")
}

func commText(s ast.Stmt) string {
	switch x := s.(type) {
	case *ast.AssignStmt:
		return exprTexts(x.Lhs) + " := " + exprTexts(x.Rhs)
	case *ast.ExprStmt:
		return exprText(x.X)
	case *ast.SendStmt:
		return exprText(x.Chan) + " <- " + exprText(x.Value)
	}
	return ""
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
	case *ast.IndexListExpr:
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
	case "e":
		// PocketBase's RequestEvent embeds echo.Context.
		switch c.Sel {
		case "JSON", "JSONPretty", "JSONBlob", "XML", "XMLPretty", "XMLBlob", "HTML", "HTMLBlob",
			"String", "Blob", "Stream", "File", "Attachment", "Inline", "NoContent", "Redirect", "Error":
			return true
		}
	}
	return false
}

// isResponsePrimitive is the net/http write itself, as opposed to a framework
// wrapper: w.WriteHeader(code), w.WriteHeaderNow() and http.Error(w, msg, code).
func isResponsePrimitive(c Call) bool {
	switch c.Sel {
	case "WriteHeader", "WriteHeaderNow":
		return c.Base != "" && c.Expr == c.Base+"."+c.Sel
	case "Error":
		return c.PkgPath == "net/http"
	}
	return false
}

// writesResponse reports whether a resolved helper funnels into the response
// writer, as utils.SendJSONResponse does. A helper that takes an
// http.ResponseWriter is a response writer by signature; one that calls a
// writer is recognized by its body, two calls deep at most.
func writesResponse(c Call) bool {
	if c.Target == nil {
		return false
	}
	if responseWriterParam(c.Target) {
		return true
	}
	return bodyWritesResponse(c.Target, 0, map[*Func]bool{})
}

func responseWriterParam(fn *Func) bool {
	if fn.Decl == nil || fn.Decl.Type.Params == nil {
		return false
	}
	for _, fld := range fn.Decl.Type.Params.List {
		if strings.Contains(typeString(fld.Type), "ResponseWriter") {
			return true
		}
	}
	return false
}

func bodyWritesResponse(fn *Func, depth int, seen map[*Func]bool) bool {
	if fn == nil || depth > 2 || seen[fn] {
		return false
	}
	seen[fn] = true
	for _, c := range fn.Calls {
		if isResponseCall(c) || isResponsePrimitive(c) {
			return true
		}
		if bodyWritesResponse(c.Target, depth+1, seen) {
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
	sw := newStatusWalk()
	codes := map[string]bool{}
	writes := []string{}
	take := func(set map[string]bool) {
		for code := range set {
			codes[code] = true
		}
	}
	noteWrite := func(ce *ast.CallExpr) {
		c, ok := describeCall(ce)
		if !ok {
			return
		}
		if resolved, known := calls[c.Expr]; known {
			c = resolved
		}
		if c.Target != nil && c.Target.Key != "" && writesResponse(c) {
			writes = append(writes, c.Target.Key)
		}
	}
	// if !decode(c, &in) { return } — the helper writes the status itself
	ast.Inspect(x.Cond, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		take(sw.callStatuses(ce, calls, f, nil))
		noteWrite(ce)
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
		take(sw.callStatuses(ce, calls, f, nil))
		noteWrite(ce)
	}
	// the call that can fail is more useful than the condition that tests it
	st := Step{Kind: "exit", Text: exprText(x.Cond), Rk: returnKind(ret.Results), Line: line(x.Pos()), SpanEnd: line(x.End())}
	if st.Rk == "" && errorBranch(x) {
		st.Rk = "error"
	}
	if init, isAssign := x.Init.(*ast.AssignStmt); isAssign && len(init.Rhs) == 1 {
		if ce := firstCall(init.Rhs[0]); ce != nil {
			take(sw.callStatuses(ce, calls, f, nil))
			if c, okc := describeCall(ce); okc {
				if resolved, known := calls[c.Expr]; known {
					c = resolved
				}
				st.Text = exprText(ce)
				st.Tag = tagOf(c)
				st.Iface = c.Iface
				st.Sel = c.Sel
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
	if len(ret.Results) > 0 {
		if ce := firstCall(ret.Results[0]); ce != nil {
			take(sw.callStatuses(ce, calls, f, nil))
		}
	}
	setCodes(&st, codes)
	if len(writes) > 0 {
		st.Writes = uniqueStrings(writes)
	}
	if st.Text != exprText(x.Cond) {
		st.Sub = "if " + exprText(x.Cond)
		st.SubLine = line(x.Pos())
	}
	return st, true
}

// statusWalk follows helpers to every status they can write. A helper with one
// branch (writeErrorMessage) gives one code; a dispatcher with many branches
// (writeError) gives several, and the caller shows all of them. The walk is
// cycle-safe and bounded so a recursive helper cannot trap it.
type statusWalk struct {
	onPath    map[*Func]bool
	localPath map[string]bool
	budget    int
}

func newStatusWalk() *statusWalk {
	return &statusWalk{
		onPath:    map[*Func]bool{},
		localPath: map[string]bool{},
		budget:    400,
	}
}

func (w *statusWalk) callStatuses(ce *ast.CallExpr, calls map[string]Call, fn *Func, bind map[string]map[string]bool) map[string]bool {
	c, ok := describeCall(ce)
	if !ok {
		return nil
	}
	if resolved, known := calls[c.Expr]; known {
		c = resolved
	}
	if isResponseCall(c) {
		return w.statusArgs(ce.Args, fn, bind)
	}
	switch {
	case c.Sel == "WriteHeader" && c.Base != "" && c.Expr == c.Base+"."+c.Sel:
		if len(ce.Args) == 0 {
			return nil
		}
		return w.statusArg(ce.Args[0], fn, bind, map[string]bool{})
	case c.Sel == "WriteHeaderNow":
		return map[string]bool{"200": true}
	case c.Sel == "Error" && c.PkgPath == "net/http":
		if len(ce.Args) < 3 {
			return nil
		}
		return w.statusArg(ce.Args[2], fn, bind, map[string]bool{})
	}
	if c.Target == nil {
		return nil
	}
	if set := w.namedStatusArg(c.Target, ce.Args, fn, bind); len(set) > 0 {
		return set
	}
	return w.bodyStatuses(c.Target, w.bindArgs(c.Target, ce.Args, fn, bind))
}

// namedStatusArg reads a helper's own status parameter from the call site, so
// SendJSONResponse(w, 401, …) answers 401 rather than every fallback branch
// inside the helper's body.
func (w *statusWalk) namedStatusArg(fn *Func, args []ast.Expr, caller *Func, bind map[string]map[string]bool) map[string]bool {
	if fn == nil || fn.Decl == nil || fn.Decl.Type.Params == nil {
		return nil
	}
	i := 0
	for _, fld := range fn.Decl.Type.Params.List {
		for _, name := range fld.Names {
			idx := i
			i++
			if idx >= len(args) {
				return nil
			}
			switch strings.ToLower(name.Name) {
			case "status", "statuscode", "code", "httpstatus", "httpstatuscode":
				if set := w.statusArg(args[idx], caller, bind, map[string]bool{}); len(set) > 0 {
					return set
				}
			}
		}
	}
	return nil
}

// bodyStatuses unions every status the function's body can write, including
// calls several helpers deep. Nested function literals are separate flows and
// are not part of this one.
func (w *statusWalk) bodyStatuses(fn *Func, bind map[string]map[string]bool) map[string]bool {
	out := map[string]bool{}
	if fn == nil || fn.Decl == nil || fn.Decl.Body == nil || w.onPath[fn] || w.budget <= 0 {
		return out
	}
	w.budget--
	w.onPath[fn] = true
	defer delete(w.onPath, fn)

	calls := make(map[string]Call, len(fn.Calls))
	for _, c := range fn.Calls {
		calls[c.Expr] = c
	}
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for code := range w.callStatuses(ce, calls, fn, bind) {
			out[code] = true
		}
		return true
	})
	return out
}

// bindArgs resolves each argument in the caller's scope, so a status held in a
// local variable still follows into the helper that writes it.
func (w *statusWalk) bindArgs(fn *Func, args []ast.Expr, caller *Func, bind map[string]map[string]bool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if fn == nil || fn.Decl == nil || fn.Decl.Type.Params == nil {
		return out
	}
	i := 0
	for _, field := range fn.Decl.Type.Params.List {
		for _, name := range field.Names {
			if i >= len(args) {
				return out
			}
			out[name.Name] = w.statusArg(args[i], caller, bind, map[string]bool{})
			i++
		}
	}
	return out
}

func (w *statusWalk) statusArgs(args []ast.Expr, fn *Func, bind map[string]map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, a := range args {
		for code := range w.statusArg(a, fn, bind, map[string]bool{}) {
			out[code] = true
		}
	}
	return out
}

// stmtStatuses unions the responses written anywhere inside one statement, such
// as every case of a switch that ends in a shared return.
func (w *statusWalk) stmtStatuses(s ast.Stmt, calls map[string]Call, fn *Func) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(s, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for code := range w.callStatuses(ce, calls, fn, nil) {
			out[code] = true
		}
		return true
	})
	return out
}

// callResults resolves one value returned by a helper, for `value, status, err
// := h.service.SaveBranch(...)` where the status is decided in the service.
func (w *statusWalk) callResults(ce *ast.CallExpr, index int, fn *Func, bind map[string]map[string]bool) map[string]bool {
	c, ok := describeCall(ce)
	if !ok {
		return nil
	}
	for _, known := range fn.Calls {
		if known.Expr == c.Expr {
			c = known
			break
		}
	}
	if c.Target == nil {
		return nil
	}
	return w.resultStatuses(c.Target, index, w.bindArgs(c.Target, ce.Args, fn, bind))
}

func (w *statusWalk) resultStatuses(fn *Func, index int, bind map[string]map[string]bool) map[string]bool {
	out := map[string]bool{}
	if fn == nil || fn.Decl == nil || fn.Decl.Body == nil || w.onPath[fn] || w.budget <= 0 {
		return out
	}
	w.budget--
	w.onPath[fn] = true
	defer delete(w.onPath, fn)

	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || index >= len(ret.Results) {
			return true
		}
		for code := range w.statusArg(ret.Results[index], fn, bind, map[string]bool{}) {
			out[code] = true
		}
		return true
	})
	return out
}

func (w *statusWalk) statusArg(e ast.Expr, fn *Func, bind map[string]map[string]bool, seen map[string]bool) map[string]bool {
	out := map[string]bool{}
	switch x := e.(type) {
	case *ast.BasicLit:
		if n, err := strconv.Atoi(x.Value); err == nil && n >= 200 && n <= 599 {
			out[strconv.Itoa(n)] = true
		}
	case *ast.SelectorExpr:
		if code := statusNames[x.Sel.Name]; code != "" {
			out[code] = true
		}
	case *ast.Ident:
		if code := statusNames[x.Name]; code != "" {
			out[code] = true
			return out
		}
		if set, bound := bind[x.Name]; bound {
			for code := range set {
				out[code] = true
			}
			return out
		}
		if seen[x.Name] {
			return out
		}
		seen[x.Name] = true
		for code := range w.localStatuses(fn, x.Name, bind, seen) {
			out[code] = true
		}
	case *ast.ParenExpr:
		return w.statusArg(x.X, fn, bind, seen)
	case *ast.CallExpr:
		if len(x.Args) == 1 {
			if id, ok := x.Fun.(*ast.Ident); ok {
				switch id.Name {
				case "int", "int32", "int64", "uint", "uint32", "uint64":
					return w.statusArg(x.Args[0], fn, bind, seen)
				}
			}
		}
	}
	return out
}

// localStatuses finds every value assigned to a name in the function, so
// `status := http.StatusBadRequest` is resolved where it is used.
func (w *statusWalk) localStatuses(fn *Func, name string, bind map[string]map[string]bool, seen map[string]bool) map[string]bool {
	out := map[string]bool{}
	if fn == nil || fn.Decl == nil || fn.Decl.Body == nil {
		return out
	}
	// bindArgs starts a fresh seen map, so an argument that is assigned from a
	// call taking itself can bounce between localStatuses and statusArg forever.
	key := fn.Key + "\x00" + name
	if w.localPath[key] {
		return out
	}
	w.localPath[key] = true
	defer delete(w.localPath, key)
	take := func(e ast.Expr) {
		for code := range w.statusArg(e, fn, bind, seen) {
			out[code] = true
		}
	}
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		switch x := n.(type) {
		case *ast.AssignStmt:
			multi := len(x.Rhs) == 1 && len(x.Lhs) > 1
			for i, lhs := range x.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name != name {
					continue
				}
				if multi {
					if ce := firstCall(x.Rhs[0]); ce != nil {
						for code := range w.callResults(ce, i, fn, bind) {
							out[code] = true
						}
					}
					continue
				}
				if i < len(x.Rhs) {
					take(x.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if id.Name != name || i >= len(x.Values) {
					continue
				}
				take(x.Values[i])
			}
		}
		return true
	})
	return out
}

// setCodes stores one status as a code, several as codes. There is no single
// right answer for a dispatcher, so the caller sees all of them.
func setCodes(st *Step, codes map[string]bool) {
	list := make([]string, 0, len(codes))
	for code := range codes {
		list = append(list, code)
	}
	sort.Strings(list)
	switch len(list) {
	case 0:
	case 1:
		st.Code = list[0]
	default:
		st.Codes = list
	}
}

// stepCodes returns every status a step can answer with.
func stepCodes(st Step) []string {
	if len(st.Codes) > 0 {
		return st.Codes
	}
	if st.Code != "" {
		return []string{st.Code}
	}
	return nil
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

// guardNames lets a call merge with its guard when the guard tests an error
// result, or a boolean result such as `claim, ok := …; if !ok`.
func guardNames(lhs []ast.Expr, cond ast.Expr) []string {
	names := errorNames(lhs)
	if cond == nil {
		return names
	}
	for _, e := range lhs {
		id, ok := e.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		if isNegated(cond, id.Name) {
			names = append(names, id.Name)
		}
	}
	return names
}

// isNegated matches `!name` and `name == false`, the two ways Go writes a
// failed boolean check.
func isNegated(e ast.Expr, name string) bool {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		id, ok := x.X.(*ast.Ident)
		return x.Op == token.NOT && ok && id.Name == name
	case *ast.BinaryExpr:
		if x.Op != token.EQL && x.Op != token.NEQ {
			return false
		}
		left, lok := x.X.(*ast.Ident)
		right, rok := x.Y.(*ast.Ident)
		return (lok && left.Name == name && rok && right.Name == "false") ||
			(rok && right.Name == name && lok && left.Name == "false")
	}
	return false
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

// returnKind marks the outcome of a return without guessing about values the
// source does not name. A bare return is void; nil and error returns are the
// two outcomes a reader needs to tell apart.
func returnKind(list []ast.Expr) string {
	if len(list) == 0 {
		return "void"
	}
	allNil := true
	for _, e := range list {
		if isNilExpr(e) {
			continue
		}
		allNil = false
		if isErrorExpr(e) {
			return "error"
		}
	}
	if allNil {
		return "nil"
	}
	return ""
}

// errorBranch recognizes an if that is handling an error even when the branch
// answers with a response instead of returning the error value itself. The
// condition is the reliable signal; a failure constructor in the body catches
// branches such as len(items) == 0 that build a New...Fail response.
func errorBranch(x *ast.IfStmt) bool {
	if errorCondition(x.Cond) {
		return true
	}
	found := false
	ast.Inspect(x.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		found = errorConstructor(exprText(ce.Fun))
		return !found
	})
	return found
}

func errorCondition(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return errorCondition(x.X)
	case *ast.UnaryExpr:
		return x.Op == token.NOT && errorCondition(x.X)
	case *ast.CallExpr:
		name := exprText(x.Fun)
		return name == "errors.Is" || name == "errors.As"
	case *ast.BinaryExpr:
		if x.Op != token.NEQ && x.Op != token.EQL {
			return false
		}
		if isNilExpr(x.Y) {
			return errorOperand(x.X)
		}
		if isNilExpr(x.X) {
			return errorOperand(x.Y)
		}
	}
	return false
}

func errorOperand(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return errorOperand(x.X)
	case *ast.Ident:
		return isErrorName(x.Name)
	case *ast.SelectorExpr:
		return isErrorName(x.Sel.Name)
	}
	return false
}

func isErrorName(name string) bool {
	return name == "err" || strings.HasPrefix(name, "Err") || strings.HasSuffix(name, "Err") || strings.HasSuffix(name, "Error")
}

func errorConstructor(name string) bool {
	if name == "fmt.Errorf" || name == "errors.New" || name == "errors.Join" {
		return true
	}
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	if name == "Errorf" {
		return true
	}
	if !strings.HasPrefix(name, "New") {
		return false
	}
	rest := name[3:]
	for _, word := range []string{"Fail", "Error", "Invalid", "Unauthorized", "Forbidden", "NotFound", "BadRequest"} {
		if strings.Contains(rest, word) {
			return true
		}
	}
	return false
}

func isNilExpr(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == "nil"
	case *ast.ParenExpr:
		return isNilExpr(x.X)
	}
	return false
}

func isErrorExpr(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return isErrorExpr(x.X)
	case *ast.Ident:
		return isErrorName(x.Name)
	case *ast.CallExpr:
		name := exprText(x.Fun)
		return name == "errors.New" || name == "errors.Join" || name == "fmt.Errorf" || strings.HasSuffix(name, ".Errorf")
	case *ast.CompositeLit:
		name := exprText(x.Type)
		return strings.HasSuffix(name, "Err") || strings.HasSuffix(name, "Error")
	}
	return false
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
