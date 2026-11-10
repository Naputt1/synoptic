package main

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// JService is one gRPC service as registered in the source. It is a separate
// surface from the HTTP router map: services and methods, not paths.
type JService struct {
	N       string           `json:"n"` // service name, e.g. jobpb.JobService
	P       string           `json:"p,omitempty"`
	F       string           `json:"f,omitempty"`
	L       int              `json:"l,omitempty"`
	D       string           `json:"d,omitempty"`
	Svc     string           `json:"svc,omitempty"`
	Mw      []JStage         `json:"mw,omitempty"`
	Methods []JServiceMethod `json:"methods"`
}

type JServiceMethod struct {
	N    string `json:"n"`
	P    string `json:"p"` // /Service/Method
	K    string `json:"k"` // unary, server stream, client stream, bidi
	Rq   string `json:"rq,omitempty"`
	Rs   string `json:"rs,omitempty"`
	D    string `json:"d,omitempty"`
	F    string `json:"f,omitempty"`
	L    int    `json:"l,omitempty"`
	Fk   string `json:"fk,omitempty"`
	Curl string `json:"curl,omitempty"`

	fn *Func
}

type grpcMethod struct {
	Name string
	Kind string // unary, server stream, client stream, bidi
}

type grpcDesc struct {
	Service string
	Methods []grpcMethod
}

type ifaceMethod struct {
	Name string
	Req  string
	Res  string
	Kind string
}

type grpcIndex struct {
	m    *Module
	ix   *inputIndex
	scan *protocolScan

	descs  map[string]map[string]grpcDesc
	ifaces map[string]map[string][]ifaceMethod
	mw     []JStage
}

var grpcIndexes = map[*Module][]JService{}

func buildServices(m *Module) []JService {
	if services, ok := grpcIndexes[m]; ok {
		return services
	}
	g := &grpcIndex{
		m:      m,
		ix:     inputsIndex(m),
		descs:  map[string]map[string]grpcDesc{},
		ifaces: map[string]map[string][]ifaceMethod{},
	}
	g.scan = &protocolScan{idx: streamsIndex(m), localTypes: map[string]string{}, localSrc: map[string]ast.Expr{}}
	g.indexTypes()
	g.mw = g.interceptors()

	byName := map[string]*JService{}
	var order []string
	for _, fn := range m.Funcs {
		if fn.Decl == nil || fn.Decl.Body == nil {
			continue
		}
		ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, _, _ := callParts(ce)
			if !strings.HasPrefix(sel, "Register") || !strings.HasSuffix(sel, "Server") || len(ce.Args) < 2 {
				return true
			}
			target := g.scan.target(&protoScope{fn: fn}, ce)
			if target == nil || target.Decl == nil || target.Decl.Type == nil || target.Decl.Type.Params == nil {
				return true
			}
			params := target.Decl.Type.Params.List
			if len(params) < 2 {
				return true
			}
			registrar := typeString(params[0].Type)
			if !strings.Contains(registrar, "ServiceRegistrar") && !strings.Contains(registrar, "grpc.Server") && !strings.Contains(registrar, "Server") {
				return true
			}
			iface := shortTypeText(typeString(params[1].Type))
			descKey := strings.TrimSuffix(iface, "Server")
			sigs := lookupIface(g.ifaces, target.PkgPath, iface)
			desc, ok := lookupDesc(g.descs, target.PkgPath, descKey)
			if !ok {
				// A missing generated descriptor is still useful when the
				// service interface itself is in the module.
				desc = grpcDesc{Service: descKey}
				for _, s := range sigs {
					desc.Methods = append(desc.Methods, grpcMethod{Name: s.Name, Kind: s.Kind})
				}
			}
			name := desc.Service
			if name == "" {
				name = descKey
			}
			if name == "" || iface == "" || len(desc.Methods) == 0 {
				return true
			}
			impl := g.exprType(fn, ce.Args[1])
			implMethods := g.methodsOf(impl)
			if len(implMethods) == 0 {
				// Factories usually return the service interface, not the
				// concrete type, so match the implementation structurally.
				if typ := g.implByIface(sigs); typ != "" {
					impl = typ
					implMethods = g.methodsOf(typ)
				}
			}

			svc := byName[name]
			if svc == nil {
				svc = &JService{
					N:  name,
					P:  altPkg(target.PkgPath),
					F:  fn.File.Rel,
					L:  line(ce.Pos()),
					Mw: g.mw,
				}
				byName[name] = svc
				order = append(order, name)
			}
			if len(svc.Methods) == 0 {
				svc.Methods = g.methods(desc, sigs, implMethods, name)
			}
			return true
		})
	}

	out := make([]JService, 0, len(order))
	for _, name := range order {
		svc := byName[name]
		sort.SliceStable(svc.Methods, func(i, j int) bool { return svc.Methods[i].N < svc.Methods[j].N })
		out = append(out, *svc)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].N < out[j].N })
	grpcIndexes[m] = out
	return out
}

func (g *grpcIndex) methods(desc grpcDesc, sigs []ifaceMethod, impl map[string]*Func, service string) []JServiceMethod {
	sig := map[string]ifaceMethod{}
	for _, s := range sigs {
		sig[s.Name] = s
	}
	var out []JServiceMethod
	for _, dm := range desc.Methods {
		m := JServiceMethod{N: dm.Name, K: dm.Kind}
		if m.K == "" {
			m.K = "unary"
		}
		if s, ok := sig[dm.Name]; ok {
			m.Rq, m.Rs = s.Req, s.Res
			if dm.Kind == "" || dm.Kind == "unary" {
				if s.Kind != "" {
					m.K = s.Kind
				}
			}
		}
		m.P = "/" + service + "/" + dm.Name
		m.Curl = grpcurlExample(service, dm.Name)
		if fn := impl[dm.Name]; fn != nil {
			m.fn = fn
			m.Fk = fn.Key
			m.D = displayDoc(fn)
			m.F = fn.File.Rel
			m.L = fn.Start
		}
		out = append(out, m)
	}
	return out
}

func grpcurlExample(service, method string) string {
	return "grpcurl -plaintext -d '{}' localhost:50051 " + service + "/" + method
}

func lookupDesc(all map[string]map[string]grpcDesc, pkg, key string) (grpcDesc, bool) {
	if d, ok := all[pkg][key]; ok {
		return d, true
	}
	for _, byKey := range all {
		if d, ok := byKey[key]; ok {
			return d, true
		}
	}
	return grpcDesc{}, false
}

func lookupIface(all map[string]map[string][]ifaceMethod, pkg, name string) []ifaceMethod {
	if m, ok := all[pkg][name]; ok {
		return m
	}
	for _, byName := range all {
		if m, ok := byName[name]; ok {
			return m
		}
	}
	return nil
}

func (g *grpcIndex) methodsOf(typ string) map[string]*Func {
	out := map[string]*Func{}
	typ = shortTypeText(typ)
	if typ == "" {
		return out
	}
	for _, fn := range g.m.ByRecv[typ] {
		out[fn.Name] = fn
	}
	for name, list := range g.m.Methods {
		for _, fn := range list {
			if strings.TrimPrefix(fn.Recv, "*") == typ {
				out[name] = fn
			}
		}
	}
	return out
}

// implByIface finds the concrete type registered for a service when the
// registration expression only names the interface. Exported interface
// methods must all be present; generated stubs and the Unimplemented embed are
// skipped because they match every service by name alone.
func (g *grpcIndex) implByIface(sigs []ifaceMethod) string {
	want := map[string]bool{}
	for _, s := range sigs {
		if s.Name == "" || s.Name[0] < 'A' || s.Name[0] > 'Z' {
			continue
		}
		want[s.Name] = true
	}
	if len(want) == 0 {
		return ""
	}
	best, bestExtra := "", -1
	for typ, fns := range g.m.ByRecv {
		if strings.HasPrefix(typ, "Unimplemented") || generatedOnly(fns) {
			continue
		}
		have := map[string]bool{}
		for _, f := range fns {
			have[f.Name] = true
		}
		missing := false
		for name := range want {
			if !have[name] {
				missing = true
				break
			}
		}
		if missing {
			continue
		}
		extra := len(fns) - len(want)
		if bestExtra < 0 || extra < bestExtra || (extra == bestExtra && typ < best) {
			best, bestExtra = typ, extra
		}
	}
	return best
}

// generatedOnly reports whether every method on the receiver came from a
// generated protobuf file, which is how client stubs look.
func generatedOnly(fns []*Func) bool {
	if len(fns) == 0 {
		return true
	}
	for _, f := range fns {
		if f.File == nil || !strings.HasSuffix(f.File.Rel, ".pb.go") {
			return false
		}
	}
	return true
}

func (g *grpcIndex) exprType(fn *Func, e ast.Expr) string {
	e = unwrapExpr(e)
	switch x := e.(type) {
	case *ast.CompositeLit:
		return typeString(x.Type)
	case *ast.Ident:
		if t := g.scan.paramType(fn, x.Name); t != "" {
			return t
		}
		c := &inputCollector{m: g.m, ix: g.ix}
		if t := c.varType(fn, x.Name); t != "" {
			return t
		}
	}
	return g.scan.typeIn(&protoScope{fn: fn}, e)
}

func (g *grpcIndex) indexTypes() {
	for _, f := range g.m.Files {
		pkg := f.PkgPath
		for _, d := range f.Ast.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			switch gd.Tok {
			case token.TYPE:
				for _, sp := range gd.Specs {
					ts, ok := sp.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if it, ok := ts.Type.(*ast.InterfaceType); ok {
						if g.ifaces[pkg] == nil {
							g.ifaces[pkg] = map[string][]ifaceMethod{}
						}
						g.ifaces[pkg][ts.Name.Name] = parseIface(it)
					}
				}
			case token.VAR:
				for _, sp := range gd.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if !strings.HasSuffix(name.Name, "_ServiceDesc") || i >= len(vs.Values) {
							continue
						}
						lit := compositeLit(vs.Values[i])
						if lit == nil {
							continue
						}
						key := strings.TrimSuffix(name.Name, "_ServiceDesc")
						desc := parseDesc(f, g.ix, lit, key)
						if g.descs[pkg] == nil {
							g.descs[pkg] = map[string]grpcDesc{}
						}
						g.descs[pkg][key] = desc
					}
				}
			}
		}
	}
}

func parseDesc(f *FileCtx, ix *inputIndex, lit *ast.CompositeLit, key string) grpcDesc {
	d := grpcDesc{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		switch identName(kv.Key) {
		case "ServiceName":
			if v, ok := ix.evalString(f, kv.Value); ok {
				d.Service = v
			}
		case "Methods":
			d.Methods = append(d.Methods, parseDescMethods(kv.Value, false)...)
		case "Streams":
			d.Methods = append(d.Methods, parseDescMethods(kv.Value, true)...)
		}
	}
	if d.Service == "" {
		d.Service = key
	}
	sort.SliceStable(d.Methods, func(i, j int) bool { return d.Methods[i].Name < d.Methods[j].Name })
	return d
}

func stringLiteral(e ast.Expr) string {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return ""
	}
	v, _ := strconv.Unquote(bl.Value)
	return v
}

func parseDescMethods(e ast.Expr, stream bool) []grpcMethod {
	lit := compositeLit(e)
	if lit == nil {
		return nil
	}
	var out []grpcMethod
	for _, elt := range lit.Elts {
		item := compositeLit(elt)
		if item == nil {
			continue
		}
		name := ""
		kind := "unary"
		server, client := false, false
		for _, f := range item.Elts {
			kv, ok := f.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			switch identName(kv.Key) {
			case "MethodName", "StreamName":
				name = stringLiteral(kv.Value)
			case "ServerStreams":
				server = identName(kv.Value) == "true"
			case "ClientStreams":
				client = identName(kv.Value) == "true"
			}
		}
		if name == "" {
			continue
		}
		if stream {
			switch {
			case server && client:
				kind = "bidi"
			case server:
				kind = "server stream"
			case client:
				kind = "client stream"
			}
		}
		out = append(out, grpcMethod{Name: name, Kind: kind})
	}
	return out
}

func parseIface(it *ast.InterfaceType) []ifaceMethod {
	if it == nil || it.Methods == nil {
		return nil
	}
	var out []ifaceMethod
	for _, f := range it.Methods.List {
		ft, ok := f.Type.(*ast.FuncType)
		if !ok || len(f.Names) == 0 {
			continue
		}
		im := ifaceMethod{Name: f.Names[0].Name}
		im.Req, im.Res, im.Kind = ifaceSig(ft)
		out = append(out, im)
	}
	return out
}

func ifaceSig(ft *ast.FuncType) (req, res, kind string) {
	var params []ast.Expr
	if ft.Params != nil {
		for _, p := range ft.Params.List {
			params = append(params, p.Type)
		}
	}
	var results []ast.Expr
	if ft.Results != nil {
		for _, r := range ft.Results.List {
			results = append(results, r.Type)
		}
	}
	for _, p := range params {
		t := typeString(p)
		lower := strings.ToLower(t)
		if strings.Contains(lower, "context.context") {
			continue
		}
		if strings.Contains(t, "ClientStreamingServer") || strings.Contains(t, "BidiStreamingServer") {
			if g := genericTypes(p); len(g) >= 2 {
				req, res = cleanIfaceType(g[0]), cleanIfaceType(g[1])
			}
			if strings.Contains(t, "BidiStreamingServer") {
				kind = "bidi"
			} else {
				kind = "client stream"
			}
			return req, res, kind
		}
		if strings.Contains(t, "ServerStreamingServer") {
			req = cleanIfaceType(firstParam(params))
			if g := genericTypes(p); len(g) >= 1 {
				res = cleanIfaceType(g[0])
			}
			return req, res, "server stream"
		}
		if strings.Contains(t, "Server") && strings.HasSuffix(t, "Server") {
			// Generated streaming helper streams such as X_Service_MethodServer.
			if strings.Contains(t, "Stream") {
				req = cleanIfaceType(firstParam(params))
				return req, "", "server stream"
			}
		}
	}
	req = cleanIfaceType(firstParam(params))
	for _, r := range results {
		t := typeString(r)
		if t == "" || t == "error" {
			continue
		}
		res = cleanIfaceType(r)
		break
	}
	return req, res, "unary"
}

func firstParam(params []ast.Expr) ast.Expr {
	for _, p := range params {
		if !strings.Contains(strings.ToLower(typeString(p)), "context.context") {
			return p
		}
	}
	return nil
}

func genericTypes(e ast.Expr) []ast.Expr {
	switch x := e.(type) {
	case *ast.IndexExpr:
		return []ast.Expr{x.Index}
	case *ast.IndexListExpr:
		return x.Indices
	}
	return nil
}

func cleanIfaceType(e ast.Expr) string {
	if e == nil {
		return ""
	}
	t := strings.TrimSpace(typeString(e))
	t = strings.TrimPrefix(t, "*")
	t = strings.TrimPrefix(t, "...")
	if t == "context.Context" || t == "error" {
		return ""
	}
	return t
}

func (g *grpcIndex) interceptors() []JStage {
	seen := map[string]bool{}
	var out []JStage
	add := func(e ast.Expr, refFn *Func) {
		name := exprText(e)
		if refFn != nil {
			name = fnName(refFn)
		}
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		st := JStage{Name: name, Kind: "func", Role: "middleware"}
		if refFn != nil {
			st.K = refFn.Key
			st.Line = refFn.Start
			if refFn.File != nil {
				st.File = refFn.File.Rel
			}
			st.Pkg = altPkg(refFn.PkgPath)
		}
		out = append(out, st)
	}
	var walk func(fn *Func, e ast.Expr)
	walk = func(fn *Func, e ast.Expr) {
		e = unwrapExpr(e)
		switch x := e.(type) {
		case *ast.CallExpr:
			sel, base, _ := callParts(x)
			if strings.Contains(sel, "Interceptor") || strings.Contains(sel, "Chain") {
				for _, a := range x.Args {
					if rf := g.refFn(fn, a); rf != nil {
						add(a, rf)
					} else {
						add(a, nil)
					}
				}
				return
			}
			_ = base
			for _, a := range x.Args {
				walk(fn, a)
			}
		}
	}
	for _, fn := range g.m.Funcs {
		if fn.Decl == nil || fn.Decl.Body == nil {
			continue
		}
		ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, base, _ := callParts(ce)
			if sel != "NewServer" || !isGRPCImport(fn, base) {
				return true
			}
			for _, a := range ce.Args {
				walk(fn, a)
			}
			return true
		})
	}
	return out
}

func isGRPCImport(fn *Func, base string) bool {
	if base == "grpc" {
		return true
	}
	return fn != nil && fn.File != nil && fn.File.Imports[base] == "google.golang.org/grpc"
}

func (g *grpcIndex) refFn(fn *Func, e ast.Expr) *Func {
	e = unwrapExpr(e)
	switch x := e.(type) {
	case *ast.Ident:
		return g.m.PKG[fn.PkgPath][x.Name]
	case *ast.SelectorExpr:
		if base := identName(x.X); base != "" {
			if pkg, ok := fn.File.Imports[base]; ok {
				return g.m.PKG[pkg][x.Sel.Name]
			}
		}
	case *ast.CallExpr:
		if rf := g.refFn(fn, x.Fun); rf != nil {
			return rf
		}
		for _, a := range x.Args {
			if rf := g.refFn(fn, a); rf != nil {
				return rf
			}
		}
	}
	return nil
}

func attachServices(m *Module, d *JData, services []JService) {
	if len(services) == 0 {
		pruneSvcs(d)
		return
	}
	d.Services = services
	d.Stats.Services = len(services)
	if len(d.Svcs) > 0 {
		orphan := false
		for si := range d.Services {
			s := svcForFile(d.Svcs, d.Services[si].F)
			if s == "" {
				orphan = true
			}
			d.Services[si].Svc = s
		}
		if orphan {
			d.Svcs = nil
			for si := range d.Services {
				d.Services[si].Svc = ""
			}
			for i := range d.Routes {
				d.Routes[i].Svc = ""
			}
		} else {
			at := map[string]int{}
			for i := range d.Svcs {
				at[d.Svcs[i].N] = i
			}
			for si := range d.Services {
				if i, ok := at[d.Services[si].Svc]; ok {
					d.Svcs[i].RPCs += len(d.Services[si].Methods)
				}
			}
		}
	}
	fkRPC := map[string]string{}
	ifaceRPC := map[string]string{}
	for si := range d.Services {
		short := shortServiceName(d.Services[si].N)
		d.Stats.RPCs += len(d.Services[si].Methods)
		for mi := range d.Services[si].Methods {
			md := d.Services[si].Methods[mi]
			ref := strconv.Itoa(si) + "/" + strconv.Itoa(mi)
			ifaceRPC[short+"."+md.N] = ref
			if md.Fk != "" {
				fkRPC[md.Fk] = ref
			}
			if fn := md.fn; fn != nil {
				indexFunc(m, d.Flows, fn, 0)
			}
		}
	}
	if d.Types == nil {
		d.Types = buildTypeCatalog(m)
	}
	for key, jf := range d.Flows {
		annotateSteps(jf.Steps, d.Types, m)
		linkRPSSteps(jf.Steps, ifaceRPC)
		if ref, ok := fkRPC[key]; ok {
			jf.Rpc = ref
		}
		d.Flows[key] = jf
	}
	d.Stats.Funcs = len(d.Flows)
	pruneSvcs(d)
}

// pruneSvcs drops entry points that own neither a route nor an RPC, so a cmd
// directory that only starts a queue worker does not show as an empty surface.
func pruneSvcs(d *JData) {
	if len(d.Svcs) == 0 {
		return
	}
	kept := d.Svcs[:0]
	for _, s := range d.Svcs {
		if s.Routes+s.RPCs > 0 {
			kept = append(kept, s)
		}
	}
	d.Svcs = kept
}

// shortServiceName turns jobpb.JobService into JobService, to match the
// generated client interface a handler holds: jobpb.JobServiceClient.
func shortServiceName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// linkRPSSteps points calls made through a generated gRPC client interface at
// the service method, so an HTTP flow can cross into the RPC surface.
func linkRPSSteps(steps []Step, idx map[string]string) {
	for i := range steps {
		st := &steps[i]
		if st.Iface != "" {
			name := strings.TrimPrefix(st.Iface, "*")
			if k := strings.LastIndex(name, "."); k >= 0 {
				name = name[k+1:]
			}
			if strings.HasSuffix(name, "Client") {
				if ref, ok := idx[strings.TrimSuffix(name, "Client")+"."+st.Sel]; ok {
					st.Rpc = ref
				}
			}
		}
		linkRPSSteps(st.Body, idx)
		linkRPSSteps(st.Else, idx)
	}
}
