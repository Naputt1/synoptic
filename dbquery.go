package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
)

// JQuery is one database operation synoptic can prove: the SQL verb and the
// table it targets. Either half may be empty when only the other is known.
type JQuery struct {
	Op string `json:"op,omitempty"` // select, insert, update, delete, replace, ddl
	Tb string `json:"tb,omitempty"` // target table, schema prefix stripped
}

var (
	sqlComments = regexp.MustCompile(`(?s)/\*.*?\*/|--[^\n]*`)
	sqlVerbRe   = regexp.MustCompile(`(?i)^\s*(select|insert|update|delete|replace|truncate|alter|create|drop|with)\b`)
	sqlFromRe   = regexp.MustCompile(`(?i)\bfrom\s+([^\s(;,]+)`)
	sqlIntoRe   = regexp.MustCompile(`(?i)\binto\s+([^\s(;,]+)`)
	sqlUpdateRe = regexp.MustCompile(`(?i)\bupdate\s+([^\s(;,]+)`)
	sqlTableRe  = regexp.MustCompile(`(?i)\btable\s+(?:if\s+not\s+exists\s+|if\s+exists\s+)?([^\s(;,]+)`)
	sqlTruncRe  = regexp.MustCompile(`(?i)\btruncate\s+(?:table\s+)?([^\s(;,]+)`)
)

// dbQueryOf reads the operation and target table a single call proves. It
// looks for a SQL statement first, then for an ORM chain. Calls that do not
// look like database access, and SQL it cannot parse, stay empty rather than
// guessed.
func dbQueryOf(m *Module, fn *Func, ce *ast.CallExpr, c Call) (JQuery, bool) {
	if m == nil || fn == nil || fn.File == nil || ce == nil || !dbCallShape(c) {
		return JQuery{}, false
	}
	if q, ok := chainSQL(m, fn, ce); ok {
		return q, true
	}
	if tb, ok := chainTable(m, fn, ce); ok {
		return JQuery{Op: ormOp(c.Sel), Tb: tb}, true
	}
	if tb, ok := chainModel(ce); ok {
		return JQuery{Op: ormOp(c.Sel), Tb: tb}, true
	}
	return JQuery{}, false
}

// dbCallShape keeps a string literal in unrelated code from reading as a
// query. The name of the call, its receiver chain, or the existing store tags
// must point at a database first.
func dbCallShape(c Call) bool {
	if tagOf(c) == "db" || effectGroup(c) == "db" || hasPart(chainParts(c), dbWords) {
		return true
	}
	switch strings.ToLower(c.Sel) {
	case "query", "queryrow", "querycontext", "queryrowcontext", "exec", "execcontext",
		"raw", "get", "select", "namedexec", "mustexec", "scan", "prepare":
		return true
	}
	return false
}

// sqlArgCalls are calls whose arguments hold the statement. Other calls on a
// chain (Table, Where, Order, …) carry names and values, so a value that looks
// like SQL must not be read as the query.
var sqlArgCalls = map[string]bool{
	"query": true, "querycontext": true, "queryrow": true, "queryrowcontext": true,
	"queryx": true, "queryrowx": true, "exec": true, "execcontext": true,
	"raw": true, "namedexec": true, "mustexec": true, "prepare": true,
	"preparecontext": true, "get": true, "getcontext": true, "select": true,
	"selectcontext": true, "namedquery": true,
}

// chainSQL walks a call chain so db.Raw("SELECT …").Scan(&v) is read from the
// inner Raw call. Only an argument that resolves to a string literal, its
// concatenation, or a string constant counts.
func chainSQL(m *Module, fn *Func, ce *ast.CallExpr) (JQuery, bool) {
	var out JQuery
	found := false
	ast.Inspect(ce, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !sqlArgCalls[strings.ToLower(callName(call))] {
			return true
		}
		for _, arg := range call.Args {
			s, ok := queryString(m, fn, arg)
			if !ok {
				continue
			}
			if q, ok := sqlQuery(s); ok {
				out, found = q, true
				return false
			}
		}
		return true
	})
	return out, found
}

// queryString resolves the statement text at a call site. It accepts what
// inputsIndex resolves (a literal, its concatenation, a constant) and, for a
// local variable, a single assignment from such a value in the same function.
func queryString(m *Module, fn *Func, e ast.Expr) (string, bool) {
	ix := inputsIndex(m)
	if s, ok := ix.evalString(fn.File, e); ok {
		return s, true
	}
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	id, ok := e.(*ast.Ident)
	if !ok || fn.Decl == nil || fn.Decl.Body == nil {
		return "", false
	}
	return localString(ix, fn, id.Name)
}

// localString reads a variable assigned exactly once, so a reassigned or
// conditionally built query is left unresolved instead of guessed.
func localString(ix *inputIndex, fn *Func, name string) (string, bool) {
	value := ""
	writes := 0
	unknown := false
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name != name {
					continue
				}
				writes++
				if (x.Tok != token.DEFINE && x.Tok != token.ASSIGN) || i >= len(x.Rhs) {
					unknown = true
					continue
				}
				s, ok := ix.evalString(fn.File, x.Rhs[i])
				if !ok {
					unknown = true
					continue
				}
				value = s
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if id.Name != name || i >= len(x.Values) {
					continue
				}
				writes++
				s, ok := ix.evalString(fn.File, x.Values[i])
				if !ok {
					unknown = true
					continue
				}
				value = s
			}
		}
		return true
	})
	if unknown || writes != 1 {
		return "", false
	}
	return value, true
}

// sqlQuery reads the verb and the target table from one SQL statement. The
// table is the one the verb acts on: FROM for a read, INTO for an insert, the
// name after UPDATE, and FROM for a delete. Joins and subqueries are left
// alone, and a CTE is skipped because its first FROM names the CTE.
func sqlQuery(s string) (JQuery, bool) {
	clean := strings.Join(strings.Fields(sqlComments.ReplaceAllString(s, " ")), " ")
	m := sqlVerbRe.FindStringSubmatch(clean)
	if m == nil {
		return JQuery{}, false
	}
	verb := strings.ToLower(m[1])
	var tb string
	switch verb {
	case "select":
		tb = firstTable(sqlFromRe, clean)
	case "insert", "replace":
		tb = firstTable(sqlIntoRe, clean)
	case "update":
		tb = firstTable(sqlUpdateRe, clean)
	case "delete":
		tb = firstTable(sqlFromRe, clean)
	case "truncate":
		tb = firstTable(sqlTruncRe, clean)
	case "alter", "create", "drop":
		verb = "ddl"
		tb = firstTable(sqlTableRe, clean)
	case "with":
		return JQuery{}, false
	}
	tb = cleanTable(tb)
	if tb == "" {
		return JQuery{}, false
	}
	return JQuery{Op: verb, Tb: tb}, true
}

func firstTable(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}

// cleanTable strips quoting and a schema prefix, and rejects a placeholder or
// a subquery where a table name should be.
func cleanTable(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'`[]")
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Trim(s, "\"'`[]")
	if s == "" || strings.ContainsAny(s, "%$?{}()") {
		return ""
	}
	switch strings.ToLower(s) {
	case "select", "values", "set", "where", "only", "lateral", "table", "if":
		return ""
	}
	return s
}

// ormOp maps the terminal method of an ORM chain to the SQL verb it runs.
func ormOp(sel string) string {
	switch strings.ToLower(sel) {
	case "find", "first", "take", "last", "scan", "count", "pluck", "findinbatches", "distinct":
		return "select"
	case "create", "createinbatches":
		return "insert"
	case "save", "update", "updates", "updatecolumn", "updatecolumns":
		return "update"
	case "delete":
		return "delete"
	}
	return ""
}

// chainTable finds Table("name") in the receiver chain, which is how GORM
// names the table a chain acts on.
func chainTable(m *Module, fn *Func, ce *ast.CallExpr) (string, bool) {
	tb := ""
	ast.Inspect(ce, func(n ast.Node) bool {
		if tb != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := unindex(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Table" || len(call.Args) == 0 {
			return true
		}
		if v, ok := queryString(m, fn, call.Args[0]); ok {
			tb = strings.TrimSpace(v)
		}
		return tb == ""
	})
	return tb, tb != ""
}

// chainModel finds Model(&User{}) in the receiver chain. The type name is not
// the table name, but it is what the data-touched panel has always shown when
// the source does not name the table itself.
func chainModel(ce *ast.CallExpr) (string, bool) {
	tb := ""
	ast.Inspect(ce, func(n ast.Node) bool {
		if tb != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := unindex(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Model" || len(call.Args) == 0 {
			return true
		}
		tb = modelTypeName(call.Args[0])
		return tb == ""
	})
	return tb, tb != ""
}

func modelTypeName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return modelTypeName(x.X)
		}
	case *ast.ParenExpr:
		return modelTypeName(x.X)
	case *ast.CompositeLit:
		name := typeString(x.Type)
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[i+1:]
		}
		return name
	case *ast.CallExpr:
		if id, ok := unindex(x.Fun).(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 {
			if id, ok := x.Args[0].(*ast.Ident); ok {
				return id.Name
			}
		}
	}
	return ""
}
