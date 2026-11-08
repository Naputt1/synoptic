package main

import (
	"os"
	"path/filepath"
	"testing"
)

func dispatchFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `package dispatch

type Req struct{}

type Router struct{}

func (r *Router) GET(path string, h func(*Req) error) {}

type API interface {
	Get(*Req) error
}

type Wrapper struct {
	Handler API
}

func (w *Wrapper) Get(r *Req) error { return w.Handler.Get(r) }

func Register(api API, r *Router) {
	w := Wrapper{Handler: api}
	r.GET("/thing", w.Get)
}

type Impl struct{}

func (h *Impl) Get(*Req) error { return nil }

func setup(r *Router) { Register(&Impl{}, r) }
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/dispatch\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dispatch.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolvesInterfaceWrapperHandler(t *testing.T) {
	m, err := loadModule(dispatchFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	routes := buildRoutes(m)
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1: %+v", len(routes), routes)
	}
	d := buildData(m, routes)
	if len(routes[0].Chain) == 0 || routes[0].Chain[0].Fn == nil {
		t.Fatalf("chain = %+v, want a resolved handler", routes[0].Chain)
	}
	fn := routes[0].Chain[0].Fn
	if fn.Recv != "Impl" || fn.Name != "Get" {
		t.Fatalf("handler = %s.%s, want Impl.Get", fn.Recv, fn.Name)
	}
	if _, ok := d.Flows[fn.Key]; !ok {
		t.Fatalf("missing baked flow for %s in %v", fn.Key, d.Flows)
	}
}
