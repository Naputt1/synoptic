package main

import (
	"go/ast"
	"strings"
)

// headerAliases caches the local variables that hold a response header map,
// such as h := c.Writer.Header(). A write through one of those aliases is a
// response header write even though the call no longer names the context.
var headerAliases = map[*Func]map[string]bool{}

func headerVars(f *Func) map[string]bool {
	if f == nil || f.Decl == nil || f.Decl.Body == nil {
		return nil
	}
	if v, ok := headerAliases[f]; ok {
		return v
	}
	v := map[string]bool{}
	ast.Inspect(f.Decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Lhs) != len(x.Rhs) {
				return true
			}
			for i, rhs := range x.Rhs {
				if isResponseHeaderMap(rhs) {
					if name := identName(x.Lhs[i]); name != "" {
						v[name] = true
					}
				}
			}
		case *ast.ValueSpec:
			if len(x.Names) != len(x.Values) {
				return true
			}
			for i, val := range x.Values {
				if isResponseHeaderMap(val) {
					v[x.Names[i].Name] = true
				}
			}
		}
		return true
	})
	headerAliases[f] = v
	return v
}

// isResponseHeaderMap reports an expression that returns the response header
// map: c.Writer.Header(), ctx.ResponseWriter.Header(), w.Header().
func isResponseHeaderMap(e ast.Expr) bool {
	ce, ok := e.(*ast.CallExpr)
	if !ok || len(ce.Args) != 0 {
		return false
	}
	c, ok := describeCall(ce)
	if !ok || c.Sel != "Header" {
		return false
	}
	switch c.Base {
	case "w", "rw", "res", "writer":
		return c.Expr == c.Base+".Header"
	}
	return strings.HasSuffix(c.Expr, ".Writer.Header") || strings.HasSuffix(c.Expr, ".ResponseWriter.Header")
}

// isResponseHeaderWrite reports a write to the response header map, either
// through an alias of it or through a direct .Header().Set/Add chain.
func isResponseHeaderWrite(c Call, f *Func) bool {
	if c.Sel != "Set" && c.Sel != "Add" {
		return false
	}
	if strings.HasSuffix(c.Expr, ".Header().Set") || strings.HasSuffix(c.Expr, ".Header().Add") {
		return true
	}
	return headerVars(f)[c.Base]
}

// isGinHeaderSet reports c.Header("name", value), the gin shortcut for a
// response header write.
func isGinHeaderSet(ce *ast.CallExpr, c Call) bool {
	if (c.Base != "c" && c.Base != "ctx") || c.Sel != "Header" {
		return false
	}
	return len(ce.Args) >= 2
}
