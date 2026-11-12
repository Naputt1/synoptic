package main

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
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
	// Why is the one reason this call did or did not resolve: resolved,
	// external, builtin, convert, blind, dynamic, ambiguous or unknown. It is
	// not serialized: the diag block is the public, aggregated form.
	Why string `json:"-"`
	// Pos is the call site in the loader's file set. It is not serialized; the
	// typed pass uses it to match this call to the type checker's own syntax.
	Pos token.Pos `json:"-"`
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
	// Vars maps parameters and named results to their declared types. Calls on
	// them can be resolved by declaration instead of guessed from assignments.
	Vars  map[string]FieldRef
	Calls []Call
}

// FieldRef is a struct field's declared type, already resolved through the
// imports of the file that declares the struct — so a caller file that does not
// import that package still finds the method.
type FieldRef struct {
	Pkg  string
	Name string
	Text string
	Func bool // the field's type is a function value, not a callable type
}

// Diag records what the loader could not read or resolve, so a report can name
// the analyzer's blind spots instead of counting only what worked.
type Diag struct {
	Parsed     int              `json:"parsed"`
	Skipped    int              `json:"skipped"`
	Read       int              `json:"read,omitempty"`
	Parse      int              `json:"parse,omitempty"`
	Generated  int              `json:"generated,omitempty"`
	Build      int              `json:"build,omitempty"`
	Skip       []SkippedFile    `json:"skip,omitempty"`
	Calls      int              `json:"calls"`
	Resolved   int              `json:"resolved"`
	Unresolved int              `json:"unresolved"` // blind only: the real gap
	External   int              `json:"ext,omitempty"`
	Convert    int              `json:"conv,omitempty"`
	Dynamic    int              `json:"dyn,omitempty"`
	Ambiguous  int              `json:"amb,omitempty"`
	Unknown    int              `json:"unk,omitempty"`
	Why        map[string]int   `json:"why,omitempty"`
	Unres      []UnresolvedCall `json:"unres,omitempty"`
	Truncated  int              `json:"truncated,omitempty"`
}

// SkippedFile is one source file the loader did not read into the module.
type SkippedFile struct {
	Path   string `json:"p"`
	Reason string `json:"r"` // read | parse | generated | build
}

// UnresolvedCall is a blind call: it pointed at an in-module function that the
// index does not contain, with the function that contains it.
type UnresolvedCall struct {
	Func   string `json:"fn"`
	Expr   string `json:"e"`
	File   string `json:"f,omitempty"`
	Line   int    `json:"l,omitempty"`
	Reason string `json:"r,omitempty"`
}

// diagLimit bounds the lists in Diag; the counters stay exact.
const diagLimit = 20

type Module struct {
	Root      string
	Path      string
	Name      string
	Files     []*FileCtx
	Funcs     map[string]*Func
	PKG       map[string]map[string]*Func    // pkgPath -> name -> package level func
	Methods   map[string][]*Func             // method name -> every method with that name
	Recv      map[string][]*Func             // "pkgPath|receiver type" -> methods
	Fields    map[string]map[string]FieldRef // "pkg|Type" -> field -> declared type
	Embeds    map[string][]FieldRef          // "pkg|Type" -> embedded types, in field order
	Ifaces    map[string][]string            // "pkg|Interface" -> exported method names
	Types     map[string]map[string]bool     // pkgPath -> every declared type name
	FuncTypes map[string]bool                // "pkg|Type" -> declared func type
	FuncVars  map[string]map[string]bool     // pkgPath -> package var of func type
	Vars      map[string]FieldRef            // "pkgPath|name" -> declared package var/const type
	// Wiring maps "pkg|Interface" to the set of "pkg|Type" the code visibly
	// assigns to it. It is collected once after indexing and never mutated.
	Wiring  map[string]map[string]bool
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
		Root:      abs,
		Funcs:     map[string]*Func{},
		PKG:       map[string]map[string]*Func{},
		Methods:   map[string][]*Func{},
		Recv:      map[string][]*Func{},
		Fields:    map[string]map[string]FieldRef{},
		Embeds:    map[string][]FieldRef{},
		Ifaces:    map[string][]string{},
		Types:     map[string]map[string]bool{},
		FuncTypes: map[string]bool{},
		FuncVars:  map[string]map[string]bool{},
		Vars:      map[string]FieldRef{},
		Wiring:    map[string]map[string]bool{},
		PkgDocs:   map[string]string{},
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

	for _, r := range parseFiles(paths, abs, modPath, mods) {
		if r.reason != "" {
			m.skipFile(r.rel, r.reason)
			continue
		}
		m.Diag.Parsed++
		m.Files = append(m.Files, r.fc)
		if r.doc != "" && m.PkgDocs[r.fc.PkgPath] == "" {
			m.PkgDocs[r.fc.PkgPath] = r.doc
		}
	}

	for _, fc := range m.Files {
		if m.Types[fc.PkgPath] == nil {
			m.Types[fc.PkgPath] = map[string]bool{}
		}
		for _, d := range fc.Ast.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				m.indexFuncVars(fc, gd)
				continue
			}
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if m.Types[fc.PkgPath] == nil {
						m.Types[fc.PkgPath] = map[string]bool{}
					}
					m.Types[fc.PkgPath][ts.Name.Name] = true
					if _, ok := ts.Type.(*ast.FuncType); ok {
						m.FuncTypes[fc.PkgPath+"|"+ts.Name.Name] = true
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
					var embeds []FieldRef
					for _, fld := range st.Fields.List {
						typ := typeString(fld.Type)
						pkg, name := typeRef(fc, typ)
						isFunc := m.isFuncType(fc, fld.Type)
						if len(fld.Names) == 0 {
							// An embedded field promotes its own methods (and, for
							// an embedded interface, its method names). The field
							// has no name of its own, so it is not a Fields entry.
							if embed, ok := embedRef(pkg, name, typ); ok {
								embeds = append(embeds, embed)
							}
							continue
						}
						for _, n := range fld.Names {
							fields[n.Name] = FieldRef{Pkg: pkg, Name: name, Text: typ, Func: isFunc}
						}
					}
					if len(fields) > 0 {
						m.Fields[fc.PkgPath+"|"+ts.Name.Name] = fields
					}
					if len(embeds) > 0 {
						m.Embeds[fc.PkgPath+"|"+ts.Name.Name] = embeds
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
				Vars:    m.fieldRefs(fc, fd.Type.Params),
			}
			for n, ref := range m.fieldRefs(fc, fd.Type.Results) {
				f.Vars[n] = ref
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
				k := recvKey(f.PkgPath, f.Recv)
				m.Recv[k] = append(m.Recv[k], f)
			}
		}
	}

	for _, fc := range m.Files {
		for _, d := range fc.Ast.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && (gd.Tok == token.VAR || gd.Tok == token.CONST) {
				m.indexPackageVars(fc, gd)
			}
		}
	}

	// Wiring evidence is read from the AST after every type, field, variable
	// and package function is indexed, and before calls are resolved: ifaceImpl
	// consults it, and it must be complete and immutable by then.
	m.indexWiring()

	for _, f := range m.Funcs {
		f.Calls = m.callsOf(f)
	}
	m.recount()
	return m, nil
}

// fileResult is one file's outcome from the parallel parse phase. The merge
// step applies results in walk order, so module contents and diagnostics stay
// deterministic even though files are read and parsed concurrently.
type fileResult struct {
	rel    string
	reason string // read | parse | generated | build
	fc     *FileCtx
	doc    string
}

// parseFiles parses every path concurrently. The shared token.FileSet is safe
// here: its AddFile and per-file line tables are mutex protected, and the
// parser writes only to the file it owns. Positions therefore keep their
// meaning and line numbers are unchanged.
func parseFiles(paths []string, abs, modPath string, mods []modRoot) []fileResult {
	out := make([]fileResult, len(paths))
	workers := runtime.GOMAXPROCS(0)
	if workers > len(paths) {
		workers = len(paths)
	}
	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				out[i] = parseOne(paths[i], abs, modPath, mods)
			}
		}()
	}
	for i := range paths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}

func parseOne(p, abs, modPath string, mods []modRoot) fileResult {
	rel, _ := filepath.Rel(abs, p)
	rel = filepath.ToSlash(rel)
	// Inactive files (GOOS/GOARCH suffixes and //go:build lines) must not be
	// read: parsing them would invent routes that never ship on this platform.
	if ok, err := build.Default.MatchFile(filepath.Dir(p), filepath.Base(p)); err != nil || !ok {
		return fileResult{rel: rel, reason: "build"}
	}
	src, err := os.ReadFile(p)
	if err != nil {
		return fileResult{rel: rel, reason: "read"}
	}
	if strings.Contains(string(src[:min(len(src), 200)]), "Code generated") && len(src) > 400_000 {
		return fileResult{rel: rel, reason: "generated"}
	}
	af, err := parser.ParseFile(fset, p, src, parser.ParseComments)
	if err != nil {
		return fileResult{rel: rel, reason: "parse"}
	}
	fc := &FileCtx{
		Path:    p,
		Rel:     rel,
		PkgPath: pkgPathFor(p, modPath, mods),
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
	r := fileResult{rel: rel, fc: fc}
	if af.Doc != nil {
		r.doc = cleanDoc(af.Doc.Text())
	}
	return r
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
	case "build":
		m.Diag.Build++
	}
	if len(m.Diag.Skip) < diagLimit {
		m.Diag.Skip = append(m.Diag.Skip, SkippedFile{Path: rel, Reason: reason})
	}
}

func (m *Module) callsOf(f *Func) []Call {
	var out []Call
	seen := map[string]bool{}
	dyn := m.funcVars(f)
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
		c.Pos = ce.Pos()
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
				} else if fn, _ := m.ifaceImpl(ref.Pkg, ref.Name, c.Sel, f); fn != nil {
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
		// methods on values whose type is declared: parameters, named results and
		// package variables. Assignments are deliberately not followed.
		if c.Target == nil {
			m.resolveDeclared(f, &c)
		}
		c.Why = m.why(f, c, dyn)
		out = append(out, c)
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

// basicTypes are conversions to a predeclared type. They read like calls but
// have no function behind them.
var basicTypes = map[string]bool{
	"bool": true, "string": true, "any": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "byte": true, "rune": true,
	"float32": true, "float64": true, "complex64": true, "complex128": true,
}

// why names the one reason c did or did not resolve. A found target wins over
// every heuristic; a conversion or a builtin is never a blind spot even when
// its name also looks like a missing function.
func (m *Module) why(f *Func, c Call, dyn map[string]bool) string {
	if c.Target != nil {
		return "resolved"
	}
	if m.isConvert(f, c) {
		return "convert"
	}
	if c.Base == "" && builtins[c.Sel] {
		return "builtin"
	}
	if m.isFuncValue(f, c, dyn) {
		return "dynamic"
	}
	if p, ok := f.File.Imports[c.Base]; ok {
		if c.Qual == c.Base {
			if !m.inModule(p) {
				return "external"
			}
			return "blind" // an in-module package function the index does not hold
		}
		// a package-qualified value: config.DB.Exec
		if field := firstField(c.Qual, c.Base); field != "" {
			if ref, ok := m.Vars[p+"|"+field]; ok {
				return m.declReason(f, ref, c.Sel, c.Qual == c.Base+"."+field)
			}
		}
		if !m.inModule(p) {
			return "external"
		}
		return "unknown" // a method on a package-level value of unknown type
	}
	if c.Base == "" {
		return "blind"
	}
	if fieldCall(c.Qual, f.RecvVar) {
		field := c.Qual[len(f.RecvVar)+1:]
		if ref, ok := m.Fields[f.PkgPath+"|"+f.Recv][field]; ok {
			if !m.hasPkg(ref.Pkg) {
				return "external"
			}
			if _, ambiguous := m.ifaceImpl(ref.Pkg, ref.Name, c.Sel, f); ambiguous {
				return "ambiguous"
			}
		}
	}
	// a value of a declared type: parameter, named result or package variable
	if c.Qual == c.Base {
		if ref, ok := m.declaredType(f, c.Base); ok {
			return m.declReason(f, ref, c.Sel, true)
		}
	}
	return "unknown"
}

// declaredType is the declared type of a value: a parameter or named result of
// f first, then a package-level variable in its package.
func (m *Module) declaredType(f *Func, name string) (FieldRef, bool) {
	if ref, ok := f.Vars[name]; ok {
		return ref, true
	}
	ref, ok := m.Vars[f.PkgPath+"|"+name]
	return ref, ok
}

// declReason names why a call on a declared type did or did not resolve. An
// in-module struct with no such method is blind: that is a real in-module gap,
// not an alias or a conversion. An interface with several implementations is
// ambiguous, and a type with no implementation indexed is unknown.
func (m *Module) declReason(f *Func, ref FieldRef, sel string, direct bool) string {
	if ref.Name == "" {
		return "unknown"
	}
	if !m.inModule(ref.Pkg) {
		return "external"
	}
	if !direct {
		return "unknown" // the call is further down the chain than the declared value
	}
	if !m.Types[ref.Pkg][ref.Name] {
		return "unknown" // a type parameter or a type the index does not hold
	}
	if m.methodOn(ref.Pkg, ref.Name, sel) != nil {
		return "resolved"
	}
	if _, ok := m.Ifaces[ref.Pkg+"|"+ref.Name]; ok {
		if _, ambiguous := m.ifaceImpl(ref.Pkg, ref.Name, sel, f); ambiguous {
			return "ambiguous"
		}
		return "unknown"
	}
	return "blind"
}

// resolveDeclared fills a target from a declared receiver type. It handles both
// s.GetUser() (a parameter or package variable) and config.DB.Exec (the first
// selector of a package-qualified value). Anything assigned inside a function
// stays unknown, so tx := db.Begin() is not guessed from the call on the right.
func (m *Module) resolveDeclared(f *Func, c *Call) {
	var ref FieldRef
	var ok, direct bool
	if c.Qual == c.Base {
		if c.Base == "" {
			return
		}
		if _, isImport := f.File.Imports[c.Base]; isImport {
			return
		}
		ref, ok = m.declaredType(f, c.Base)
		direct = true
	} else if p, isImport := f.File.Imports[c.Base]; isImport {
		field := firstField(c.Qual, c.Base)
		if field == "" {
			return
		}
		ref, ok = m.Vars[p+"|"+field]
		direct = c.Qual == c.Base+"."+field
	}
	if !ok || !direct || ref.Name == "" || !m.inModule(ref.Pkg) {
		return
	}
	if fn := m.methodOn(ref.Pkg, ref.Name, c.Sel); fn != nil {
		c.Target = fn
		c.PkgPath = fn.PkgPath
		return
	}
	if fn, _ := m.ifaceImpl(ref.Pkg, ref.Name, c.Sel, f); fn != nil {
		c.Target = fn
		c.PkgPath = fn.PkgPath
		c.Iface = ref.Text
	}
}

// firstField returns the selector directly after base in a dotted expression:
// DB for config.DB.Exec and for config.DB.Model().Where.
func firstField(qual, base string) string {
	rest := strings.TrimPrefix(qual, base+".")
	if rest == qual {
		return ""
	}
	if i := strings.IndexByte(rest, '.'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// isConvert reports whether the call is a type conversion: a basic type, or a
// type declared by a package whose import path this module owns.
func (m *Module) isConvert(f *Func, c Call) bool {
	if c.Base == "" {
		return basicTypes[c.Sel] || m.Types[f.PkgPath][c.Sel]
	}
	// pkg.Type(x): the receiver is the package itself. A chain such as
	// pkg.Type.Method() is a method call, not a conversion.
	if c.Qual != c.Base {
		return false
	}
	p, ok := f.File.Imports[c.Base]
	return ok && m.Types[p][c.Sel]
}

// isFuncValue reports whether the callee is a function value rather than a
// declared function: a func-typed parameter, local, package variable or field.
func (m *Module) isFuncValue(f *Func, c Call, dyn map[string]bool) bool {
	if c.Base == "" {
		return dyn[c.Sel] || m.FuncVars[f.PkgPath][c.Sel]
	}
	if fieldCall(c.Qual, f.RecvVar) {
		field := c.Qual[len(f.RecvVar)+1:]
		ref, ok := m.Fields[f.PkgPath+"|"+f.Recv][field]
		return ok && ref.Func
	}
	// a call on a func-typed field of the receiver itself: s.cb()
	if f.RecvVar != "" && c.Qual == f.RecvVar {
		ref, ok := m.Fields[f.PkgPath+"|"+f.Recv][c.Sel]
		return ok && ref.Func
	}
	return false
}

// funcVars collects the parameters and locals of f that hold a function value.
// A call through one is dynamic dispatch: there is no static target to find.
func (m *Module) funcVars(f *Func) map[string]bool {
	out := map[string]bool{}
	if f.Decl == nil {
		return out
	}
	if f.Decl.Type != nil && f.Decl.Type.Params != nil {
		for _, p := range f.Decl.Type.Params.List {
			if !m.isFuncType(f.File, p.Type) {
				continue
			}
			for _, n := range p.Names {
				out[n.Name] = true
			}
		}
	}
	ast.Inspect(f.Decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range x.Rhs {
				if i >= len(x.Lhs) || !isFuncRef(rhs) {
					continue
				}
				if id, ok := x.Lhs[i].(*ast.Ident); ok {
					out[id.Name] = true
				}
			}
		case *ast.ValueSpec:
			if !m.isFuncType(f.File, x.Type) && !hasFuncRef(x.Values) {
				return true
			}
			for _, n := range x.Names {
				out[n.Name] = true
			}
		}
		return true
	})
	return out
}

// indexFuncVars records package variables that hold a function value, so a call
// through one is dynamic instead of a missing in-module function.
func (m *Module) indexFuncVars(fc *FileCtx, gd *ast.GenDecl) {
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if !m.isFuncType(fc, vs.Type) && !hasFuncRef(vs.Values) {
			continue
		}
		if m.FuncVars[fc.PkgPath] == nil {
			m.FuncVars[fc.PkgPath] = map[string]bool{}
		}
		for _, n := range vs.Names {
			m.FuncVars[fc.PkgPath][n.Name] = true
		}
	}
}

// hasFuncRef reports whether a value could hold a function: a literal, or a
// name that may point at one (a function or a package-level value).
func hasFuncRef(vals []ast.Expr) bool {
	for _, v := range vals {
		if isFuncRef(v) {
			return true
		}
	}
	return false
}

func isFuncRef(e ast.Expr) bool {
	switch e.(type) {
	case *ast.FuncLit, *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
		return true
	}
	return false
}

// isFuncType reports whether the written type is a function type: a literal
// func type or a named type this module declares as one.
func (m *Module) isFuncType(file *FileCtx, e ast.Expr) bool {
	if _, ok := e.(*ast.FuncType); ok {
		return true
	}
	pkg, name := typeRef(file, typeString(e))
	return name != "" && m.FuncTypes[pkg+"|"+name]
}

func (m *Module) hasPkg(p string) bool { return m.PKG[p] != nil }

// inModule reports whether a package has source in this module. Types is seeded
// for every parsed package, so type-only packages are in-module too.
func (m *Module) inModule(p string) bool { return m.Types[p] != nil }

// fieldRefs resolves a parameter or result list through the file's imports,
// exactly like struct fields.
func (m *Module) fieldRefs(fc *FileCtx, fl *ast.FieldList) map[string]FieldRef {
	out := map[string]FieldRef{}
	if fl == nil {
		return out
	}
	for _, fld := range fl.List {
		ref := m.typeRefOf(fc, fld.Type)
		if ref.Name == "" {
			continue
		}
		for _, n := range fld.Names {
			out[n.Name] = ref
		}
	}
	return out
}

// indexPackageVars records package var/const types that can be read from the
// declaration. Composite literals are read from their written type; a single
// in-module constructor is followed through its result type. Everything else is
// left out rather than guessed.
func (m *Module) indexPackageVars(fc *FileCtx, gd *ast.GenDecl) {
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		ref := FieldRef{}
		if vs.Type != nil {
			ref = m.typeRefOf(fc, vs.Type)
		} else {
			ref, _ = m.valueRef(fc, vs)
		}
		if ref.Name == "" {
			continue
		}
		for _, n := range vs.Names {
			m.Vars[fc.PkgPath+"|"+n.Name] = ref
		}
	}
}

// valueRef reads a value's type from a composite literal or an in-module
// constructor's single result. A call into another package is not followed.
func (m *Module) valueRef(fc *FileCtx, vs *ast.ValueSpec) (FieldRef, bool) {
	if len(vs.Values) != 1 {
		return FieldRef{}, false
	}
	switch v := vs.Values[0].(type) {
	case *ast.CompositeLit:
		if v.Type == nil {
			return FieldRef{}, false
		}
		return m.typeRefOf(fc, v.Type), true
	case *ast.UnaryExpr:
		if v.Op != token.AND {
			return FieldRef{}, false
		}
		if cl, ok := v.X.(*ast.CompositeLit); ok && cl.Type != nil {
			return m.typeRefOf(fc, cl.Type), true
		}
	case *ast.CallExpr:
		if len(vs.Names) != 1 {
			return FieldRef{}, false // one value cannot type several names
		}
		id, ok := unindex(v.Fun).(*ast.Ident)
		if !ok {
			return FieldRef{}, false
		}
		fn := m.PKG[fc.PkgPath][id.Name]
		if fn == nil || fn.Decl == nil || fn.Decl.Type == nil || fn.Decl.Type.Results == nil {
			return FieldRef{}, false
		}
		if len(fn.Decl.Type.Results.List) != 1 {
			return FieldRef{}, false
		}
		ref := m.typeRefOf(fn.File, fn.Decl.Type.Results.List[0].Type)
		return ref, ref.Name != ""
	}
	return FieldRef{}, false
}

// externalPkg marks a type whose qualifier is not a known import alias. The
// written type still names a package, so it is outside this module even when we
// cannot recover its import path (an unaliased package whose name differs from
// the last path element, such as valkey-go imported as valkey).
const externalPkg = "\x00external"

// typeRefOf turns a written type into the FieldRef used by fields, parameters
// and package variables.
func (m *Module) typeRefOf(fc *FileCtx, e ast.Expr) FieldRef {
	if e == nil {
		return FieldRef{}
	}
	text := typeString(e)
	pkg, name := typeRef(fc, text)
	if alias, ok := typeAlias(text); ok {
		if _, imported := fc.Imports[alias]; !imported {
			pkg = externalPkg
		}
	}
	return FieldRef{Pkg: pkg, Name: name, Text: text, Func: m.isFuncType(fc, e)}
}

// typeAlias returns the qualifier of a written type: gorm in *gorm.DB and
// valkey in valkey.Client.
func typeAlias(text string) (string, bool) {
	t := strings.TrimPrefix(text, "*")
	i := strings.LastIndex(t, ".")
	if i < 0 {
		return "", false
	}
	alias := t[:i]
	if j := strings.LastIndexAny(alias, "[]*"); j >= 0 {
		alias = alias[j+1:]
	}
	return alias, alias != ""
}

// recount rebuilds the call diagnostics from the per-call reasons. It runs at
// the end of loading and again after typed resolution, so -typed moves the
// metric instead of leaving a stale count. File diagnostics survive untouched.
func (m *Module) recount() {
	d := m.Diag
	d.Calls, d.Resolved, d.Unresolved = 0, 0, 0
	d.External, d.Convert, d.Dynamic, d.Ambiguous, d.Unknown = 0, 0, 0, 0, 0
	d.Why = map[string]int{}
	d.Unres = nil
	for _, f := range m.Funcs {
		for i := range f.Calls {
			c := &f.Calls[i]
			if c.Target != nil {
				c.Why = "resolved"
			}
			if c.Why == "" {
				c.Why = "unknown"
			}
			d.Calls++
			d.Why[c.Why]++
			switch c.Why {
			case "resolved":
				d.Resolved++
			case "blind":
				d.Unresolved++
				if len(d.Unres) < diagLimit {
					d.Unres = append(d.Unres, UnresolvedCall{
						Func:   fnName(f),
						Expr:   c.Expr,
						File:   f.File.Rel,
						Line:   c.Line,
						Reason: c.Why,
					})
				}
			case "external":
				d.External++
			case "convert":
				d.Convert++
			case "dynamic":
				d.Dynamic++
			case "ambiguous":
				d.Ambiguous++
			case "unknown":
				d.Unknown++
			}
		}
	}
	m.Diag = d
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

// embedDepth bounds the walk over embedded types. Deep embedding is rare, and
// the cap stops a pathological graph from turning a lookup into a full walk.
const embedDepth = 8

// methodOn finds sel on the declared type: declared directly, or promoted
// through embedding. A direct method shadows an embedded one, and an ambiguous
// promotion has no target. It returns nil for interface types: their methods
// live on implementations, not on the type itself.
func (m *Module) methodOn(pkgPath, name, sel string) *Func {
	fn, _, ambiguous := m.findMethod(pkgPath, name, sel)
	if ambiguous {
		return nil
	}
	return fn
}

// typeName keys one declared type for the breadth-first method-set walk.
type typeName struct {
	pkg  string
	name string
}

// findMethod reports whether sel is in the method set of the named type. The
// walk is breadth-first, so a shallower method shadows a deeper one. found is
// true for an interface's own method but fn is nil then: no function backs it.
// ambiguous is true when two different embedded types at the same depth both
// provide sel; Go does not promote an ambiguous name, and neither does this
// analyzer.
func (m *Module) findMethod(pkgPath, name, sel string) (fn *Func, found, ambiguous bool) {
	if name == "" || sel == "" {
		return nil, false, false
	}
	if fn := m.Funcs[fnKey(pkgPath, name, sel)]; fn != nil {
		return fn, true, false
	}
	if m.ifaceHas(pkgPath, name, sel) {
		return nil, true, false
	}
	frontier := []typeName{{pkg: pkgPath, name: name}}
	seen := map[typeName]bool{{pkg: pkgPath, name: name}: true}
	for depth := 0; depth < embedDepth && len(frontier) > 0; depth++ {
		var next []typeName
		nextSet := map[typeName]bool{}
		var hits []*Func
		ifaceHits := 0
		for _, tn := range frontier {
			for _, ref := range m.Embeds[tn.pkg+"|"+tn.name] {
				en := typeName{pkg: ref.Pkg, name: ref.Name}
				if fn := m.Funcs[fnKey(en.pkg, en.name, sel)]; fn != nil {
					hits = append(hits, fn)
				} else if m.ifaceHas(en.pkg, en.name, sel) {
					ifaceHits++
				}
				if !seen[en] && !nextSet[en] {
					nextSet[en] = true
					next = append(next, en)
				}
			}
		}
		switch n := len(hits) + ifaceHits; {
		case n > 1:
			return nil, true, true
		case n == 1:
			if len(hits) == 1 {
				return hits[0], true, false
			}
			return nil, true, false
		}
		for _, tn := range next {
			seen[tn] = true
		}
		frontier = next
	}
	return nil, false, false
}

// ifaceHas reports whether an interface declares sel. Only exported methods are
// indexed, so an unexported one stays outside the walk.
func (m *Module) ifaceHas(pkgPath, name, sel string) bool {
	for _, n := range m.Ifaces[pkgPath+"|"+name] {
		if n == sel {
			return true
		}
	}
	return false
}

// embedRef records an embedded field's type. It keeps only types the index can
// name: a bare in-module type or an imported pkg.Type, with or without a
// pointer. A literal or unresolvable type is skipped rather than guessed.
func embedRef(pkg, name, text string) (FieldRef, bool) {
	if pkg == "" || !plainTypeName(name) {
		return FieldRef{}, false
	}
	return FieldRef{Pkg: pkg, Name: name, Text: text}, true
}

// plainTypeName reports whether name is one identifier and nothing else, so a
// literal such as interface{} is not mistaken for an embeddable type.
func plainTypeName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
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

// ifaceImpl finds the method sel on the one concrete type that implements every
// method of the interface: the receiver an interface call reaches when there is
// exactly one, as with a repository interface and its store. Methods may be
// declared directly or promoted through embedding. Generated stubs are skipped.
// More than one implementation is reported as ambiguous instead of guessed, so
// the caller can say why the call has no target.
func (m *Module) ifaceImpl(pkgPath, name, sel string, self *Func) (*Func, bool) {
	ms, ok := m.Ifaces[pkgPath+"|"+name]
	if !ok {
		return nil, false
	}
	var match *Func
	for _, tn := range m.candidateTypes() {
		// the caller's own method is not an implementation of its field's interface
		if self != nil && tn.pkg == self.PkgPath && tn.name == self.Recv {
			continue
		}
		fn, found, ambiguous := m.findMethod(tn.pkg, tn.name, sel)
		if !found || ambiguous || fn == nil {
			continue
		}
		if !m.declaresAll(tn.pkg, tn.name, ms) {
			continue
		}
		if generatedOnly(m.recvMethods(fn.PkgPath, fn.Recv)) {
			continue
		}
		if match != nil {
			return nil, true // more than one implementation: do not guess
		}
		match = fn
	}
	return match, false
}

// candidateTypes lists every type that could satisfy a non-empty interface: a
// type with declared methods, or one that embeds another type. A type with
// neither has no method set to check.
func (m *Module) candidateTypes() []typeName {
	seen := map[typeName]bool{}
	var out []typeName
	add := func(key string) {
		pkg, name := splitRecvKey(key)
		if pkg == "" || name == "" {
			return
		}
		tn := typeName{pkg: pkg, name: name}
		if seen[tn] {
			return
		}
		seen[tn] = true
		out = append(out, tn)
	}
	for key := range m.Recv {
		add(key)
	}
	for key := range m.Embeds {
		add(key)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].pkg != out[j].pkg {
			return out[i].pkg < out[j].pkg
		}
		return out[i].name < out[j].name
	})
	return out
}

// declaresAll reports whether the type's method set contains every name,
// directly or promoted through embedding. An ambiguous promotion does not count.
func (m *Module) declaresAll(pkgPath, recv string, names []string) bool {
	for _, n := range names {
		if _, found, ambiguous := m.findMethod(pkgPath, recv, n); !found || ambiguous {
			return false
		}
	}
	return true
}

// recvMethods collects every method declared on a type.
func (m *Module) recvMethods(pkgPath, recv string) []*Func {
	return m.Recv[recvKey(pkgPath, recv)]
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

// recvKey keys a receiver's methods by package and type, so two packages that
// both declare type Service keep their methods apart.
func recvKey(pkg, recv string) string { return pkg + "|" + recv }

func splitRecvKey(key string) (pkg, recv string) {
	if i := strings.LastIndexByte(key, '|'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
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
