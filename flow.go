package main

import (
	"bytes"
	"go/ast"
	"go/printer"
	"strconv"
	"strings"
)

// Step is one box in a function's code flow: a call that matters, a branch that
// changes where control goes, or the way out. Assignments and defer are not steps.
type Step struct {
	Kind  string `json:"k"` // call, exit, if, loop, switch, ret, note
	Text  string `json:"t"`
	Sub   string `json:"s,omitempty"`
	Code  string `json:"cd,omitempty"`
	Line  int    `json:"l,omitempty"`
	Tag   string `json:"g,omitempty"`
	Key   string `json:"fk,omitempty"`
	File  string `json:"f,omitempty"`
	Count int    `json:"c,omitempty"`
	Body  []Step `json:"b,omitempty"`
	Else  []Step `json:"e,omitempty"`
}

func buildFlow(f *Func) []Step {
	if f == nil || f.Decl == nil || f.Decl.Body == nil {
		return nil
	}
	body := f.Decl.Body.List
	// middleware factories hand back their handler; that body is the flow
	if len(body) == 1 {
		if ret, ok := body[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			if lit, isLit := ret.Results[0].(*ast.FuncLit); isLit {
				return flowSteps(f, lit.Body.List)
			}
		}
	}
	return flowSteps(f, body)
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

	for _, s := range list {
		switch x := s.(type) {
		case *ast.IfStmt:
			if ex, ok := exitOf(f, x, calls); ok {
				add(ex)
				continue
			}
			st := Step{Kind: "if", Text: exprText(x.Cond), Line: line(x.Pos())}
			if x.Init != nil {
				st.Sub = exprText(stmtExpr(x.Init))
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
	if !known && isNoise(c) {
		return Step{}
	}
	if !known && c.Target == nil && c.PkgPath == "" && tagOf(c) == "" {
		return Step{}
	}

	st := Step{Kind: "call", Text: exprText(ce), Tag: tagOf(c), Line: line(ce.Pos())}
	if c.Target != nil {
		st.Sub = altPkg(c.Target.PkgPath)
		st.File = c.Target.File.Rel
		if c.Target.Key != "" {
			st.Key = c.Target.Key
		}
		st.Line = c.Target.Start
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
	switch c.Base {
	case "c", "ctx", "fiber", "gin":
		switch c.Sel {
		case "JSON", "XML", "String", "Send", "SendString", "SendStatus", "Render", "Redirect",
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
	for _, s := range x.Body.List {
		es, isExpr := s.(*ast.ExprStmt)
		if !isExpr {
			continue
		}
		ce := firstCall(es.X)
		if ce == nil {
			continue
		}
		if c, okc := describeCall(ce); okc && isResponseCall(c) {
			if code := statusIn(ce); code != "" {
				guardCode = code
			}
		}
	}
	// the call that can fail is more useful than the condition that tests it
	st := Step{Kind: "exit", Text: exprText(x.Cond), Line: line(x.Pos())}
	if init, isAssign := x.Init.(*ast.AssignStmt); isAssign && len(init.Rhs) == 1 {
		if ce := firstCall(init.Rhs[0]); ce != nil {
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
					st.File = c.Target.File.Rel
					st.Line = c.Target.Start
				}
			}
		}
	}
	code := guardCode
	if len(ret.Results) > 0 {
		if ce := firstCall(ret.Results[0]); ce != nil {
			if c := statusIn(ce); c != "" {
				code = c
			}
		}
	}
	st.Code = code
	if st.Text != exprText(x.Cond) {
		st.Sub = "if " + exprText(x.Cond)
	}
	return st, true
}

func statusIn(ce *ast.CallExpr) string {
	for _, a := range ce.Args {
		if lit, ok := a.(*ast.BasicLit); ok {
			if n, err := strconv.Atoi(lit.Value); err == nil && n >= 200 && n <= 599 {
				return strconv.Itoa(n)
			}
		}
		if sel, ok := a.(*ast.SelectorExpr); ok {
			if n, found := statusNames[sel.Sel.Name]; found {
				return n
			}
		}
		if id, ok := a.(*ast.Ident); ok {
			if n, found := statusNames[id.Name]; found {
				return n
			}
		}
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
