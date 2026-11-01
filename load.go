package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type FileCtx struct {
	Path    string
	Rel     string
	PkgPath string
	Imports map[string]string // local alias -> import path
	Lines   []string
	Ast     *ast.File
}

type Call struct {
	Base    string // first identifier of the chain, e.g. "config" in config.DB.Table
	Sel     string // last identifier, e.g. "Table"
	Expr    string // full chain as written
	Line    int
	PkgPath string
	Target  *Func
}

type Func struct {
	Key     string
	PkgPath string
	Name    string
	Recv    string
	RecvVar string
	File    *FileCtx
	Decl    *ast.FuncDecl
	Doc     string
	Start   int
	End     int
	Calls   []Call
}

type Module struct {
	Root    string
	Path    string
	Name    string
	Files   []*FileCtx
	Funcs   map[string]*Func
	PKG     map[string]map[string]*Func // pkgPath -> name -> package level func
	Methods map[string][]*Func          // method name -> every method with that name
	ByRecv  map[string][]*Func          // receiver type -> methods
	PkgDocs map[string]string
}

var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, ".git": true, "tmp": true,
	"testdata": true, "dist": true, "build": true, ".idea": true,
	".vscode": true, "postgres-data": true, "logs": true,
}

func loadModule(root string) (*Module, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	m := &Module{
		Root:    abs,
		Funcs:   map[string]*Func{},
		PKG:     map[string]map[string]*Func{},
		Methods: map[string][]*Func{},
		ByRecv:  map[string][]*Func{},
		PkgDocs: map[string]string{},
	}
	modPath, modName := readGoMod(abs)
	m.Path, m.Name = modPath, modName

	var paths []string
	err = filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || (strings.HasPrefix(d.Name(), ".") && d.Name() != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.Contains(string(src[:min(len(src), 200)]), "Code generated") {
			continue
		}
		af, err := parser.ParseFile(fset, p, src, parser.ParseComments)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(abs, p)
		dir := filepath.Dir(rel)
		pkgPath := modPath
		if dir != "." {
			pkgPath = modPath + "/" + filepath.ToSlash(dir)
		}
		fc := &FileCtx{
			Path:    p,
			Rel:     filepath.ToSlash(rel),
			PkgPath: pkgPath,
			Imports: map[string]string{},
			Lines:   strings.Split(string(src), "\n"),
			Ast:     af,
		}
		for _, imp := range af.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			alias := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			fc.Imports[alias] = path
		}
		m.Files = append(m.Files, fc)
		if af.Doc != nil && m.PkgDocs[pkgPath] == "" {
			m.PkgDocs[pkgPath] = cleanDoc(af.Doc.Text())
		}
	}

	for _, fc := range m.Files {
		for _, d := range fc.Ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			f := &Func{
				PkgPath: fc.PkgPath,
				Name:    fd.Name.Name,
				Recv:    recvName(fd),
				RecvVar: recvVar(fd),
				File:    fc,
				Decl:    fd,
				Start:   line(fd.Pos()),
				End:     line(fd.End()),
			}
			if fd.Doc != nil {
				f.Doc = cleanDoc(fd.Doc.Text())
			}
			f.Key = fnKey(f.PkgPath, f.Recv, f.Name)
			m.Funcs[f.Key] = f
			if f.Recv == "" {
				if m.PKG[f.PkgPath] == nil {
					m.PKG[f.PkgPath] = map[string]*Func{}
				}
				m.PKG[f.PkgPath][f.Name] = f
			} else {
				m.Methods[f.Name] = append(m.Methods[f.Name], f)
				m.ByRecv[f.Recv] = append(m.ByRecv[f.Recv], f)
			}
		}
	}

	for _, f := range m.Funcs {
		f.Calls = m.callsOf(f)
	}
	return m, nil
}

func (m *Module) callsOf(f *Func) []Call {
	var out []Call
	seen := map[string]bool{}
	ast.Inspect(f.Decl.Body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		c, ok := describeCall(ce)
		if !ok {
			return true
		}
		key := c.Expr
		if seen[key] {
			return true
		}
		seen[key] = true
		c.Line = line(ce.Pos())
		if c.Base == "" {
			if fn := m.PKG[f.PkgPath][c.Sel]; fn != nil {
				c.Target = fn
			}
		} else if p, ok := f.File.Imports[c.Base]; ok {
			c.PkgPath = p
			if fn := m.PKG[p][c.Sel]; fn != nil {
				c.Target = fn
			}
		}
		out = append(out, c)
		return true
	})
	return out
}

func describeCall(ce *ast.CallExpr) (Call, bool) {
	switch fun := ce.Fun.(type) {
	case *ast.Ident:
		return Call{Sel: fun.Name, Expr: fun.Name}, true
	case *ast.SelectorExpr:
		base, ex := chain(fun.X)
		return Call{Base: base, Sel: fun.Sel.Name, Expr: ex + "." + fun.Sel.Name}, true
	}
	return Call{}, false
}

// chain returns the first identifier and the dotted expression text.
func chain(e ast.Expr) (string, string) {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name, x.Name
	case *ast.SelectorExpr:
		b, s := chain(x.X)
		return b, s + "." + x.Sel.Name
	case *ast.CallExpr:
		b, s := chain(x.Fun)
		return b, s + "()"
	}
	return "", ""
}

func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// keyOf names anything a router can live in: a variable, a parameter, or a
// field path such as s.app.
func keyOf(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if b := keyOf(x.X); b != "" {
			return b + "." + x.Sel.Name
		}
	}
	return ""
}

func typeString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return "*" + typeString(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return typeString(x.X) + "." + x.Sel.Name
	case *ast.IndexExpr:
		return typeString(x.X)
	}
	return ""
}

func isRouterType(e ast.Expr) bool {
	switch typeString(e) {
	case "*fiber.App", "fiber.Router", "fiber.Group",
		"*gin.Engine", "*gin.RouterGroup", "gin.IRouter", "gin.IRoutes",
		"chi.Router", "chi.Mux", "*mux.Router", "*echo.Echo", "echo.Group":
		return true
	}
	return false
}

func recvVar(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 {
		return ""
	}
	return fd.Recv.List[0].Names[0].Name
}

func recvName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	switch t := fd.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func fnKey(pkg, recv, name string) string {
	if recv != "" {
		return pkg + "|" + recv + "|" + name
	}
	return pkg + "|" + name
}

func readGoMod(root string) (string, string) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "module", filepath.Base(root)
	}
	path := ""
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "module ") {
			path = strings.TrimSpace(strings.TrimPrefix(ln, "module"))
			break
		}
	}
	if path == "" {
		return "module", filepath.Base(root)
	}
	return path, path[strings.LastIndex(path, "/")+1:]
}

func cleanDoc(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(ln)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

var fset = token.NewFileSet()

func line(p token.Pos) int { return fset.Position(p).Line }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
