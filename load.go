package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
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
	Qual    string // receiver expression, e.g. "s.userStatusService" in s.userStatusService.GetUser
	Sel     string // last identifier, e.g. "Table"
	Expr    string // full chain as written
	Iface   string // declared type of the field the call goes through, when that field is not a struct
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

// FieldRef is a struct field's declared type, already resolved through the
// imports of the file that declares the struct — so a caller file that does not
// import that package still finds the method.
type FieldRef struct {
	Pkg  string
	Name string
	Text string
}

// Diag records what the loader could not read or resolve, so a report can name
// the analyzer's blind spots instead of counting only what worked.
type Diag struct {
	Parsed     int              `json:"parsed"`
	Skipped    int              `json:"skipped"`
	Read       int              `json:"read,omitempty"`
	Parse      int              `json:"parse,omitempty"`
	Generated  int              `json:"generated,omitempty"`
	Skip       []SkippedFile    `json:"skip,omitempty"`
	Calls      int              `json:"calls"`
	Resolved   int              `json:"resolved"`
	Unresolved int              `json:"unresolved"`
	Unres      []UnresolvedCall `json:"unres,omitempty"`
	Truncated  int              `json:"truncated,omitempty"`
}

// SkippedFile is one source file the loader did not read into the module.
type SkippedFile struct {
	Path   string `json:"p"`
	Reason string `json:"r"` // read | parse | generated
}

// UnresolvedCall is a call that pointed inside this module and still had no
// target, with the function that contains it.
type UnresolvedCall struct {
	Func string `json:"fn"`
	Expr string `json:"e"`
	File string `json:"f,omitempty"`
	Line int    `json:"l,omitempty"`
}

// diagLimit bounds the lists in Diag; the counters stay exact.
const diagLimit = 20

type Module struct {
	Root    string
	Path    string
	Name    string
	Files   []*FileCtx
	Funcs   map[string]*Func
	PKG     map[string]map[string]*Func    // pkgPath -> name -> package level func
	Methods map[string][]*Func             // method name -> every method with that name
	ByRecv  map[string][]*Func             // receiver type -> methods
	Fields  map[string]map[string]FieldRef // "pkg|Type" -> field -> declared type
	Ifaces  map[string][]string            // "pkg|Interface" -> exported method names
	PkgDocs map[string]string
	Diag    Diag
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
		Fields:  map[string]map[string]FieldRef{},
		Ifaces:  map[string][]string{},
		PkgDocs: map[string]string{},
	}
	modPath, modName := readGoMod(abs)
	m.Path, m.Name = modPath, modName
	mods := findModules(abs)

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
		rel, _ := filepath.Rel(abs, p)
		rel = filepath.ToSlash(rel)
		src, err := os.ReadFile(p)
		if err != nil {
			m.skipFile(rel, "read")
			continue
		}
		if strings.Contains(string(src[:min(len(src), 200)]), "Code generated") && len(src) > 400_000 {
			m.skipFile(rel, "generated")
			continue
		}
		af, err := parser.ParseFile(fset, p, src, parser.ParseComments)
		if err != nil {
			m.skipFile(rel, "parse")
			continue
		}
		pkgPath := pkgPathFor(p, modPath, mods)
		fc := &FileCtx{
			Path:    p,
			Rel:     rel,
			PkgPath: pkgPath,
			Imports: map[string]string{},
			Lines:   strings.Split(string(src), "\n"),
			Ast:     af,
		}
		m.Diag.Parsed++
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
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if it, ok := ts.Type.(*ast.InterfaceType); ok {
						var names []string
						for _, mth := range it.Methods.List {
							for _, n := range mth.Names {
								if n.IsExported() {
									names = append(names, n.Name)
								}
							}
						}
						if len(names) > 0 {
							m.Ifaces[fc.PkgPath+"|"+ts.Name.Name] = names
						}
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					fields := map[string]FieldRef{}
					for _, fld := range st.Fields.List {
						typ := typeString(fld.Type)
						pkg, name := typeRef(fc, typ)
						for _, n := range fld.Names {
							fields[n.Name] = FieldRef{Pkg: pkg, Name: name, Text: typ}
						}
					}
					if len(fields) > 0 {
						m.Fields[fc.PkgPath+"|"+ts.Name.Name] = fields
					}
				}
				continue
			}
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

// skipFile records a source file the loader could not use. The counters stay
// exact while the path list is bounded, so a broken tree cannot bloat the page.
func (m *Module) skipFile(rel, reason string) {
	m.Diag.Skipped++
	switch reason {
	case "read":
		m.Diag.Read++
	case "parse":
		m.Diag.Parse++
	case "generated":
		m.Diag.Generated++
	}
	if len(m.Diag.Skip) < diagLimit {
		m.Diag.Skip = append(m.Diag.Skip, SkippedFile{Path: rel, Reason: reason})
	}
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
		// methods called on the current receiver: h.requireParentPhone(...)
		if c.Target == nil && f.RecvVar != "" && c.Qual == f.RecvVar && f.Recv != "" {
			for _, fn := range m.Methods[c.Sel] {
				if fn.Recv == f.Recv {
					c.Target = fn
					c.PkgPath = fn.PkgPath
					break
				}
			}
		}
		// methods on a field of the receiver: s.userStatusService.GetUser(...).
		// The field's declared type decides the target, so a same-named method on
		// the receiver itself is not picked by mistake.
		if c.Target == nil && fieldCall(c.Qual, f.RecvVar) {
			field := c.Qual[len(f.RecvVar)+1:]
			if ref, ok := m.Fields[f.PkgPath+"|"+f.Recv][field]; ok {
				c.Iface = ref.Text
				if fn := m.methodOn(ref.Pkg, ref.Name, c.Sel); fn != nil {
					c.Target = fn
					c.PkgPath = fn.PkgPath
				} else if fn := m.ifaceImpl(ref.Pkg, ref.Name, c.Sel, f); fn != nil {
					c.Target = fn
					c.PkgPath = fn.PkgPath
				}
			}
		}
		// service fields: h.service.CreateBatch(...)
		if c.Target == nil && isServiceReceiver(c.Expr) {
			for _, fn := range m.Methods[c.Sel] {
				if isServiceReceiver(fn.Recv) {
					c.Target = fn
					c.PkgPath = fn.PkgPath
					break
				}
			}
		}
		out = append(out, c)
		m.Diag.Calls++
		if c.Target != nil {
			m.Diag.Resolved++
			return true
		}
		if m.unresolvedCall(f, c) {
			m.Diag.Unresolved++
			if len(m.Diag.Unres) < diagLimit {
				m.Diag.Unres = append(m.Diag.Unres, UnresolvedCall{
					Func: fnName(f),
					Expr: c.Expr,
					File: f.File.Rel,
					Line: c.Line,
				})
			}
		}
		return true
	})
	return out
}

// builtins are language functions, not calls this module could resolve.
var builtins = map[string]bool{
	"len": true, "cap": true, "append": true, "make": true, "new": true,
	"copy": true, "delete": true, "panic": true, "recover": true,
	"print": true, "println": true, "complex": true, "real": true,
	"imag": true, "close": true, "clear": true, "min": true, "max": true,
}

// unresolvedCall reports whether a call should have resolved inside this
// module but did not. Stdlib and framework calls are not blind spots: their
// PkgPath is in no package of this module.
func (m *Module) unresolvedCall(f *Func, c Call) bool {
	if c.PkgPath != "" {
		_, inModule := m.PKG[c.PkgPath]
		return inModule
	}
	if c.Base != "" || c.Qual != "" {
		return false
	}
	return !builtins[c.Sel] && m.PKG[f.PkgPath][c.Sel] == nil
}

// fieldCall reports whether the call hangs off a field of the receiver,
// as in s.userService.GetUser: exactly one selector past the receiver.
func fieldCall(qual, recvVar string) bool {
	if recvVar == "" || !strings.HasPrefix(qual, recvVar+".") {
		return false
	}
	rest := qual[len(recvVar)+1:]
	return rest != "" && !strings.Contains(rest, ".")
}

// methodOn finds sel on the declared field type. It returns nil for interface
// types: their methods live on implementations, not on the type itself.
func (m *Module) methodOn(pkgPath, name, sel string) *Func {
	for _, fn := range m.Methods[sel] {
		if fn.PkgPath == pkgPath && fn.Recv == name {
			return fn
		}
	}
	return nil
}

// typeRef resolves a type as written in a file to its package path and name,
// following the file's import aliases.
func typeRef(file *FileCtx, typeText string) (string, string) {
	t := strings.TrimPrefix(typeText, "*")
	if i := strings.Index(t, "["); i >= 0 {
		t = t[:i]
	}
	if i := strings.LastIndex(t, "."); i >= 0 {
		alias, short := t[:i], t[i+1:]
		if p, ok := file.Imports[alias]; ok {
			return p, short
		}
	}
	return file.PkgPath, t
}

// ifaceImpl finds the method sel on the one concrete type that declares every
// method of the interface: the receiver an interface call reaches when there is
// exactly one, as with a repository interface and its store. Generated stubs
// are skipped, and an ambiguous interface resolves to nothing.
func (m *Module) ifaceImpl(pkgPath, name, sel string, self *Func) *Func {
	ms, ok := m.Ifaces[pkgPath+"|"+name]
	if !ok {
		return nil
	}
	var match *Func
	for _, fn := range m.Methods[sel] {
		if fn.Recv == "" || !m.declaresAll(fn.PkgPath, fn.Recv, ms) {
			continue
		}
		// the caller's own method is not an implementation of its field's interface
		if self != nil && fn.PkgPath == self.PkgPath && fn.Recv == self.Recv {
			continue
		}
		if generatedOnly(m.recvMethods(fn.PkgPath, fn.Recv)) {
			continue
		}
		if match != nil {
			return nil // more than one implementation: do not guess
		}
		match = fn
	}
	return match
}

// declaresAll reports whether the type has a method for every name.
func (m *Module) declaresAll(pkgPath, recv string, names []string) bool {
	for _, n := range names {
		found := false
		for _, fn := range m.Methods[n] {
			if fn.PkgPath == pkgPath && fn.Recv == recv {
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

// recvMethods collects every method declared on a type.
func (m *Module) recvMethods(pkgPath, recv string) []*Func {
	var out []*Func
	for _, fns := range m.Methods {
		for _, fn := range fns {
			if fn.PkgPath == pkgPath && fn.Recv == recv {
				out = append(out, fn)
			}
		}
	}
	return out
}

func describeCall(ce *ast.CallExpr) (Call, bool) {
	switch fun := unindex(ce.Fun).(type) {
	case *ast.Ident:
		return Call{Sel: fun.Name, Expr: fun.Name}, true
	case *ast.SelectorExpr:
		base, ex := chain(fun.X)
		return Call{Base: base, Qual: ex, Sel: fun.Sel.Name, Expr: ex + "." + fun.Sel.Name}, true
	}
	return Call{}, false
}

// unindex strips generic type arguments, so pkg.Get[T](x) reads like pkg.Get(x).
func unindex(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		default:
			return e
		}
	}
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
	case *ast.ArrayType:
		return "[]" + typeString(x.Elt)
	case *ast.MapType:
		return "map[" + typeString(x.Key) + "]" + typeString(x.Value)
	case *ast.ChanType:
		switch x.Dir {
		case ast.SEND:
			return "chan<- " + typeString(x.Value)
		case ast.RECV:
			return "<-chan " + typeString(x.Value)
		}
		return "chan " + typeString(x.Value)
	case *ast.Ellipsis:
		return "..." + typeString(x.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	}
	return ""
}

func isRouterType(e ast.Expr) bool {
	s := typeString(e)
	switch s {
	case "*fiber.App", "fiber.Router", "fiber.Group",
		"*gin.Engine", "*gin.RouterGroup", "gin.IRouter", "gin.IRoutes",
		"chi.Router", "chi.Mux", "*mux.Router", "*echo.Echo", "echo.Group":
		return true
	}
	for _, n := range []string{"RouterGroup", "Router", "ServeMux", "Engine", "ControllerRegister", "Container"} {
		if strings.Contains(s, n) && !strings.Contains(s, "Route[") && !strings.Contains(s, "Route;") {
			return true
		}
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
	var walk func(ast.Expr) string
	walk = func(e ast.Expr) string {
		switch t := e.(type) {
		case *ast.StarExpr:
			return walk(t.X)
		case *ast.ParenExpr:
			return walk(t.X)
		case *ast.IndexExpr:
			return walk(t.X)
		case *ast.IndexListExpr:
			return walk(t.X)
		case *ast.Ident:
			return t.Name
		}
		return ""
	}
	return walk(fd.Recv.List[0].Type)
}

func fnKey(pkg, recv, name string) string {
	if recv != "" {
		return pkg + "|" + recv + "|" + name
	}
	return pkg + "|" + name
}

// modRoot is one go.mod inside the tree. A repo may hold several modules, each
// with its own import path; files belong to the deepest module above them.
type modRoot struct {
	dir  string
	path string
}

func findModules(abs string) []modRoot {
	var out []modRoot
	filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != abs && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		dir := filepath.Dir(p)
		path, _ := readGoMod(dir)
		out = append(out, modRoot{dir: dir, path: path})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return len(out[i].dir) > len(out[j].dir) })
	return out
}

// pkgPathFor assigns a file the import path of its owning module, so imports
// such as shared/pkg/proto/jobpb resolve inside a multi-module workspace.
func pkgPathFor(p, fallback string, mods []modRoot) string {
	for _, mr := range mods {
		if p != mr.dir && !strings.HasPrefix(p, mr.dir+string(filepath.Separator)) {
			continue
		}
		rel, err := filepath.Rel(mr.dir, filepath.Dir(p))
		if err != nil || rel == "." {
			return mr.path
		}
		return mr.path + "/" + filepath.ToSlash(rel)
	}
	return fallback
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
