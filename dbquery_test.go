package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLQueryReadsVerbAndTable(t *testing.T) {
	cases := []struct {
		in string
		op string
		tb string
		ok bool
	}{
		{"SELECT id, name FROM users WHERE id = $1", "select", "users", true},
		{"select * from public.users", "select", "users", true},
		{`SELECT * FROM "public"."users"`, "select", "users", true},
		{"SELECT * FROM `events`", "select", "events", true},
		{"SELECT * FROM [audit_log]", "select", "audit_log", true},
		{"-- recent rows\nSELECT * FROM orders", "select", "orders", true},
		{"/* block */ SELECT * FROM sessions", "select", "sessions", true},
		{"INSERT INTO orders (id, total) VALUES ($1, $2)", "insert", "orders", true},
		{"UPDATE sessions SET token = $1 WHERE id = $2", "update", "sessions", true},
		{"DELETE FROM tokens WHERE user_id = $1", "delete", "tokens", true},
		{"REPLACE INTO cache (k, v) VALUES (?, ?)", "replace", "cache", true},
		{"TRUNCATE TABLE events", "truncate", "events", true},
		{"CREATE TABLE audit_log (id int)", "ddl", "audit_log", true},
		{"DROP TABLE IF EXISTS sessions", "ddl", "sessions", true},
		// Not a table: dynamic names, subqueries and CTEs stay unresolved.
		{"SELECT * FROM %s", "", "", false},
		{"SELECT * FROM $1", "", "", false},
		{"SELECT * FROM (SELECT 1) t", "", "", false},
		{"WITH x AS (SELECT 1) SELECT * FROM x", "", "", false},
		{"SELECT 1", "", "", false},
		{"not sql", "", "", false},
	}
	for _, c := range cases {
		got, ok := sqlQuery(c.in)
		if ok != c.ok || got.Op != c.op || got.Tb != c.tb {
			t.Errorf("sqlQuery(%q) = %+v/%v, want %s/%s/%v", c.in, got, ok, c.op, c.tb, c.ok)
		}
	}
}

const dbQueryFixture = `package dbq

import "context"

type DB struct{}

func (d *DB) QueryContext(ctx context.Context, q string, args ...any) (*Rows, error) {
	return nil, nil
}

func (d *DB) ExecContext(ctx context.Context, q string, args ...any) (any, error) {
	return nil, nil
}

func (d *DB) Table(name string) *DB                 { return d }
func (d *DB) Where(q string, args ...any) *DB       { return d }
func (d *DB) Find(dest any) error                   { return nil }
func (d *DB) Delete(dest any) error                 { return nil }
func (d *DB) Model(dest any) *DB                    { return d }
func (d *DB) Create(dest any) error                 { return nil }

type Rows struct{}
type User struct{}

type Server struct{ db *DB }

const updateQ = "UPDATE sessions SET token = ?"

func (s *Server) Handler(ctx context.Context, table string) {
	s.db.QueryContext(ctx, "SELECT id FROM users WHERE id = ?", 1)
	s.db.ExecContext(ctx, updateQ)
	s.db.Table("orders").Where("id = ?", 1).Find(&User{})
	s.db.Table("audit").Where("id = ?", 1).Delete(&User{})
	s.db.Table("comments").Where("body = ?", "SELECT * FROM secrets").Find(&User{})
	s.db.Model(&User{}).Create(&User{})
	s.db.QueryContext(ctx, "SELECT * FROM "+table)

	deleteQ := "DELETE FROM tokens WHERE id = ?"
	s.db.ExecContext(ctx, deleteQ)

	ambiguous := "SELECT * FROM first"
	ambiguous = "SELECT * FROM second"
	s.db.ExecContext(ctx, ambiguous)
}
`

func dbQueryModule(t *testing.T) (*Module, *Func) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/dbq\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "db.go"), []byte(dbQueryFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	fn := m.Funcs["example.com/dbq|Server|Handler"]
	if fn == nil {
		t.Fatal("missing Server.Handler")
	}
	return m, fn
}

func TestEffectsCarryDBOperationAndTable(t *testing.T) {
	m, fn := dbQueryModule(t)
	fx := buildEffects(m, fn)
	if len(fx) != 9 {
		t.Fatalf("effects = %+v, want 9", fx)
	}
	got := map[string]JEffect{}
	for _, e := range fx {
		got[e.N+"|"+e.Tb] = e
	}
	want := []struct{ name, op, tb string }{
		{"db.QueryContext", "select", "users"},
		{"db.ExecContext", "update", "sessions"},
		{"db.Find", "select", "orders"},
		{"db.Delete", "delete", "audit"},
		{"db.Find", "select", "comments"},
		{"db.Create", "insert", "User"},
		{"db.QueryContext", "", ""}, // dynamic table
		{"db.ExecContext", "delete", "tokens"},
		{"db.ExecContext", "", ""}, // reassigned local, not guessed
	}
	for _, w := range want {
		e, ok := got[w.name+"|"+w.tb]
		if !ok {
			t.Fatalf("missing effect %s on %q; got %+v", w.name, w.tb, fx)
		}
		if e.Op != w.op {
			t.Fatalf("%s op = %q, want %q", w.name, e.Op, w.op)
		}
	}
}

func TestRouteDocCarriesDBQueries(t *testing.T) {
	m, fn := dbQueryModule(t)
	doc := buildDoc(m, &Route{
		Method: "GET",
		Path:   "/db",
		Chain:  []Ref{{Name: "Handler", Role: "handler", Fn: fn}},
	})
	if doc == nil {
		t.Fatal("no doc")
	}
	got := map[string]bool{}
	for _, q := range doc.Queries {
		got[q.Op+"|"+q.Tb] = true
	}
	for _, want := range []string{"select|users", "update|sessions", "select|orders", "delete|audit", "select|comments", "insert|User", "delete|tokens"} {
		if !got[want] {
			t.Fatalf("missing query %q; got %+v", want, doc.Queries)
		}
	}
	// A SQL-looking argument to Where is a value, not the statement.
	if got["select|secrets"] {
		t.Fatalf("a Where argument must not read as the query; got %+v", doc.Queries)
	}
	// The bare Table(...) and Model(...) calls inside each chain must not
	// duplicate the operation entry.
	for _, bare := range []string{"|orders", "|audit", "|comments", "|User"} {
		if got[bare] {
			t.Fatalf("bare table %q should be folded into its operation; got %+v", bare, doc.Queries)
		}
	}
	if len(doc.Queries) != 7 {
		t.Fatalf("queries = %+v, want 7", doc.Queries)
	}
}
