package main

import (
	"go/ast"
	"strconv"
	"strings"
)

// JEffect is one side effect in a handler: a read, a write, or an event
// publication. Effects keep source order so the UI can show what happened
// before the reply was written.
type JEffect struct {
	K  string `json:"k"` // read, write, publish
	N  string `json:"n"` // call name, e.g. db.Save
	B  string `json:"b,omitempty"`
	S  string `json:"s,omitempty"`
	L  int    `json:"l,omitempty"`
	Fk string `json:"fk,omitempty"`
	C  int    `json:"c,omitempty"`
}

func buildEffects(fn *Func) []JEffect {
	if fn == nil {
		return nil
	}
	var out []JEffect
	seen := map[string]int{}
	resolved := map[string]Call{}
	for _, c := range fn.Calls {
		if _, ok := resolved[c.Expr]; !ok {
			resolved[c.Expr] = c
		}
	}
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok || len(out) >= 32 {
			return true
		}
		d, ok := describeCall(ce)
		if !ok || (d.Sel == "" && d.Base == "") {
			return true
		}
		if c, ok := resolved[d.Expr]; ok {
			d = c
		}
		k, b := effectKind(d)
		if k == "" {
			return true
		}
		name := strings.TrimPrefix(d.Base+"."+d.Sel, ".")
		at := line(ce.Pos())
		// Calls are kept per source line: the SSE loop writes the preamble,
		// every event, and a heartbeat from three different lines.
		key := k + "|" + name + "|" + d.Expr + "|" + strconv.Itoa(at)
		if pos, ok := seen[key]; ok {
			out[pos].C++
			return true
		}
		e := JEffect{
			K: k,
			N: name,
			B: b,
			S: d.Expr,
			L: at,
			C: 1,
		}
		if d.Target != nil {
			e.Fk = d.Target.Key
		}
		seen[key] = len(out)
		out = append(out, e)
		return true
	})
	return out
}

func effectKind(c Call) (string, string) {
	sel := strings.ToLower(c.Sel)
	base := strings.ToLower(c.Base)
	// The socket reply is already the server-replies section; do not repeat it
	// as an after-effect.
	if strings.HasPrefix(sel, "write") && (base == "c" || base == "conn" || base == "w" || base == "ctx") {
		return "", ""
	}
	if strings.Contains(c.Expr, ".Header().Set") {
		return "", ""
	}
	switch sel {
	case "newEvent", "ssevent", "sseventwithtype", "publish", "publishevent", "broadcast",
		"broadcastevent", "broadcastto", "broadcastuser", "sendevent", "notify", "enqueue":
		return "publish", "event"
	}
	switch sel {
	case "create", "save", "update", "updates", "delete", "insert", "exec", "put", "set",
		"add", "incr", "incrby", "del", "remove", "push", "publish", "write", "writemsg",
		"writemsgpack", "writejson", "writemessage", "writestring", "send", "sendto", "commit":
		return "write", effectGroup(c)
	case "get", "find", "first", "last", "take", "scan", "count", "exists", "load",
		"query", "select", "raw", "fetch", "read", "readmessage", "readjson", "search":
		return "read", effectGroup(c)
	}
	// A few clients name the store only in the receiver, with a neutral verb.
	if (strings.Contains(base, "db") || strings.Contains(base, "repo") || strings.Contains(base, "store") || strings.Contains(base, "mongo") || strings.Contains(base, "redis")) && sel != "" {
		return "read", effectGroup(c)
	}
	return "", ""
}

func effectGroup(c Call) string {
	if g := tagOf(c); g != "" {
		return g
	}
	base := strings.ToLower(c.Base)
	switch {
	case strings.Contains(base, "db"), strings.Contains(base, "repo"), strings.Contains(base, "store"),
		strings.Contains(base, "mongo"), strings.Contains(base, "sql"), strings.Contains(base, "tx"):
		return "db"
	case strings.Contains(base, "redis"), strings.Contains(base, "cache"):
		return "cache"
	case strings.Contains(base, "client"), strings.Contains(base, "http"), strings.Contains(base, "grpc"):
		return "net"
	}
	return ""
}
