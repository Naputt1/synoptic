package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A handler received as a parameter must resolve to its declared receiver type,
// not the first method with that name in the module.
func TestRouteHandlerMethodUsesParameterType(t *testing.T) {
	dir := t.TempDir()
	body := `package bind

type Router struct{}

type Service struct{}

func (s *Service) Update() {}

type Handler struct{}

func (h *Handler) Update() {}

func setup(r *Router, h *Handler) {
	r.POST("/store/update", h.Update)
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/bind\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bind.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	routes := buildRoutes(m)
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}
	chain := routes[0].Chain
	if len(chain) != 1 || chain[0].Fn == nil {
		t.Fatalf("chain = %+v, want the handler", chain)
	}
	if chain[0].Fn.Recv != "Handler" {
		t.Fatalf("handler receiver = %q, want Handler", chain[0].Fn.Recv)
	}
	if routes[0].Method != "POST" {
		t.Fatalf("method = %q, want POST", routes[0].Method)
	}
}
