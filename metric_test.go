package main

import "testing"

// metricFixture exercises every reason once. The point of the file is the
// classification, so the bodies stay deliberately small.
const metricFixture = `package metric

import (
	"database/sql"
	"time"

	"example.com/metric/sub"
)

type UserID string

type Store interface {
	Get(id string) string
}

type Memory struct{}

func (m *Memory) Get(id string) string { return id }

type Redis struct{}

func (r *Redis) Get(id string) string { return id }

type configDB struct{}

func (configDB) Exec(q string) {}

var config = struct{ DB configDB }{}

type Server struct {
	store Store
	db    *sql.DB
	cb    func(string) string
}

func (s *Server) Ambiguous() string { return s.store.Get("x") }

func (s *Server) External() error { return s.db.Ping() }

func (s *Server) FieldFunc() string { return s.cb("x") }

func known() {}

func useConfig() { config.DB.Exec("q") }

func convertInt(n int) string { return string(n) }

func convertNamed(id string) UserID { return UserID(id) }

func convertPkg(s string) sub.Code { return sub.Code(s) }

func blind() { missing() }

func useBuiltin(xs []int) int { return len(xs) }

func useParam(fn func(string) string) string { return fn("x") }

func useKnown() { known() }

func useTime() { _ = time.Now() }
`

func loadMetricFixture(t *testing.T) *Module {
	t.Helper()
	dir := t.TempDir()
	writeDiagFile(t, dir, "go.mod", "module example.com/metric\n\ngo 1.22\n")
	writeDiagFile(t, dir, "main.go", metricFixture)
	writeDiagFile(t, dir, "sub/sub.go", "package sub\n\ntype Code string\n")
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Every call is counted once under exactly one reason, so a miss can be read as
// "external" instead of the old catch-all "in-module unresolved".
func TestCallReasonsAreExact(t *testing.T) {
	m := loadMetricFixture(t)
	d := m.Diag

	if d.Calls != 12 {
		t.Fatalf("calls = %d, want 12: %+v", d.Calls, d)
	}
	if d.Resolved != 1 {
		t.Fatalf("resolved = %d, want 1", d.Resolved)
	}
	if d.Unresolved != 1 {
		t.Fatalf("unresolved (blind) = %d, want 1", d.Unresolved)
	}
	if d.External != 2 {
		t.Fatalf("external = %d, want 2", d.External)
	}
	if d.Convert != 3 {
		t.Fatalf("convert = %d, want 3", d.Convert)
	}
	if d.Dynamic != 2 {
		t.Fatalf("dynamic = %d, want 2", d.Dynamic)
	}
	if d.Ambiguous != 1 {
		t.Fatalf("ambiguous = %d, want 1", d.Ambiguous)
	}
	if d.Unknown != 1 {
		t.Fatalf("unknown = %d, want 1 (config.DB.Exec)", d.Unknown)
	}
	if len(d.Unres) != 1 {
		t.Fatalf("blind list = %+v, want one entry", d.Unres)
	}
	if got := d.Unres[0]; got.Expr != "missing" || got.Func != "blind" || got.Reason != "blind" {
		t.Fatalf("blind entry = %+v, want blind: missing", got)
	}

	want := map[string]int{
		"resolved": 1, "blind": 1, "external": 2, "convert": 3,
		"dynamic": 2, "ambiguous": 1, "unknown": 1, "builtin": 1,
	}
	for reason, n := range want {
		if d.Why[reason] != n {
			t.Fatalf("why[%s] = %d, want %d (all: %+v)", reason, d.Why[reason], n, d.Why)
		}
	}
}

// The named reasons must land on the calls that earned them, not only on the
// aggregate counters.
func TestCallReasonsNameTheRightCall(t *testing.T) {
	m := loadMetricFixture(t)
	cases := []struct {
		fn, expr, why string
	}{
		{"useConfig", "config.DB.Exec", "unknown"},
		{"convertInt", "string", "convert"},
		{"convertNamed", "UserID", "convert"},
		{"convertPkg", "sub.Code", "convert"},
		{"blind", "missing", "blind"},
		{"useBuiltin", "len", "builtin"},
		{"Ambiguous", "s.store.Get", "ambiguous"},
		{"External", "s.db.Ping", "external"},
		{"FieldFunc", "s.cb", "dynamic"},
		{"useParam", "fn", "dynamic"},
		{"useKnown", "known", "resolved"},
		{"useTime", "time.Now", "external"},
	}
	for _, tc := range cases {
		var found string
		for _, f := range m.Funcs {
			if f.Name != tc.fn {
				continue
			}
			for _, c := range f.Calls {
				if c.Expr == tc.expr {
					found = c.Why
				}
			}
		}
		if found != tc.why {
			t.Fatalf("%s: %s = %q, want %q", tc.fn, tc.expr, found, tc.why)
		}
	}
}
