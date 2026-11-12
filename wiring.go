package main

import (
	"go/ast"
	"go/token"
)

// indexWiring collects the static evidence that one concrete type is assigned
// to one interface. It reads only syntax this module owns: a composite literal
// field whose declared type is an interface, a variable declared with an
// interface type, an assignment to such a variable, or a call argument paired
// with an interface parameter. Nothing is followed across functions or returns,
// so the index never invents a target from dataflow.
//
// The index is a set per interface, not a per-call answer. If two different
// implementations are wired to the same interface anywhere in the module, that
// interface stays ambiguous even when each call site looks locally clear: a
// global disagreement is stronger evidence than a local guess. See ifaceImpl.
func (m *Module) indexWiring() {
	m.Wiring = map[string]map[string]bool{}
	for _, fc := range m.Files {
		for _, d := range fc.Ast.Decls {
			switch x := d.(type) {
			case *ast.GenDecl:
				if x.Tok != token.VAR {
					continue
				}
				s := &wireScope{m: m, file: fc, env: map[string]FieldRef{}}
				for _, spec := range x.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					ast.Inspect(vs, func(n ast.Node) bool {
						s.collect(n)
						return true
					})
				}
			case *ast.FuncDecl:
				if x.Body != nil {
					m.collectFuncWiring(fc, x)
				}
			}
		}
	}
}

// wireScope is the declared-type context of one function body (or one package
// variable initializer). env holds parameters, named results and locals with an
// explicit type; recv/recvVar name the receiver so a field assignment can be
// read. It deliberately does not hold locals declared with := from a concrete
// value: that says nothing about which interface they were assigned to.
type wireScope struct {
	m       *Module
	file    *FileCtx
	env     map[string]FieldRef
	recv    string
	recvVar string
}

func (m *Module) collectFuncWiring(fc *FileCtx, fd *ast.FuncDecl) {
	s := &wireScope{
		m:       m,
		file:    fc,
		env:     map[string]FieldRef{},
		recv:    recvName(fd),
		recvVar: recvVar(fd),
	}
	if f := m.Funcs[fnKey(fc.PkgPath, s.recv, fd.Name.Name)]; f != nil {
		for n, ref := range f.Vars {
			s.env[n] = ref
		}
	}

	// One source-order walk. A local is declared in the AST before any use in
	// the same block, so collect can add its type to env as it meets the
	// declaration. Nested function literals are skipped: their variables shadow
	// the outer scope, and a guess there would be worse than a missing binding.
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		s.collect(n)
		return true
	})
}

// collect dispatches one AST node to the evidence it can provide. The walk
// visits every expression once, so a nested composite literal is read by its
// own node and not by its parent.
func (s *wireScope) collect(n ast.Node) {
	switch x := n.(type) {
	case *ast.CompositeLit:
		s.fieldWiring(x)
	case *ast.ValueSpec:
		s.valueSpecWiring(x)
		if x.Type != nil {
			ref := s.m.typeRefOf(s.file, x.Type)
			for _, name := range x.Names {
				s.env[name.Name] = ref
			}
		}
	case *ast.AssignStmt:
		s.assignWiring(x)
	case *ast.CallExpr:
		s.callWiring(x)
	}
}

// fieldWiring reads Server{svc: &Postgres{}}: the struct's field is declared as
// an interface, and the literal names the concrete type assigned to it.
func (s *wireScope) fieldWiring(cl *ast.CompositeLit) {
	if cl.Type == nil {
		return
	}
	ref := s.m.typeRefOf(s.file, cl.Type)
	fields := s.m.Fields[recvKey(ref.Pkg, ref.Name)]
	if len(fields) == 0 {
		return
	}
	for _, el := range cl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		field, ok := fields[key.Name]
		if !ok {
			continue
		}
		s.record(field, s.concrete(kv.Value))
	}
}

// valueSpecWiring reads var s Service = &Postgres{}. Only an explicit interface
// type is evidence; var s = &Postgres{} names no interface at all.
func (s *wireScope) valueSpecWiring(vs *ast.ValueSpec) {
	if vs.Type == nil {
		return
	}
	ref := s.m.typeRefOf(s.file, vs.Type)
	if !s.isIface(ref) {
		return
	}
	for _, v := range vs.Values {
		s.record(ref, s.concrete(v))
	}
}

// assignWiring reads s = &Postgres{} through a declared interface (parameter,
// result, package variable, interface-typed local or receiver field), and the
// explicit conversion s := Service(&Postgres{}).
func (s *wireScope) assignWiring(as *ast.AssignStmt) {
	for i, lhs := range as.Lhs {
		if i >= len(as.Rhs) {
			continue
		}
		rhs := as.Rhs[i]
		switch as.Tok {
		case token.DEFINE:
			ref, ok := s.conversionType(rhs)
			if !ok {
				continue
			}
			if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
				s.env[id.Name] = ref
			}
			s.record(ref, s.concrete(callArg(rhs)))
		case token.ASSIGN:
			ref, ok := s.lhsType(lhs)
			if !ok || !s.isIface(ref) {
				continue
			}
			s.record(ref, s.concrete(rhs))
		}
	}
}

// callWiring reads NewServer(&Postgres{}): each argument is paired with the
// callee's parameter at the same index, and only an interface parameter takes a
// concrete argument as wiring evidence.
func (s *wireScope) callWiring(ce *ast.CallExpr) {
	target := s.callee(ce)
	if target == nil {
		return
	}
	params := s.m.paramRefs(target)
	for i, arg := range ce.Args {
		if i >= len(params) {
			return
		}
		if !s.isIface(params[i]) {
			continue
		}
		if ce.Ellipsis.IsValid() && i == len(ce.Args)-1 {
			continue // the argument is a slice, not one element
		}
		s.record(params[i], s.concrete(arg))
	}
}

// callee finds the indexed function a call names. It stays on package functions
// and methods of a typed value; a conversion and a function value have no
// target and therefore provide no parameter evidence.
func (s *wireScope) callee(ce *ast.CallExpr) *Func {
	switch x := unindex(ce.Fun).(type) {
	case *ast.Ident:
		return s.m.PKG[s.file.PkgPath][x.Name]
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if p, ok := s.file.Imports[id.Name]; ok {
				return s.m.PKG[p][x.Sel.Name]
			}
			if id.Name == s.recvVar && s.recv != "" {
				return s.m.methodOn(s.file.PkgPath, s.recv, x.Sel.Name)
			}
		}
		if ref := s.concrete(x.X); ref.Name != "" {
			return s.m.methodOn(ref.Pkg, ref.Name, x.Sel.Name)
		}
	}
	return nil
}

// paramRefs flattens a function's parameters in call order. A variadic
// parameter is read by its element type, so a Route argument still counts.
func (m *Module) paramRefs(fn *Func) []FieldRef {
	if fn == nil || fn.Decl == nil || fn.Decl.Type == nil || fn.Decl.Type.Params == nil {
		return nil
	}
	var out []FieldRef
	for _, fld := range fn.Decl.Type.Params.List {
		typ := fld.Type
		if el, ok := typ.(*ast.Ellipsis); ok {
			typ = el.Elt
		}
		ref := m.typeRefOf(fn.File, typ)
		n := len(fld.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			out = append(out, ref)
		}
	}
	return out
}

// concrete names the concrete type of an assigning expression: a composite
// literal with or without &, or a variable whose declared type is that concrete
// type. Anything else returns the zero FieldRef and records nothing.
func (s *wireScope) concrete(e ast.Expr) FieldRef {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return s.concrete(x.X)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return s.concrete(x.X)
		}
	case *ast.CompositeLit:
		if x.Type != nil {
			return s.m.typeRefOf(s.file, x.Type)
		}
	case *ast.Ident:
		ref, _ := s.declType(x.Name)
		return ref
	case *ast.SelectorExpr:
		ref, _ := s.selectorType(x)
		return ref
	}
	return FieldRef{}
}

func (s *wireScope) lhsType(e ast.Expr) (FieldRef, bool) {
	switch x := e.(type) {
	case *ast.Ident:
		return s.declType(x.Name)
	case *ast.SelectorExpr:
		return s.selectorType(x)
	}
	return FieldRef{}, false
}

// declType reads a name's declared type without following assignments: a
// parameter or result, a local with an explicit type, then a package variable.
func (s *wireScope) declType(name string) (FieldRef, bool) {
	if ref, ok := s.env[name]; ok {
		return ref, true
	}
	ref, ok := s.m.Vars[s.file.PkgPath+"|"+name]
	return ref, ok
}

// selectorType reads pkg.Value and the receiver's own field, s.svc. A field of
// a local struct value is not followed; that would need dataflow.
func (s *wireScope) selectorType(x *ast.SelectorExpr) (FieldRef, bool) {
	id, ok := x.X.(*ast.Ident)
	if !ok {
		return FieldRef{}, false
	}
	if id.Name == s.recvVar && s.recv != "" {
		ref, ok := s.m.Fields[recvKey(s.file.PkgPath, s.recv)][x.Sel.Name]
		return ref, ok
	}
	if p, ok := s.file.Imports[id.Name]; ok {
		ref, ok := s.m.Vars[p+"|"+x.Sel.Name]
		return ref, ok
	}
	return FieldRef{}, false
}

// conversionType reports the interface type of an explicit conversion such as
// Service(&Postgres{}). A call to anything else, including a constructor, is
// not a conversion and returns false.
func (s *wireScope) conversionType(e ast.Expr) (FieldRef, bool) {
	ce, ok := unindex(e).(*ast.CallExpr)
	if !ok || len(ce.Args) != 1 {
		return FieldRef{}, false
	}
	ref := s.m.typeRefOf(s.file, ce.Fun)
	return ref, s.isIface(ref)
}

func (s *wireScope) isIface(ref FieldRef) bool {
	_, ok := s.m.Ifaces[recvKey(ref.Pkg, ref.Name)]
	return ok
}

// record adds one assignment to the index. It keeps only in-module concrete
// types that actually declare every method of the interface, and it drops
// generated-only receivers the same way ifaceImpl does. A type whose declared
// type is another interface says nothing about the concrete implementation, so
// it is ignored rather than followed.
func (s *wireScope) record(iface, concrete FieldRef) {
	ms, ok := s.m.Ifaces[recvKey(iface.Pkg, iface.Name)]
	if !ok || concrete.Pkg == "" || concrete.Name == "" {
		return
	}
	if !s.m.Types[concrete.Pkg][concrete.Name] {
		return
	}
	if _, isIface := s.m.Ifaces[recvKey(concrete.Pkg, concrete.Name)]; isIface {
		return
	}
	if !s.m.declaresAll(concrete.Pkg, concrete.Name, ms) {
		return
	}
	if generatedOnly(s.m.recvMethods(concrete.Pkg, concrete.Name)) {
		return
	}
	key := recvKey(iface.Pkg, iface.Name)
	if s.m.Wiring[key] == nil {
		s.m.Wiring[key] = map[string]bool{}
	}
	s.m.Wiring[key][recvKey(concrete.Pkg, concrete.Name)] = true
}

// callArg returns the single argument of a conversion call.
func callArg(e ast.Expr) ast.Expr {
	ce, ok := unindex(e).(*ast.CallExpr)
	if !ok || len(ce.Args) != 1 {
		return nil
	}
	return ce.Args[0]
}
