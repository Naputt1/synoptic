package main

import (
	"go/ast"
	"strings"
)

// resolveDispatchedHandlers rewrites route stages that point at a generated
// interface wrapper so they point at the concrete method the wrapper calls.
// oapi-codegen registers ServerInterfaceWrapper.GetAdminAssistant, for
// example, while the real handler is Handler.GetAdminAssistant on the struct
// passed to the generated Register function. Without this the route documents
// and bakes the wrapper's one-line return instead of the handler.
func resolveDispatchedHandlers(m *Module, routes []*Route, types map[string]JType) {
	for _, r := range routes {
		for i := range r.Chain {
			ref := r.Chain[i]
			if ref.Fn == nil {
				continue
			}
			fn := dispatchedImpl(m, ref.Fn, types)
			if fn == nil {
				continue
			}
			ref.Name = fn.Name
			ref.Kind = "method"
			ref.File = fn.File
			ref.Line = fn.Start
			ref.Fn = fn
			r.Chain[i] = ref
		}
	}
}

// dispatchedImpl returns the concrete method behind a thin interface wrapper,
// or nil when the shape is not unambiguous. It only follows wrappers that
// delegate to an interface field with the same method name, and only when the
// interface has exactly one implementation besides the wrapper itself.
func dispatchedImpl(m *Module, fn *Func, types map[string]JType) *Func {
	if fn == nil || fn.Recv == "" || fn.Decl == nil || fn.Decl.Body == nil || fn.File == nil {
		return nil
	}
	recv := strings.TrimPrefix(fn.Recv, "*")
	selfKey := fn.PkgPath + "|" + recv
	wrapper, ok := types[selfKey]
	if !ok || wrapper.K != "struct" {
		return nil
	}
	for _, f := range wrapper.Fields {
		iface, ok := findInterface(types, fn.PkgPath, f.T)
		if !ok || len(iface.Impls) == 0 {
			continue
		}
		if !delegatesTo(fn.Decl.Body, f.N, fn.Name) {
			continue
		}
		var impls []string
		for _, key := range iface.Impls {
			if key != selfKey {
				impls = append(impls, key)
			}
		}
		if len(impls) != 1 {
			continue
		}
		pkg, name := splitTypeKey(impls[0])
		for _, cand := range m.Recv[recvKey(pkg, name)] {
			if cand.Name == fn.Name {
				return cand
			}
		}
	}
	return nil
}

// findInterface resolves a field's written type to a catalog interface. The
// common case is an unqualified name in the same package.
func findInterface(types map[string]JType, pkg, raw string) (JType, bool) {
	name := strings.TrimSpace(raw)
	name = strings.TrimPrefix(name, "*")
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		qual, short := name[:i], name[i+1:]
		for _, t := range types {
			if t.K == "interface" && t.N == short &&
				(t.Pkg == qual || strings.HasSuffix(t.Pkg, "/"+qual)) {
				return t, true
			}
		}
		return JType{}, false
	}
	if t, ok := types[pkg+"|"+name]; ok && t.K == "interface" {
		return t, true
	}
	var hit JType
	found := false
	for _, t := range types {
		if t.K != "interface" || t.N != name {
			continue
		}
		if found {
			return JType{}, false
		}
		hit, found = t, true
	}
	return hit, found
}

func splitTypeKey(key string) (pkg, name string) {
	i := strings.IndexByte(key, '|')
	if i < 0 {
		return "", ""
	}
	return key[:i], key[i+1:]
}

// delegatesTo reports whether the body calls field.method(...), which is how
// generated wrappers hand control to the interface implementation.
func delegatesTo(body *ast.BlockStmt, field, method string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != method {
			return true
		}
		recv, ok := sel.X.(*ast.SelectorExpr)
		if ok && recv.Sel.Name == field {
			found = true
			return false
		}
		return true
	})
	return found
}
