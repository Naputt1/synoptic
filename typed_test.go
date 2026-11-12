package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// typedFixture is a self-contained module: svc declares a method on an embedded
// Base, helper is a package whose name (svc) differs from its directory so the
// AST import map cannot name it, and app registers a net/http handler that calls
// both. No import reaches outside the module, so packages.Load needs no network.
const typedFixture = `module svc

go 1.22
`

func writeTypedFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", typedFixture)
	write("svc.go", `package svc

type Base struct{}

func (Base) Ping() {}

type API struct{ Base }
`)
	write("helper/helper.go", `package svc

func Helper() {}
`)
	write("app/app.go", `package app

import (
	"net/http"

	"svc"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	api := svc.API{}
	api.Ping()
	w.WriteHeader(http.StatusOK)
}
`)
	write("use/use.go", `package use

import "svc/helper"

func Call() {
	svc.Helper()
}
`)
	write("cmd/main.go", `package main

import (
	"net/http"

	"svc/app"
)

func main() {
	http.HandleFunc("/ping", app.Handler)
}
`)
	return dir
}

func findCall(t *testing.T, f *Func, expr string) *Call {
	t.Helper()
	if f == nil {
		t.Fatalf("missing function")
	}
	for i := range f.Calls {
		if f.Calls[i].Expr == expr {
			return &f.Calls[i]
		}
	}
	t.Fatalf("%s: call %q not found in %+v", f.Key, expr, f.Calls)
	return nil
}

// A method promoted from an embedded type has no receiver of its own, so the
// AST pass cannot name it. The type checker can.
func TestTypedResolvesPromotedMethod(t *testing.T) {
	dir := writeTypedFixture(t)

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	handler := m.Funcs["svc/app|Handler"]
	if handler == nil {
		t.Fatal("missing app.Handler")
	}
	if c := findCall(t, handler, "api.Ping"); c.Target != nil {
		t.Fatalf("AST mode already resolved api.Ping to %s, fixture no longer exercises typed mode", c.Target.Key)
	}

	n, err := resolveTypedCalls(m, dir)
	if err != nil {
		t.Fatalf("resolveTypedCalls: %v", err)
	}
	if n == 0 {
		t.Fatal("typed mode resolved no calls")
	}
	c := findCall(t, handler, "api.Ping")
	if c.Target == nil {
		t.Fatal("api.Ping still unresolved after typed mode")
	}
	if c.Target.Key != "svc|Base|Ping" {
		t.Fatalf("api.Ping resolved to %q, want svc|Base|Ping", c.Target.Key)
	}
	if c.PkgPath != "svc" {
		t.Fatalf("PkgPath = %q, want svc", c.PkgPath)
	}
}

// A package whose name differs from its directory is invisible to the AST
// import map, while the type checker knows the real package name.
func TestTypedResolvesPackageCall(t *testing.T) {
	dir := writeTypedFixture(t)

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	call := m.Funcs["svc/use|Call"]
	if call == nil {
		t.Fatal("missing use.Call")
	}
	c := findCall(t, call, "svc.Helper")
	if c.Target != nil {
		t.Fatalf("AST mode already resolved svc.Helper to %s, fixture no longer exercises typed mode", c.Target.Key)
	}

	if _, err := resolveTypedCalls(m, dir); err != nil {
		t.Fatalf("resolveTypedCalls: %v", err)
	}
	c = findCall(t, call, "svc.Helper")
	if c.Target == nil {
		t.Fatal("svc.Helper still unresolved after typed mode")
	}
	if c.Target.Key != "svc/helper|Helper" {
		t.Fatalf("svc.Helper resolved to %q, want svc/helper|Helper", c.Target.Key)
	}
}

// Typed mode must never touch the default output: with the flag off the JSON
// is the same as a fresh AST-only load, and the new target is absent.
func TestTypedOffKeepsASTJSON(t *testing.T) {
	dir := writeTypedFixture(t)

	load := func() ([]byte, *Module) {
		m, err := loadModule(dir)
		if err != nil {
			t.Fatal(err)
		}
		routes := buildRoutes(m)
		if len(routes) == 0 {
			t.Fatal("fixture route not found")
		}
		raw, err := json.Marshal(buildData(m, routes))
		if err != nil {
			t.Fatal(err)
		}
		return raw, m
	}

	first, _ := load()
	second, _ := load()
	if !bytes.Equal(first, second) {
		t.Fatal("AST mode JSON is not deterministic")
	}
	if bytes.Contains(first, []byte(`"Pos"`)) {
		t.Fatal("call position leaked into the serialized output")
	}

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	astData := buildData(m, buildRoutes(m))
	if flowHasKey(astData.Flows["svc/app|Handler"].Steps, "svc|Base|Ping") {
		t.Fatal("AST mode flow contains the promoted target; the default output changed")
	}

	// The same fixture with typed mode on does carry the target, proving the
	// absence above is the unresolved call and not a handler that never baked.
	if _, err := resolveTypedCalls(m, dir); err != nil {
		t.Fatalf("resolveTypedCalls: %v", err)
	}
	typedData := buildData(m, buildRoutes(m))
	if !flowHasKey(typedData.Flows["svc/app|Handler"].Steps, "svc|Base|Ping") {
		t.Fatal("typed mode flow does not contain the promoted target")
	}
}

func flowHasKey(steps []Step, key string) bool {
	for _, s := range steps {
		if s.Key == key || flowHasKey(s.Body, key) || flowHasKey(s.Else, key) {
			return true
		}
	}
	return false
}
