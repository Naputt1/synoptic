package main

import "testing"

// rectypeFixture uses declared receiver types instead of assignments: a
// parameter, an interface parameter and a package variable are all readable
// from the declarations, while tx := db.Begin() is not.
const rectypeFixture = `package rectype

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Service struct{}

func (Service) GetUser() string { return "" }

type Reader interface{ Read() string }

type File struct{}

func (File) Read() string { return "" }

type Getter interface{ Get() string }

type A struct{}

func (A) Get() string { return "" }

type B struct{}

func (B) Get() string { return "" }

var DB *gorm.DB

func handler(c *gin.Context) { c.JSON(200, nil) }

func useService(s *Service) { s.GetUser() }

func useServiceMissing(s *Service) { s.Nope() }

func useReader(s Reader) { s.Read() }

func useGetter(s Getter) { s.Get() }

func useDB() { DB.Exec("q") }

func useLocal(db *gorm.DB) {
	tx := db.Begin()
	tx.Exec("q")
}
`

func TestDeclaredReceiverTypes(t *testing.T) {
	dir := t.TempDir()
	writeDiagFile(t, dir, "go.mod", "module example.com/rectype\n\ngo 1.22\n")
	writeDiagFile(t, dir, "main.go", rectypeFixture)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		fn, expr, why, target string
	}{
		{"handler", "c.JSON", "external", ""},
		{"useService", "s.GetUser", "resolved", "example.com/rectype|Service|GetUser"},
		{"useServiceMissing", "s.Nope", "blind", ""},
		{"useReader", "s.Read", "resolved", "example.com/rectype|File|Read"},
		{"useGetter", "s.Get", "ambiguous", ""},
		{"useDB", "DB.Exec", "external", ""},
		{"useLocal", "db.Begin", "external", ""},
		{"useLocal", "tx.Exec", "unknown", ""},
	}
	for _, tc := range cases {
		call := findFuncCall(m, tc.fn, tc.expr)
		if call == nil {
			t.Fatalf("%s: no call %q", tc.fn, tc.expr)
		}
		if call.Why != tc.why {
			t.Fatalf("%s: %s why = %q, want %q", tc.fn, tc.expr, call.Why, tc.why)
		}
		if tc.target != "" {
			if call.Target == nil || call.Target.Key != tc.target {
				t.Fatalf("%s: %s target = %+v, want %s", tc.fn, tc.expr, call.Target, tc.target)
			}
		} else if call.Target != nil {
			t.Fatalf("%s: %s resolved to %s, want no target", tc.fn, tc.expr, call.Target.Key)
		}
	}

	d := m.Diag
	if d.Calls != 8 || d.Resolved != 2 || d.External != 3 || d.Ambiguous != 1 || d.Unknown != 1 || d.Unresolved != 1 {
		t.Fatalf("diag = calls %d, resolved %d, external %d, ambiguous %d, unknown %d, blind %d",
			d.Calls, d.Resolved, d.External, d.Ambiguous, d.Unknown, d.Unresolved)
	}
}

func findFuncCall(m *Module, fnName, expr string) *Call {
	for _, f := range m.Funcs {
		if f.Name != fnName {
			continue
		}
		for i := range f.Calls {
			if f.Calls[i].Expr == expr {
				return &f.Calls[i]
			}
		}
	}
	return nil
}
