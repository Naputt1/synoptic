package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// typedLoadMode is everything the resolver needs: type information, the syntax
// it is keyed by, and the dependencies those types refer to.
const typedLoadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedSyntax |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedDeps

// callSite keys a call by file and byte offset. The AST pass and the type
// checker parse the same bytes, so an offset is stable across the two file
// sets even though token.Pos values are not comparable between them.
type callSite struct {
	file string
	off  int
}

// resolveTypedCalls type-checks the module under root and fills Call.Target for
// calls the AST heuristics could not name: promoted and embedded methods,
// aliases, generic instantiations and package-level calls whose import name
// differs from the directory name. It never overwrites a target the AST pass
// already found, so AST mode output is a strict prefix of typed mode output.
//
// Interface dispatch stays static: the type checker reports the interface
// method for a call through an interface value, not one implementation, so
// those calls are left to the existing single-implementation fallback.
func resolveTypedCalls(m *Module, root string) (resolved int, err error) {
	index := make(map[callSite][]*Call)
	norm := newPathNormalizer()
	for _, f := range m.Funcs {
		for i := range f.Calls {
			c := &f.Calls[i]
			if c.Target != nil || !c.Pos.IsValid() {
				continue
			}
			p := fset.PositionFor(c.Pos, false)
			if !p.IsValid() {
				continue
			}
			site := callSite{file: norm.file(p.Filename), off: p.Offset}
			index[site] = append(index[site], c)
		}
	}
	if len(index) == 0 {
		m.recount()
		return 0, nil
	}

	cfg := &packages.Config{
		Mode:  typedLoadMode,
		Dir:   root,
		Tests: false,
		Env:   typedEnv(),
	}
	pkgs, loadErr := packages.Load(cfg, "./...")

	var problems []string
	if loadErr != nil {
		problems = append(problems, loadErr.Error())
	}
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, e := range pkg.Errors {
			problems = append(problems, e.Error())
		}
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if pkg.Fset == nil {
					return true
				}
				p := pkg.Fset.PositionFor(ce.Pos(), false)
				if !p.IsValid() {
					return true
				}
				list := index[callSite{file: norm.file(p.Filename), off: p.Offset}]
				if len(list) == 0 {
					return true
				}
				fn := typedCallee(pkg.TypesInfo, ce)
				if fn == nil {
					return true
				}
				target := typedFunc(m, fn)
				if target == nil {
					return true
				}
				for _, c := range list {
					// Only fill a gap. An AST result is never replaced.
					if c.Target != nil {
						continue
					}
					c.Target = target
					c.PkgPath = target.PkgPath
					resolved++
				}
				return true
			})
		}
	}

	// Typed targets change what counts as resolved, so the honest metric must
	// be rebuilt from the calls, not decremented by hand.
	m.recount()

	if len(problems) > 0 {
		sort.Strings(problems)
		head := problems
		if len(head) > 3 {
			head = head[:3]
		}
		return resolved, fmt.Errorf("%d package errors: %s", len(problems), strings.Join(head, "; "))
	}
	return resolved, nil
}

// typedCallee returns the function object behind a call expression. A method
// (including one promoted from an embedded type) is a selection; a call through
// a package qualifier or a bare identifier is a use. Fields and builtins are
// not functions and fall through.
func typedCallee(info *types.Info, ce *ast.CallExpr) *types.Func {
	switch fun := unindex(ce.Fun).(type) {
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[fun]; ok {
			if fn, ok := sel.Obj().(*types.Func); ok {
				return fn
			}
			return nil
		}
		if fn, ok := info.Uses[fun.Sel].(*types.Func); ok {
			return fn
		}
	case *ast.Ident:
		if fn, ok := info.Uses[fun].(*types.Func); ok {
			return fn
		}
	}
	return nil
}

// typedFunc maps a type-checker function to the indexed function the rest of
// the analyzer walks. Methods are keyed by package and receiver type name, so
// a promoted method points at the type that actually declares it.
func typedFunc(m *Module, fn *types.Func) *Func {
	pkg := fn.Pkg()
	if pkg == nil {
		return nil
	}
	recv := ""
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		recv = recvTypeName(sig.Recv().Type())
		if recv == "" {
			return nil
		}
	}
	return m.Funcs[fnKey(pkg.Path(), recv, fn.Name())]
}

// recvTypeName unwraps a receiver to its named type, stripping pointers and
// aliases so a method on *T and a method on T share one key.
func recvTypeName(t types.Type) string {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	if n, ok := t.(*types.Named); ok && n.Obj() != nil {
		return n.Obj().Name()
	}
	return ""
}

// typedEnv keeps a parent go.work from pulling packages outside root into the
// type-checked build.
func typedEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOWORK=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GOWORK=off")
}

// pathNormalizer canonicalizes file names so the parser's path and the type
// checker's path compare equal even when one goes through a symlink, as /var
// does on macOS.
type pathNormalizer struct {
	cache map[string]string
}

func newPathNormalizer() *pathNormalizer {
	return &pathNormalizer{cache: map[string]string{}}
}

func (n *pathNormalizer) file(p string) string {
	if v, ok := n.cache[p]; ok {
		return v
	}
	v := p
	if abs, err := filepath.Abs(v); err == nil {
		v = abs
	}
	v = filepath.Clean(v)
	if resolved, err := filepath.EvalSymlinks(v); err == nil {
		v = filepath.Clean(resolved)
	}
	n.cache[p] = v
	return v
}
