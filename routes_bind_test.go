package main

import (
	"os"
	"path/filepath"
	"testing"
)

func bindFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `package bind

type Router struct{}

type Route struct{}

func (r *Router) PUT(path string, h func()) *Route { return &Route{} }

func (r *Route) Bind(mws ...func()) *Route { return r }

func middleware() {}

func handler() {}

func setup(r *Router) {
	r.PUT("/x", handler).Bind(middleware)
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/bind\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bind.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBoundMiddlewareRunsBeforeHandler(t *testing.T) {
	m, err := loadModule(bindFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	routes := buildRoutes(m)
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1: %+v", len(routes), routes)
	}
	chain := routes[0].Chain
	if len(chain) != 2 {
		t.Fatalf("chain = %+v, want middleware and handler", chain)
	}
	if chain[0].Name != "middleware" || chain[0].Role != "middleware" {
		t.Fatalf("chain[0] = %+v, want middleware", chain[0])
	}
	if chain[1].Name != "handler" || chain[1].Role != "handler" {
		t.Fatalf("chain[1] = %+v, want handler last", chain[1])
	}
}
