package main

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// JEvidence is one place in the source that proves a transport: an upgrade
// call, a content-type write, an event line, or a flush.
type JEvidence struct {
	F string `json:"f"`
	L int    `json:"l"`
	N string `json:"n,omitempty"`
}

// JMessage is one frame shape on a stream. Direction is send or recv. Type is
// the named type when the reader could resolve one; Expr is the source
// expression when it could not.
type JMessage struct {
	D    string   `json:"d"` // send, recv
	N    string   `json:"n,omitempty"`
	T    string   `json:"t,omitempty"`
	F    []JField `json:"f,omitempty"`
	E    string   `json:"e,omitempty"`
	Ex   string   `json:"ex,omitempty"`
	Ev   string   `json:"ev,omitempty"` // event this frame belongs to
	File string   `json:"file,omitempty"`
	L    int      `json:"l,omitempty"`
}

// JStream is the transport a route really speaks. It is only attached when the
// source proves an upgrade or an event-stream response.
// JExt is one external call reached by a stream handler: a database,
// cache, or network operation behind a message.
type JExt struct {
	G  string `json:"g"` // db, cache, net
	N  string `json:"n"`
	Fk string `json:"fk,omitempty"`
	F  string `json:"f,omitempty"`
	L  int    `json:"l,omitempty"`
	Ev string `json:"ev,omitempty"` // event whose handler made the call
}

type JStream struct {
	K      string      `json:"k"` // ws, sse
	Ev     []JEvidence `json:"ev,omitempty"`
	Events []string    `json:"events,omitempty"`
	Evs    []JEvent    `json:"evs,omitempty"`
	Hk     string      `json:"hk,omitempty"`
	Send   []JMessage  `json:"send,omitempty"`
	Recv   []JMessage  `json:"recv,omitempty"`
	Ext    []JExt      `json:"ext,omitempty"`
	SP     []string    `json:"sp,omitempty"`
	OC     bool        `json:"oc,omitempty"`
	HB     string      `json:"hb,omitempty"`
	RT     string      `json:"rt,omitempty"`
}

type protocolIndex struct {
	m           *Module
	ix          *inputIndex
	upgrader    map[string]map[string]bool // pkg -> variable holding websocket.Upgrader
	origin      map[string]map[string]bool // pkg -> variable with CheckOrigin
	eventConsts map[string][]string        // named string type -> constant values
	pkgEvents   map[string][]string        // package -> likely event/message constants
	fieldEvents []string                   // constants on fields named Type
	typePkg     map[string]string          // named type -> package
}

var protocolIndexes = map[*Module]*protocolIndex{}

func streamsIndex(m *Module) *protocolIndex {
	if p := protocolIndexes[m]; p != nil {
		return p
	}
	p := &protocolIndex{
		m:           m,
		ix:          inputsIndex(m),
		upgrader:    map[string]map[string]bool{},
		origin:      map[string]map[string]bool{},
		eventConsts: map[string][]string{},
		pkgEvents:   map[string][]string{},
		typePkg:     map[string]string{},
	}
	addVar := func(pkg, name, typ string, val ast.Expr) {
		if name == "" || !strings.Contains(typ, "Upgrader") {
			return
		}
		if p.upgrader[pkg] == nil {
			p.upgrader[pkg] = map[string]bool{}
		}
		p.upgrader[pkg][name] = true
		if val != nil && hasCompositeField(val, "CheckOrigin") {
			if p.origin[pkg] == nil {
				p.origin[pkg] = map[string]bool{}
			}
			p.origin[pkg][name] = true
		}
	}
	addEventValue := func(typ, value string) {
		if value == "" || strings.Contains(value, "%") {
			return
		}
		p.eventConsts[typ] = addUnique(p.eventConsts[typ], value)
	}

	for _, f := range m.Files {
		pkg := f.PkgPath
		ast.Inspect(f.Ast, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ValueSpec:
				for i, name := range x.Names {
					typ := ""
					if x.Type != nil {
						typ = typeString(x.Type)
					}
					var val ast.Expr
					if i < len(x.Values) {
						val = x.Values[i]
						if typ == "" {
							typ = typeNameOf(val)
						}
					}
					addVar(pkg, name.Name, typ, val)
				}
			case *ast.AssignStmt:
				for i, lhs := range x.Lhs {
					if i >= len(x.Rhs) {
						break
					}
					if sel, ok := lhs.(*ast.SelectorExpr); ok && (sel.Sel.Name == "CheckOrigin" || sel.Sel.Name == "OriginPatterns") {
						if p.origin[pkg] == nil {
							p.origin[pkg] = map[string]bool{}
						}
						if name := identName(sel.X); name != "" {
							p.origin[pkg][name] = true
						}
					}
					id, ok := lhs.(*ast.Ident)
					if !ok {
						continue
					}
					typ := typeNameOf(x.Rhs[i])
					addVar(pkg, id.Name, typ, x.Rhs[i])
				}
			}
			return true
		})

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
					p.typePkg[ts.Name.Name] = pkg
					st, ok := ts.Type.(*ast.StructType)
					if !ok || st.Fields == nil {
						continue
					}
					for _, fld := range st.Fields.List {
						for _, name := range fld.Names {
							if name.Name != "Type" {
								continue
							}
							ft := shortTypeName(fld.Type)
							if ft == "" {
								continue
							}
							for _, v := range p.eventConsts[ft] {
								p.fieldEvents = addUnique(p.fieldEvents, v)
							}
						}
					}
				}
			case token.CONST:
				var prev []ast.Expr
				for _, sp := range gd.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok {
						continue
					}
					vals := vs.Values
					if len(vals) == 0 {
						vals = prev
					}
					prev = vals
					typ := ""
					if vs.Type != nil {
						typ = shortTypeName(vs.Type)
					}
					for i, name := range vs.Names {
						if i >= len(vals) {
							continue
						}
						v, ok := p.ix.evalString(f, vals[i])
						if !ok {
							continue
						}
						if typ != "" {
							addEventValue(typ, v)
							continue
						}
						if looksEventName(name.Name) {
							p.pkgEvents[pkg] = addUnique(p.pkgEvents[pkg], v)
						}
					}
				}
			}
		}
	}

	// Untyped constants in a package belong to its named string types. This is
	// how a block such as MSG_TYPE_CONNECT = "connect" joins MessageType.
	for typ, pkg := range p.typePkg {
		if len(p.eventConsts[typ]) == 0 && len(p.pkgEvents[pkg]) > 0 {
			p.eventConsts[typ] = append([]string(nil), p.pkgEvents[pkg]...)
		}
	}

	sort.Strings(p.fieldEvents)
	for typ := range p.eventConsts {
		sort.Strings(p.eventConsts[typ])
	}
	protocolIndexes[m] = p
	return p
}

func addUnique(list []string, v string) []string {
	if v == "" {
		return list
	}
	for _, old := range list {
		if old == v {
			return list
		}
	}
	return append(list, v)
}

func looksEventName(name string) bool {
	u := strings.ToUpper(name)
	return strings.HasPrefix(u, "EVENT") || strings.HasPrefix(u, "MSG") || strings.Contains(u, "EVENT")
}

func typeNameOf(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.CompositeLit:
		return typeString(x.Type)
	case *ast.UnaryExpr:
		return typeNameOf(x.X)
	case *ast.ParenExpr:
		return typeNameOf(x.X)
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewUpgrader" {
			return "websocket.Upgrader"
		}
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "NewUpgrader" {
			return "websocket.Upgrader"
		}
	}
	return ""
}

func hasCompositeField(e ast.Expr, field string) bool {
	lit := compositeLit(e)
	if lit == nil {
		return false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if ok && identName(kv.Key) == field {
			return true
		}
	}
	return false
}

type protoScope struct {
	fn     *Func
	parent *protoScope
	bind   map[string]ast.Expr
}

type protocolScan struct {
	idx *protocolIndex

	scans int
	seen  map[string]bool

	localTypes map[string]string
	localSrc   map[string]ast.Expr

	wsStrong  bool
	sseStrong bool
	wsEv      []JEvidence
	sseEv     []JEvidence

	contentType bool
	flush       bool
	streamCall  bool
	eventLines  bool
	dataLines   bool
	sseEvent    bool

	origin       bool
	subprotocols []string
	heartbeat    string
	retry        string
	ping         bool

	events      []string
	event       string
	schemaEvent string
	eventDefs   map[string]*JEvent
	send        []JMessage
	recv        []JMessage
	ext         []JExt
}

// classifyStream reads the route chain and the in-repo helpers it calls. It
// returns nil for an ordinary HTTP route; a WS upgrade outranks an SSE hint.
func classifyStream(m *Module, r *Route) *JStream {
	if r == nil || r.Stream != nil {
		return r.Stream
	}
	p := &protocolScan{
		idx:        streamsIndex(m),
		seen:       map[string]bool{},
		localTypes: map[string]string{},
		localSrc:   map[string]ast.Expr{},
		eventDefs:  map[string]*JEvent{},
	}
	for _, ref := range r.Chain {
		if ref.Fn != nil {
			p.walk(&protoScope{fn: ref.Fn}, 0)
		}
	}
	if r.Entry != nil {
		p.walk(&protoScope{fn: r.Entry}, 0)
	}
	return p.finish()
}

func (p *protocolScan) walk(scope *protoScope, depth int) {
	if scope == nil || scope.fn == nil || scope.fn.Decl == nil || scope.fn.Decl.Body == nil {
		return
	}
	fn := scope.fn
	if depth > 3 || p.seen[fn.Key] || p.scans > 700 {
		return
	}
	p.seen[fn.Key] = true
	p.scans++
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			p.assign(scope, x.Lhs, x.Rhs)
		case *ast.ValueSpec:
			p.valueSpec(scope, x)
		case *ast.RangeStmt:
			p.rangeStmt(scope, x)
		case *ast.CallExpr:
			p.call(scope, x, depth)
		case *ast.SwitchStmt:
			p.messageSwitch(scope, x)
		}
		return true
	})
}

// messageSwitch turns a switch over a message/event discriminator into one
// received frame per case. Frameworks that dispatch through an interface
// (OnMessage -> switch msgType -> handler) otherwise leave the route with no
// inbound shapes at all.
func (p *protocolScan) messageSwitch(scope *protoScope, x *ast.SwitchStmt) {
	if x.Tag == nil || x.Body == nil {
		return
	}
	tag := strings.ToLower(keyOf(unwrapExpr(x.Tag)))
	if !strings.Contains(tag, "type") && !strings.Contains(tag, "event") && !strings.Contains(tag, "kind") {
		return
	}
	for _, s := range x.Body.List {
		cc, ok := s.(*ast.CaseClause)
		if !ok || len(cc.List) == 0 {
			continue
		}
		handler := p.caseHandler(scope, cc.Body)
		typ := ""
		if handler != nil {
			typ = cleanStreamType(lastParamType(handler))
		}
		for _, e := range cc.List {
			names := p.eventNames(scope, e)
			if len(names) == 0 {
				if name := exprText(e); name != "" && !strings.Contains(name, ".") {
					names = []string{name}
				}
			}
			for _, name := range names {
				p.addEvent(name)
				p.addEventDef(name, handler, scope.fn.File, line(e.Pos()))
				prev := p.event
				p.event = name
				p.addMsg("recv", name, typ, nil, "", "", scope.fn.File, line(e.Pos()))
				if handler != nil {
					p.walk(&protoScope{fn: handler, parent: scope}, 2)
				}
				p.event = prev
			}
		}
	}
}

// caseHandler reads the concrete handler passed in a switch case:
// handle(c, data, h.Query) -> Handler.Query. Its last parameter is the
// decoded request type.
func (p *protocolScan) caseHandler(scope *protoScope, body []ast.Stmt) *Func {
	var out *Func
	for _, s := range body {
		ast.Inspect(s, func(n ast.Node) bool {
			if out != nil {
				return false
			}
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for _, arg := range ce.Args {
				if fn := p.funcValue(scope, arg); fn != nil && fn != scope.fn {
					out = fn
					return false
				}
			}
			return true
		})
	}
	return out
}

func (p *protocolScan) funcValue(scope *protoScope, e ast.Expr) *Func {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		if base := identName(x.X); base != "" {
			if pkg, ok := scope.fn.File.Imports[base]; ok {
				return p.idx.m.PKG[pkg][x.Sel.Name]
			}
		}
		typ := strings.TrimPrefix(strings.TrimSpace(p.typeIn(scope, x.X)), "*")
		if base := identName(x.X); base != "" && base == scope.fn.RecvVar {
			typ = scope.fn.Recv
		}
		if i := strings.LastIndex(typ, "."); i >= 0 {
			typ = typ[i+1:]
		}
		if i := strings.Index(typ, "["); i > 0 {
			typ = typ[:i]
		}
		for _, m := range p.idx.m.Methods[x.Sel.Name] {
			if m.PkgPath == scope.fn.PkgPath && typ != "" && m.Recv == typ {
				return m
			}
		}
	case *ast.Ident:
		return p.idx.m.PKG[scope.fn.PkgPath][x.Name]
	}
	return nil
}

func lastParamType(fn *Func) string {
	if fn == nil || fn.Decl == nil || fn.Decl.Type == nil || fn.Decl.Type.Params == nil {
		return ""
	}
	var typ string
	for _, f := range fn.Decl.Type.Params.List {
		typ = typeString(f.Type)
	}
	return typ
}

func (p *protocolScan) valueSpec(scope *protoScope, x *ast.ValueSpec) {
	fn := scope.fn
	for i, name := range x.Names {
		key := fn.Key + "." + name.Name
		if x.Type != nil {
			p.localTypes[key] = typeString(x.Type)
		}
		if i < len(x.Values) {
			p.localSrc[key] = x.Values[i]
			if x.Type == nil {
				if t := p.typeIn(scope, x.Values[i]); t != "" {
					p.localTypes[key] = t
				}
			}
		}
	}
}

func (p *protocolScan) assign(scope *protoScope, lhs, rhs []ast.Expr) {
	fn := scope.fn
	for i, l := range lhs {
		if i >= len(rhs) {
			break
		}
		id, ok := l.(*ast.Ident)
		if !ok {
			continue
		}
		key := fn.Key + "." + id.Name
		if u, ok := rhs[i].(*ast.UnaryExpr); !ok || u.Op != token.ARROW {
			p.localSrc[key] = rhs[i]
		}
		if t := p.assignType(scope, rhs[i]); t != "" {
			p.localTypes[key] = t
		}
	}
	// _, message, err := conn.ReadMessage() leaves message as the raw frame.
	if len(rhs) == 1 && len(lhs) > 1 {
		if ce, ok := unwrapExpr(rhs[0]).(*ast.CallExpr); ok {
			if sel := callName(ce); sel == "ReadMessage" || sel == "Read" {
				// no extra bookkeeping needed: json.Unmarshal below sees the name
			}
		}
	}
}

func (p *protocolScan) assignType(scope *protoScope, e ast.Expr) string {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.ARROW {
		return channelElem(p.typeIn(scope, u.X))
	}
	if pn, ok := e.(*ast.ParenExpr); ok {
		return p.assignType(scope, pn.X)
	}
	return p.typeIn(scope, e)
}

func (p *protocolScan) rangeStmt(scope *protoScope, x *ast.RangeStmt) {
	elem := channelElem(p.typeIn(scope, x.X))
	if elem == "" {
		return
	}
	for _, e := range []ast.Expr{x.Key, x.Value} {
		if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
			p.localTypes[scope.fn.Key+"."+id.Name] = elem
		}
	}
}

func (p *protocolScan) call(scope *protoScope, ce *ast.CallExpr, depth int) {
	fn := scope.fn
	sel, base, expr := callParts(ce)
	pkg := ""
	if base != "" {
		pkg = fn.File.Imports[base]
	}

	// websocket upgrade
	switch sel {
	case "Upgrade", "Accept":
		if p.isUpgraderCall(scope, ce) {
			p.wsStrong = true
			p.wsEv = appendEvidence(p.wsEv, fn.File, line(ce.Pos()), "websocket upgrade")
			if p.originFor(fn, base, ce) {
				p.origin = true
			}
		}
	case "New":
		if strings.Contains(pkg, "gofiber") || strings.Contains(pkg, "melody") {
			p.wsStrong = true
			p.wsEv = appendEvidence(p.wsEv, fn.File, line(ce.Pos()), "websocket handler")
		}
	case "HandleRequest":
		if strings.Contains(pkg, "melody") {
			p.wsStrong = true
			p.wsEv = appendEvidence(p.wsEv, fn.File, line(ce.Pos()), "melody handler")
		}
	case "Subprotocols":
		for _, a := range ce.Args {
			if v, ok := p.idx.ix.evalString(fn.File, a); ok {
				p.subprotocols = addUnique(p.subprotocols, v)
			}
		}
	case "SetPongHandler", "SetPingHandler":
		p.ping = true
	}
	if strings.Contains(exprText(ce), "PingMessage") || strings.Contains(exprText(ce), "PongMessage") {
		p.ping = true
	}
	if sel == "WriteMessage" || sel == "WriteControl" {
		for _, a := range ce.Args {
			if strings.Contains(exprText(a), "PingMessage") {
				p.ping = true
			}
		}
	}

	// frames
	switch sel {
	case "ReadJSON", "ReadJSONWithOptions", "ReadJson", "ReadMsgpack":
		if len(ce.Args) > 0 {
			p.recvSchema(scope, ce.Args[0])
		}
	case "ReadMessage", "Read":
		// raw bytes; json.Unmarshal below supplies the shape when present
	case "WriteJSON", "WriteJson", "WriteMsgpack":
		if len(ce.Args) > 0 {
			p.sendSchema(scope, ce.Args[0])
		}
	case "WriteMessage":
		if len(ce.Args) > 1 {
			p.sendSchema(scope, ce.Args[1])
		}
	case "Unmarshal":
		if base == "json" || pkg == "encoding/json" {
			if len(ce.Args) > 1 && p.looksLikeFrame(scope, ce.Args[0]) {
				p.recvSchema(scope, ce.Args[1])
			}
		}
	case "Decode":
		if base == "json" || pkg == "encoding/json" {
			if len(ce.Args) > 0 {
				p.recvSchema(scope, ce.Args[0])
			}
		}
	case "Encode":
		if strings.Contains(expr, "NewEncoder") {
			if len(ce.Args) > 0 {
				p.sendSchema(scope, ce.Args[0])
			}
		}
	case "SSEvent", "SSEventWithType":
		p.sseStrong = true
		p.sseEvent = true
		p.sseEv = appendEvidence(p.sseEv, fn.File, line(ce.Pos()), "SSEvent")
		if len(ce.Args) > 0 {
			for _, n := range p.eventNames(scope, ce.Args[0]) {
				p.addEvent(n)
			}
			if len(ce.Args) > 1 {
				typ, fields, ex, raw := p.schema(scope, ce.Args[1])
				p.addMsg("send", p.firstEvent(scope, ce.Args[0]), typ, fields, ex, raw, fn.File, line(ce.Pos()))
			}
		}
	case "Flush":
		p.flush = true
	case "Stream", "SendStream":
		p.streamCall = true
	}
	if strings.Contains(strings.ToLower(sel), "unmarshal") && len(ce.Args) > 1 && p.looksLikeFrame(scope, ce.Args[0]) {
		p.recvSchema(scope, ce.Args[1])
	}
	if g := protocolTag(fn, ce); g != "" {
		fk := ""
		if t := p.target(scope, ce); t != nil {
			fk = t.Key
		}
		p.addExt(g, exprText(ce), fn.File, line(ce.Pos()), fk)
	}
	p.stringEvidence(scope, ce, sel)

	if depth >= 3 {
		return
	}
	// A message dispatcher passes the concrete handler as a function value:
	// handle(c, data, h.Query). Walk those handlers so their WriteMsgpack
	// responses become frames even though the wrapper itself has no target.
	if scope.fn.Name == "OnMessage" || strings.Contains(strings.ToLower(scope.fn.Name), "message") {
		for _, arg := range ce.Args {
			if fn := p.funcValue(scope, arg); fn != nil && fn != scope.fn {
				p.walk(&protoScope{fn: fn, parent: scope}, depth+1)
			}
		}
	}
	if target := p.target(scope, ce); target != nil {
		child := &protoScope{fn: target, parent: scope, bind: map[string]ast.Expr{}}
		params := paramNames(target)
		for i, name := range params {
			if i < len(ce.Args) {
				child.bind[name] = ce.Args[i]
			}
		}
		p.walk(child, depth+1)
	}
}

func (p *protocolScan) isUpgraderCall(scope *protoScope, ce *ast.CallExpr) bool {
	sel, base, expr := callParts(ce)
	if sel != "Upgrade" && sel != "Accept" {
		return false
	}
	if p.idx.upgrader[scope.fn.PkgPath][base] {
		return true
	}
	if strings.Contains(expr, "websocket.Upgrader") {
		return true
	}
	if base != "" {
		pkg := scope.fn.File.Imports[base]
		if strings.Contains(pkg, "websocket") || strings.Contains(pkg, "gobwas/ws") {
			return true
		}
	}
	return false
}

func (p *protocolScan) originFor(fn *Func, base string, ce *ast.CallExpr) bool {
	if p.idx.origin[fn.PkgPath][base] {
		return true
	}
	if strings.Contains(exprText(ce), "CheckOrigin") || strings.Contains(exprText(ce), "OriginPatterns") {
		return true
	}
	return false
}

func (p *protocolScan) addEvent(name string) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "%") || len(name) > 64 {
		return
	}
	p.events = addUnique(p.events, name)
}

// addEventDef remembers the concrete handler behind a switch case so the UI
// can show how that one event is processed, not just the transport dispatcher.
func (p *protocolScan) addEventDef(name string, handler *Func, f *FileCtx, l int) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if p.eventDefs == nil {
		p.eventDefs = map[string]*JEvent{}
	}
	d := p.eventDefs[name]
	if d == nil {
		d = &JEvent{N: name}
		p.eventDefs[name] = d
	}
	if handler == nil {
		if d.L == 0 {
			d.L = l
		}
		if d.F == "" && f != nil {
			d.F = f.Rel
		}
		return
	}
	d.Fk = handler.Key
	d.Fn = fnName(handler)
	d.L = handler.Start
	if handler.File != nil {
		d.F = handler.File.Rel
	}
	d.fn = handler
}

func (p *protocolScan) eventList() []JEvent {
	out := make([]JEvent, 0, len(p.events))
	for _, name := range p.events {
		if d := p.eventDefs[name]; d != nil {
			out = append(out, *d)
			continue
		}
		out = append(out, JEvent{N: name})
	}
	return out
}

func (p *protocolScan) addMsg(dir, name, typ string, fields []JField, expr, example string, f *FileCtx, l int) {
	if typ == "" && expr == "" && example == "" && name == "" {
		return
	}
	key := dir + "|" + typ + "|" + expr + "|" + name + "|" + p.event
	for i := range p.send {
		if dir != "send" {
			break
		}
		if p.send[i].D+"|"+p.send[i].T+"|"+p.send[i].E+"|"+p.send[i].N+"|"+p.send[i].Ev == key {
			if len(p.send[i].F) == 0 && len(fields) > 0 {
				p.send[i].F = fields
			}
			return
		}
	}
	for i := range p.recv {
		if dir != "recv" {
			break
		}
		if p.recv[i].D+"|"+p.recv[i].T+"|"+p.recv[i].E+"|"+p.recv[i].N+"|"+p.recv[i].Ev == key {
			if len(p.recv[i].F) == 0 && len(fields) > 0 {
				p.recv[i].F = fields
			}
			return
		}
	}
	// Prefer a resolved type over the same unresolved expression.
	target := &p.send
	if dir == "recv" {
		target = &p.recv
	}
	for i := range *target {
		old := &(*target)[i]
		if old.D == dir && old.E == expr && old.Ev == p.event && expr != "" && old.T == "" && typ != "" {
			old.T, old.F, old.N = typ, fields, name
			return
		}
	}
	ev := p.event
	if ev == "" && name != "" {
		ev = name
	}
	if ev == "" {
		ev = p.schemaEvent
	}
	if ev == "" {
		ev = eventFromLiteral(example)
	}
	p.schemaEvent = ""
	m := JMessage{D: dir, N: name, T: typ, F: fields, E: expr, Ex: example, Ev: ev, L: l}
	if f != nil {
		m.File = f.Rel
	}
	*target = append(*target, m)
}

func (p *protocolScan) addExt(g, name string, f *FileCtx, l int, fk string) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return
	}
	rel := ""
	if f != nil {
		rel = f.Rel
	}
	for _, x := range p.ext {
		if x.G == g && x.N == name && x.F == rel && x.L == l && x.Ev == p.event {
			return
		}
	}
	if len(p.ext) >= 60 {
		return
	}
	p.ext = append(p.ext, JExt{G: g, N: name, Fk: fk, F: rel, L: l, Ev: p.event})
}

func protocolTag(fn *Func, ce *ast.CallExpr) string {
	sel, base, expr := callParts(ce)
	switch sel {
	case "WriteJSON", "WriteJson", "WriteMsgpack", "WriteMessage", "ReadJSON", "ReadJson", "ReadMsgpack", "ReadMessage", "SSEvent", "SSEventWithType":
		return ""
	}
	if base == "options" || base == "opt" {
		return ""
	}
	g := tagOf(Call{Base: base, Sel: sel, Expr: expr, PkgPath: fn.PkgPath})
	switch g {
	case "db", "cache", "net":
		return g
	}
	return ""
}

func (p *protocolScan) recvSchema(scope *protoScope, e ast.Expr) {
	typ, fields, expr, ex := p.schema(scope, e)
	p.addMsg("recv", "", typ, fields, expr, ex, scope.fn.File, line(e.Pos()))
}

func (p *protocolScan) sendSchema(scope *protoScope, e ast.Expr) {
	typ, fields, expr, ex := p.schema(scope, e)
	p.addMsg("send", "", typ, fields, expr, ex, scope.fn.File, line(e.Pos()))
}

func (p *protocolScan) firstEvent(scope *protoScope, e ast.Expr) string {
	tag := ""
	if v, ok := p.idx.ix.evalString(scope.fn.File, e); ok {
		tag = v
	}
	return tag
}

func (p *protocolScan) looksLikeFrame(scope *protoScope, e ast.Expr) bool {
	name := strings.ToLower(identName(unwrapExpr(e)))
	switch name {
	case "message", "msg", "data", "payload", "frame", "bytes", "b", "raw":
		return true
	}
	expr := strings.ToLower(exprText(e))
	return strings.Contains(expr, "message") || strings.Contains(expr, "payload")
}

func (p *protocolScan) schema(scope *protoScope, e ast.Expr) (string, []JField, string, string) {
	p.schemaEvent = ""
	return p.schemaSeen(scope, e, map[string]bool{})
}

func (p *protocolScan) schemaSeen(scope *protoScope, e ast.Expr, seen map[string]bool) (string, []JField, string, string) {
	e = unwrapExpr(e)
	if e == nil || scope == nil || scope.fn == nil {
		return "", nil, "", ""
	}
	expr := exprText(e)
	if id, ok := e.(*ast.Ident); ok {
		if seen[id.Name] {
			return "", nil, expr, ""
		}
		if scope.bind != nil && scope.parent != nil {
			if be := scope.bind[id.Name]; be != nil {
				next := map[string]bool{}
				for k := range seen {
					next[k] = true
				}
				next[id.Name] = true
				typ, fields, _, ex := p.schemaSeen(scope.parent, be, next)
				if typ != "" || len(fields) > 0 || ex != "" {
					return typ, fields, expr, ex
				}
			}
		}
		if src := p.localSrc[scope.fn.Key+"."+id.Name]; src != nil {
			next := map[string]bool{}
			for k := range seen {
				next[k] = true
			}
			next[id.Name] = true
			typ, fields, _, ex := p.schemaSeen(scope, src, next)
			if typ != "" || len(fields) > 0 || ex != "" {
				return typ, fields, expr, ex
			}
		}
	}
	if ce, ok := e.(*ast.CallExpr); ok {
		switch callName(ce) {
		case "Marshal":
			if len(ce.Args) > 0 {
				return p.schemaSeen(scope, ce.Args[0], seen)
			}
		case "string", "String":
			if len(ce.Args) > 0 {
				return p.schemaSeen(scope, ce.Args[0], seen)
			}
		}
		if at, ok := ce.Fun.(*ast.ArrayType); ok && typeString(at.Elt) == "byte" && len(ce.Args) > 0 {
			return p.schemaSeen(scope, ce.Args[0], seen)
		}
	}
	if lit := compositeLit(e); lit != nil {
		return p.schemaLit(scope, lit, expr)
	}
	typ := p.typeIn(scope, e)
	if typ != "" {
		if st := p.structFor(scope.fn, typ); st != nil {
			return cleanStreamType(typ), streamFields(st), expr, ""
		}
		return cleanStreamType(typ), nil, expr, ""
	}
	return "", nil, expr, ""
}

func (p *protocolScan) schemaLit(scope *protoScope, lit *ast.CompositeLit, expr string) (string, []JField, string, string) {
	typ := ""
	if lit.Type != nil {
		typ = cleanStreamType(typeString(lit.Type))
		if st, ok := lit.Type.(*ast.StructType); ok {
			p.noteLiteralEvent(scope, lit)
			return typ, streamFields(st), expr, exprText(lit)
		}
		if st := p.structFor(scope.fn, typ); st != nil {
			p.noteLiteralEvent(scope, lit)
			return typ, streamFields(st), expr, exprText(lit)
		}
	}
	fields, example := p.literalFields(scope, lit)
	return typ, fields, expr, example
}

func (p *protocolScan) literalFields(scope *protoScope, lit *ast.CompositeLit) ([]JField, string) {
	var out []JField
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key := ""
		if v, ok := p.idx.ix.evalString(scope.fn.File, kv.Key); ok {
			key = v
		}
		if key == "" {
			key = exprText(kv.Key)
		}
		val := unwrapExpr(kv.Value)
		typ := p.typeIn(scope, val)
		if typ == "" {
			typ = literalKind(val)
		}
		if key != "" {
			out = append(out, JField{N: key, T: cleanStreamType(typ)})
		}
		if strings.EqualFold(key, "type") {
			if v, ok := p.idx.ix.evalString(scope.fn.File, val); ok {
				p.addEvent(v)
				p.schemaEvent = v
			}
		}
	}
	if ex := exprText(lit); ex != "" {
		return out, ex
	}
	return out, ""
}

func (p *protocolScan) noteLiteralEvent(scope *protoScope, lit *ast.CompositeLit) {
	if ev := p.literalEvent(scope, lit); ev != "" {
		p.schemaEvent = ev
	}
}

func (p *protocolScan) literalEvent(scope *protoScope, lit *ast.CompositeLit) string {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key := ""
		if v, ok := p.idx.ix.evalString(scope.fn.File, kv.Key); ok {
			key = v
		}
		if key == "" {
			key = exprText(kv.Key)
		}
		if !strings.EqualFold(key, "type") {
			continue
		}
		if v, ok := p.idx.ix.evalString(scope.fn.File, kv.Value); ok {
			p.addEvent(v)
			return v
		}
	}
	return ""
}

// eventFromLiteral reads a literal "type" field from a frame expression, so a
// send outside a switch still lands on the event it announces.
func eventFromLiteral(example string) string {
	compact := strings.ReplaceAll(example, " ", "")
	for _, key := range []string{`"type":"`, `"Type":"`, `Type:"`, `type:"`} {
		i := strings.Index(compact, key)
		if i < 0 {
			continue
		}
		rest := compact[i+len(key):]
		j := strings.IndexByte(rest, '"')
		if j <= 0 {
			continue
		}
		return rest[:j]
	}
	return ""
}

func literalKind(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.STRING:
			return "string"
		case token.INT:
			return "int"
		case token.FLOAT:
			return "float64"
		}
	case *ast.Ident:
		if x.Name == "true" || x.Name == "false" {
			return "bool"
		}
		if x.Name == "nil" {
			return "any"
		}
	case *ast.CompositeLit:
		return cleanStreamType(typeString(x.Type))
	}
	return ""
}

func streamFields(st *ast.StructType) []JField {
	if st == nil || st.Fields == nil {
		return nil
	}
	var out []JField
	for _, f := range st.Fields.List {
		tag := ""
		if f.Tag != nil {
			tag = strings.Trim(f.Tag.Value, "`")
		}
		jsonName := tagValue(tag, "json")
		if jsonName == "-" {
			continue
		}
		typ := exprText(f.Type)
		req := strings.Contains(tag, "required")
		names := make([]string, 0, len(f.Names))
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
		if len(names) == 0 {
			names = []string{jsonName}
			if names[0] == "" {
				names[0] = typ
			}
		}
		for _, n := range names {
			if jsonName != "" && len(names) == 1 {
				n = jsonName
			}
			out = append(out, JField{N: n, T: typ, Tag: tag, Req: req})
			if len(out) >= 24 {
				return out
			}
		}
	}
	return out
}

func cleanStreamType(t string) string {
	t = strings.TrimSpace(t)
	for strings.HasPrefix(t, "*") {
		t = strings.TrimPrefix(t, "*")
	}
	if strings.HasPrefix(t, "<-chan ") {
		return strings.TrimPrefix(t, "<-chan ")
	}
	if strings.HasPrefix(t, "chan ") {
		return strings.TrimPrefix(t, "chan ")
	}
	return t
}

func (p *protocolScan) structFor(fn *Func, typ string) *ast.StructType {
	typ = strings.TrimSpace(typ)
	typ = strings.TrimPrefix(typ, "*")
	if strings.HasPrefix(typ, "[]") || strings.HasPrefix(typ, "<-chan ") || strings.HasPrefix(typ, "chan ") {
		return nil
	}
	if i := strings.Index(typ, "["); i > 0 {
		typ = typ[:i]
	}
	if st := p.idx.ix.structOf(fn.File, typ); st != nil {
		return st
	}
	// A type from another package may be reached without that package being
	// imported by this file (an interface result, for example). Resolve by the
	// package's short name.
	if dot := strings.LastIndex(typ, "."); dot > 0 {
		alias, name := typ[:dot], typ[dot+1:]
		for pkg, types := range p.idx.ix.types {
			if altPkg(pkg) == alias || strings.HasSuffix(pkg, "/"+alias) {
				if st := types[name]; st != nil {
					return st
				}
			}
		}
	}
	return nil
}

func (p *protocolScan) typeIn(scope *protoScope, e ast.Expr) string {
	e = unwrapExpr(e)
	if e == nil || scope == nil || scope.fn == nil {
		return ""
	}
	switch x := e.(type) {
	case *ast.Ident:
		if scope.bind != nil {
			if be, ok := scope.bind[x.Name]; ok && scope.parent != nil {
				return p.typeIn(scope.parent, be)
			}
		}
		if t := p.localTypes[scope.fn.Key+"."+x.Name]; t != "" {
			return t
		}
		return p.paramType(scope.fn, x.Name)
	case *ast.SelectorExpr:
		base := p.typeIn(scope, x.X)
		if base == "" {
			return ""
		}
		return p.fieldType(scope.fn, base, x.Sel.Name)
	case *ast.CompositeLit:
		return typeString(x.Type)
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) > 0 {
			return typeString(x.Args[0])
		}
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			if (sel.Sel.Name == "Upgrade" || sel.Sel.Name == "Accept") && p.isUpgraderCall(scope, x) {
				return "*websocket.Conn"
			}
		}
		if target := p.target(scope, x); target != nil && target.Decl.Type.Results != nil && len(target.Decl.Type.Results.List) > 0 {
			return qualifyResult(target, typeString(target.Decl.Type.Results.List[0].Type))
		}
	}
	return ""
}

func qualifyResult(fn *Func, typ string) string {
	t := typ
	prefix := ""
	switch {
	case strings.HasPrefix(t, "<-chan "):
		prefix, t = "<-chan ", strings.TrimPrefix(t, "<-chan ")
	case strings.HasPrefix(t, "chan "):
		prefix, t = "chan ", strings.TrimPrefix(t, "chan ")
	case strings.HasPrefix(t, "[]"):
		prefix, t = "[]", strings.TrimPrefix(t, "[]")
	}
	t = strings.TrimPrefix(t, "*")
	star := ""
	if strings.HasPrefix(typ, "*") {
		star = "*"
	}
	if t != "" && !strings.Contains(t, ".") && isBareTypeName(t) && !isBuiltinType(t) {
		t = altPkg(fn.PkgPath) + "." + t
	}
	return star + prefix + t
}

func isBuiltinType(t string) bool {
	switch t {
	case "string", "bool", "byte", "rune", "error", "any",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float32", "float64", "complex64", "complex128":
		return true
	}
	return false
}

func isBareTypeName(t string) bool {
	if t == "" {
		return false
	}
	for _, r := range t {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func (p *protocolScan) paramType(fn *Func, name string) string {
	if fn == nil || fn.Decl == nil || fn.Decl.Type == nil || fn.Decl.Type.Params == nil {
		return ""
	}
	for _, f := range fn.Decl.Type.Params.List {
		for _, n := range f.Names {
			if n.Name == name {
				return typeString(f.Type)
			}
		}
	}
	return ""
}

func (p *protocolScan) fieldType(fn *Func, typ, field string) string {
	typ = strings.TrimPrefix(strings.TrimSpace(typ), "*")
	st := p.structFor(fn, typ)
	if st == nil || st.Fields == nil {
		return ""
	}
	for _, f := range st.Fields.List {
		for _, n := range f.Names {
			if n.Name == field {
				return typeString(f.Type)
			}
		}
	}
	return ""
}

func (p *protocolScan) target(scope *protoScope, ce *ast.CallExpr) *Func {
	fn := scope.fn
	switch f := ce.Fun.(type) {
	case *ast.Ident:
		return p.idx.m.PKG[fn.PkgPath][f.Name]
	case *ast.SelectorExpr:
		if base := identName(f.X); base != "" {
			if pkg, ok := fn.File.Imports[base]; ok {
				return p.idx.m.PKG[pkg][f.Sel.Name]
			}
		}
		typ := strings.TrimPrefix(strings.TrimSpace(p.typeIn(scope, f.X)), "*")
		if i := strings.LastIndex(typ, "."); i >= 0 {
			typ = typ[i+1:]
		}
		for _, m := range p.idx.m.Methods[f.Sel.Name] {
			if typ != "" && m.Recv == typ {
				return m
			}
		}
		if typ == "" && len(p.idx.m.Methods[f.Sel.Name]) == 1 {
			return p.idx.m.Methods[f.Sel.Name][0]
		}
	}
	return nil
}

func (p *protocolScan) eventNames(scope *protoScope, e ast.Expr) []string {
	e = unwrapExpr(e)
	if e == nil {
		return nil
	}
	if ce, ok := e.(*ast.CallExpr); ok && len(ce.Args) == 1 {
		if id, ok := ce.Fun.(*ast.Ident); ok {
			switch id.Name {
			case "string", "EventType", "MessageType", "MsgType":
				return p.eventNames(scope, ce.Args[0])
			}
		}
	}
	if v, ok := p.idx.ix.evalString(scope.fn.File, e); ok {
		return []string{v}
	}
	if sel, ok := e.(*ast.SelectorExpr); ok {
		typ := p.typeIn(scope, sel.X)
		if typ != "" {
			if ft := p.fieldType(scope.fn, typ, sel.Sel.Name); ft != "" {
				if names := p.idx.eventConsts[shortTypeText(ft)]; len(names) > 0 {
					return names
				}
			}
		}
		if sel.Sel.Name == "Type" && len(p.idx.fieldEvents) > 0 {
			return p.idx.fieldEvents
		}
		return nil
	}
	if id, ok := e.(*ast.Ident); ok {
		if t := p.typeIn(scope, id); t != "" {
			if names := p.idx.eventConsts[shortTypeText(t)]; len(names) > 0 {
				return names
			}
		}
	}
	return nil
}

func shortTypeName(e ast.Expr) string {
	return shortTypeText(typeString(e))
}

func shortTypeText(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "*")
	if i := strings.LastIndex(t, "."); i >= 0 {
		t = t[i+1:]
	}
	return t
}

func channelElem(t string) string {
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "<-chan ") {
		return strings.TrimPrefix(t, "<-chan ")
	}
	if strings.HasPrefix(t, "chan ") {
		return strings.TrimPrefix(t, "chan ")
	}
	return ""
}

func paramNames(fn *Func) []string {
	if fn == nil || fn.Decl == nil || fn.Decl.Type == nil || fn.Decl.Type.Params == nil {
		return nil
	}
	var out []string
	for _, f := range fn.Decl.Type.Params.List {
		for _, n := range f.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

func (p *protocolScan) stringEvidence(scope *protoScope, ce *ast.CallExpr, sel string) {
	for _, a := range ce.Args {
		p.sseExpr(scope, a)
	}
	switch sel {
	case "Fprintf", "Sprintf", "Printf":
		p.sseFormat(scope, ce)
	}
}

// sseExpr walks a concatenation and reads the event/data line anchors. The
// expression after the anchor supplies the event name or the payload type.
func (p *protocolScan) sseExpr(scope *protoScope, e ast.Expr) {
	parts := flattenAdd(e)
	for i, part := range parts {
		txt := part.text
		low := strings.ToLower(txt)
		if strings.Contains(low, "text/event-stream") {
			p.contentType = true
			p.sseStrong = true
			p.sseEv = appendEvidence(p.sseEv, scope.fn.File, line(e.Pos()), "content-type")
		}
		if strings.HasPrefix(txt, ":") && (p.contentType || p.sseStrong || p.sseEvent) {
			h := strings.TrimSpace(txt)
			if h != "" && len(h) < 32 {
				p.heartbeat = addUniqueText(p.heartbeat, strings.Trim(h, ":"))
			}
		}
		if strings.Contains(low, "retry:") {
			p.retry = strings.TrimSpace(txt[strings.Index(low, "retry:")+6:])
		}
		if strings.Contains(txt, "event:") {
			p.eventLines = true
			p.sseEv = appendEvidence(p.sseEv, scope.fn.File, line(e.Pos()), "event line")
			rest := txt[strings.Index(txt, "event:")+6:]
			if nl := strings.Index(rest, "\n"); nl >= 0 {
				rest = rest[:nl]
			}
			rest = strings.TrimSpace(rest)
			if rest != "" && !strings.Contains(rest, "%") {
				p.addEvent(rest)
			} else if rest == "" && i+1 < len(parts) && parts[i+1].expr != nil {
				for _, n := range p.eventNames(scope, parts[i+1].expr) {
					p.addEvent(n)
				}
			}
		}
		if strings.Contains(txt, "data:") {
			p.dataLines = true
			p.sseEv = appendEvidence(p.sseEv, scope.fn.File, line(e.Pos()), "data line")
			rest := txt[strings.Index(txt, "data:")+5:]
			if nl := strings.Index(rest, "\n"); nl >= 0 {
				rest = rest[:nl]
			}
			if strings.TrimSpace(rest) == "" && i+1 < len(parts) && parts[i+1].expr != nil {
				typ, fields, ex, raw := p.schema(scope, parts[i+1].expr)
				p.addMsg("send", "", typ, fields, ex, raw, scope.fn.File, line(e.Pos()))
			}
		}
	}
}

type ssePart struct {
	text string
	expr ast.Expr
}

func flattenAdd(e ast.Expr) []ssePart {
	e = unwrapExpr(e)
	switch x := e.(type) {
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return append(flattenAdd(x.X), flattenAdd(x.Y)...)
		}
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			if v, err := strconv.Unquote(x.Value); err == nil {
				return []ssePart{{text: v}}
			}
		}
	}
	return []ssePart{{expr: e}}
}

func (p *protocolScan) sseFormat(scope *protoScope, ce *ast.CallExpr) {
	args := ce.Args
	sel := callName(ce)
	formatAt := 0
	if sel == "Fprintf" {
		formatAt = 1
	}
	if formatAt >= len(args) {
		return
	}
	v, ok := p.idx.ix.evalString(scope.fn.File, args[formatAt])
	if !ok {
		return
	}
	low := strings.ToLower(v)
	if strings.Contains(low, "text/event-stream") {
		p.contentType = true
		p.sseStrong = true
	}
	rest := args[formatAt+1:]
	ai := 0
	for pos := 0; pos < len(v); {
		idx := -1
		for _, ph := range []string{"%s", "%v", "%q"} {
			if j := strings.Index(v[pos:], ph); j >= 0 && (idx < 0 || j < idx) {
				idx = j
			}
		}
		if idx < 0 {
			p.sseText(scope, ce, v[pos:], nil)
			break
		}
		seg := v[pos : pos+idx]
		var arg ast.Expr
		if ai < len(rest) {
			arg = rest[ai]
		}
		p.sseText(scope, ce, seg, arg)
		pos += idx + 2
		ai++
	}
	_ = low
}

func (p *protocolScan) sseText(scope *protoScope, ce *ast.CallExpr, seg string, arg ast.Expr) {
	if strings.Contains(strings.ToLower(seg), "text/event-stream") {
		p.contentType = true
		p.sseStrong = true
	}
	if strings.HasPrefix(strings.TrimSpace(seg), ":") && (p.contentType || p.sseStrong || p.sseEvent) {
		h := strings.TrimSpace(seg)
		if len(h) < 32 {
			p.heartbeat = addUniqueText(p.heartbeat, strings.Trim(h, ":"))
		}
	}
	if strings.Contains(seg, "event:") {
		p.eventLines = true
		p.sseEv = appendEvidence(p.sseEv, scope.fn.File, line(ce.Pos()), "event line")
		rest := seg[strings.Index(seg, "event:")+6:]
		if nl := strings.Index(rest, "\n"); nl >= 0 {
			rest = rest[:nl]
		}
		rest = strings.TrimSpace(rest)
		if rest != "" && !strings.Contains(rest, "%") {
			p.addEvent(rest)
		} else if rest == "" && arg != nil {
			for _, n := range p.eventNames(scope, arg) {
				p.addEvent(n)
			}
		}
	}
	if strings.Contains(seg, "data:") {
		p.dataLines = true
		p.sseEv = appendEvidence(p.sseEv, scope.fn.File, line(ce.Pos()), "data line")
		rest := seg[strings.Index(seg, "data:")+5:]
		if nl := strings.Index(rest, "\n"); nl >= 0 {
			rest = rest[:nl]
		}
		if strings.TrimSpace(rest) == "" && arg != nil {
			typ, fields, ex, raw := p.schema(scope, arg)
			p.addMsg("send", "", typ, fields, ex, raw, scope.fn.File, line(ce.Pos()))
		}
	}
	if strings.Contains(seg, "retry:") {
		p.retry = strings.TrimSpace(seg[strings.Index(seg, "retry:")+6:])
	}
}

func appendEvidence(list []JEvidence, f *FileCtx, l int, note string) []JEvidence {
	if f == nil || l == 0 || len(list) >= 10 {
		return list
	}
	for _, old := range list {
		if old.F == f.Rel && old.L == l && old.N == note {
			return list
		}
	}
	return append(list, JEvidence{F: f.Rel, L: l, N: note})
}

func addUniqueText(old, v string) string {
	v = strings.TrimSpace(v)
	if v == "" || old == v {
		return old
	}
	if old == "" {
		return v
	}
	for _, part := range strings.Split(old, ", ") {
		if part == v {
			return old
		}
	}
	return old + ", " + v
}

func (p *protocolScan) finish() *JStream {
	if p.wsStrong {
		s := &JStream{
			K:      "ws",
			Ev:     p.wsEv,
			Events: p.events,
			Evs:    p.eventList(),
			Send:   limitMsgs(p.send, 96),
			Recv:   limitMsgs(p.recv, 96),
			Ext:    limitExt(p.ext, 48),
			SP:     p.subprotocols,
			OC:     p.origin,
			HB:     p.heartbeat,
		}
		if p.ping {
			s.HB = addUniqueText(s.HB, "ping/pong")
		}
		sortEvidence(s.Ev)
		return s
	}
	if p.sseStrong || p.sseEvent || (p.streamCall && (p.eventLines || p.dataLines)) || (p.flush && (p.eventLines || p.dataLines)) {
		s := &JStream{
			K:      "sse",
			Ev:     p.sseEv,
			Events: p.events,
			Evs:    p.eventList(),
			Send:   limitMsgs(p.send, 48),
			HB:     p.heartbeat,
			RT:     p.retry,
		}
		// A flush is supporting evidence, not a stream by itself. The
		// content-type/event-line evidence above is what carries the verdict.
		sortEvidence(s.Ev)
		return s
	}
	return nil
}

func limitMsgs(in []JMessage, max int) []JMessage {
	if len(in) <= max {
		return in
	}
	return in[:max]
}

func limitExt(in []JExt, max int) []JExt {
	if len(in) <= max {
		return in
	}
	return in[:max]
}

func sortEvidence(list []JEvidence) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].F != list[j].F {
			return list[i].F < list[j].F
		}
		return list[i].L < list[j].L
	})
}
