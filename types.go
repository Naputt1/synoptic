package main

import (
	"go/ast"
	"strings"
)

// JTypeMethod is one method on an interface or struct.
type JTypeMethod struct {
	N   string `json:"n"`
	Key string `json:"k,omitempty"`
	Sig string `json:"sig,omitempty"`
	L   int    `json:"l,omitempty"`
}

// JTypeField is one struct field.
type JTypeField struct {
	N  string `json:"n"`
	T  string `json:"t"`
	Tg string `json:"tg,omitempty"`
	E  bool   `json:"e,omitempty"`
}

// JType is an interface or struct reachable from route calls.
type JType struct {
	K       string        `json:"k"` // interface | struct
	N       string        `json:"n"`
	Key     string        `json:"key"`
	Pkg     string        `json:"pkg"`
	File    string        `json:"f,omitempty"`
	L       int           `json:"l,omitempty"`
	Fields  []JTypeField  `json:"fields,omitempty"`
	Methods []JTypeMethod `json:"methods,omitempty"`
	Impls   []string      `json:"impls,omitempty"`
}

type typeDecl struct {
	key  string
	name string
	pkg  string
	file *FileCtx
	line int
	it   *ast.InterfaceType
	st   *ast.StructType
}

func buildTypeCatalog(m *Module) map[string]JType {
	decls := map[string]*typeDecl{}
	for _, fc := range m.Files {
		for _, d := range fc.Ast.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok.String() != "type" {
				continue
			}
			for _, sp := range gd.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok {
					continue
				}
				key := fc.PkgPath + "|" + ts.Name.Name
				td := &typeDecl{key: key, name: ts.Name.Name, pkg: fc.PkgPath, file: fc, line: line(ts.Pos())}
				switch t := ts.Type.(type) {
				case *ast.InterfaceType:
					td.it = t
				case *ast.StructType:
					td.st = t
				}
				if td.it != nil || td.st != nil {
					decls[key] = td
				}
			}
		}
	}

	out := map[string]JType{}
	for key, td := range decls {
		jt := JType{N: td.name, Key: key, Pkg: td.pkg, File: td.file.Rel, L: td.line}
		switch {
		case td.it != nil:
			jt.K = "interface"
			jt.Methods = interfaceMethods(td.it)
		case td.st != nil:
			jt.K = "struct"
			jt.Fields = structFields(td.st)
			for _, f := range m.ByRecv[td.name] {
				if f.PkgPath != td.pkg {
					continue
				}
				jt.Methods = append(jt.Methods, JTypeMethod{N: f.Name, Key: f.Key, Sig: funcSig(f.Decl.Type), L: f.Start})
			}
		}
		out[key] = jt
	}

	// interfaces -> concrete structs whose method names and arities cover them
	for ikey, iface := range out {
		if iface.K != "interface" {
			continue
		}
		for skey, st := range out {
			if st.K != "struct" || !m.implements(ikey, st.Pkg, st.N) {
				continue
			}
			iface.Impls = append(iface.Impls, skey)
			st.Impls = append(st.Impls, ikey)
			out[skey] = st
		}
		out[ikey] = iface
	}
	return out
}

func interfaceMethods(it *ast.InterfaceType) []JTypeMethod {
	var out []JTypeMethod
	if it.Methods == nil {
		return out
	}
	for _, field := range it.Methods.List {
		ft, ok := field.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		for _, name := range field.Names {
			out = append(out, JTypeMethod{N: name.Name, Sig: funcSig(ft)})
		}
	}
	return out
}

func structFields(st *ast.StructType) []JTypeField {
	var out []JTypeField
	if st.Fields == nil {
		return out
	}
	for _, field := range st.Fields.List {
		t := typeString(field.Type)
		tag := ""
		if field.Tag != nil {
			tag = strings.Trim(field.Tag.Value, "`")
		}
		if len(field.Names) == 0 {
			out = append(out, JTypeField{N: t, T: t, Tg: tag, E: true})
			continue
		}
		for _, name := range field.Names {
			out = append(out, JTypeField{N: name.Name, T: t, Tg: tag})
		}
	}
	return out
}

func funcSig(ft *ast.FuncType) string {
	if ft == nil {
		return ""
	}
	var params, results []string
	if ft.Params != nil {
		for _, f := range ft.Params.List {
			t := typeString(f.Type)
			for range max(1, len(f.Names)) {
				params = append(params, t)
			}
		}
	}
	if ft.Results != nil {
		for _, f := range ft.Results.List {
			t := typeString(f.Type)
			for range max(1, len(f.Names)) {
				results = append(results, t)
			}
		}
	}
	s := "(" + strings.Join(params, ", ") + ")"
	if len(results) == 1 {
		s += " " + results[0]
	} else if len(results) > 1 {
		s += " (" + strings.Join(results, ", ") + ")"
	}
	return s
}

func covers(iface, impl []JTypeMethod) bool {
	if len(iface) == 0 || len(impl) == 0 {
		return false
	}
	for _, want := range iface {
		found := false
		for _, got := range impl {
			if got.N == want.N && arity(got.Sig) == arity(want.Sig) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func arity(sig string) int {
	sig = strings.TrimSpace(sig)
	if !strings.HasPrefix(sig, "(") {
		return 0
	}
	end := matchingParen(sig, 0)
	if end < 0 {
		return 0
	}
	inner := strings.TrimSpace(sig[1:end])
	if inner == "" {
		return 0
	}
	return strings.Count(inner, ",") + 1
}

func matchingParen(s string, at int) int {
	depth := 0
	for i := at; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// annotateStep adds the struct and interface context to a service method call.
func annotateStep(s *Step, types map[string]JType, m *Module) {
	if s.IfaceKey != "" && s.Key == "" {
		impls := m.implementers(s.IfaceKey, s.Sel, nil)
		if len(impls) > 1 {
			s.Ik = s.IfaceKey
			s.Impls = make([]JImpl, 0, len(impls))
			for _, fn := range impls {
				s.Impls = append(s.Impls, JImpl{K: fn.Key, N: fn.Name, R: fn.Recv, P: fn.PkgPath, F: fn.File.Rel, L: fn.Start})
			}
		}
	}
	if s.Key == "" {
		return
	}
	parts := strings.Split(s.Key, "|")
	if len(parts) != 3 {
		return
	}
	sk := parts[0] + "|" + parts[1]
	st, ok := types[sk]
	if !ok || st.K != "struct" {
		return
	}
	s.Sk = sk
	for _, ik := range st.Impls {
		iface, ok := types[ik]
		if !ok {
			continue
		}
		for _, im := range iface.Methods {
			if im.N == parts[2] {
				s.Ik = ik
				return
			}
		}
	}
}

func annotateSteps(list []Step, types map[string]JType, m *Module) {
	for i := range list {
		annotateStep(&list[i], types, m)
		annotateSteps(list[i].Body, types, m)
		annotateSteps(list[i].Else, types, m)
	}
}
