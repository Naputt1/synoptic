package main

import (
	"os"
	"path/filepath"
	"testing"
)

func multiEntryFixture(t *testing.T) string {
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
	write("go.mod", "module example.com/labor\n\ngo 1.22\n")
	service := func(path, handler string) string {
		return `package main

type Router struct{}

type Route struct{}

func (r *Router) GET(p string, h func()) *Route { return &Route{} }

func ` + handler + `() {}

func setup(r *Router) {
	r.GET("` + path + `", ` + handler + `)
}

func main() {
	setup(&Router{})
}
`
	}
	write("jobs/cmd/main.go", service("/jobs", "listJobs"))
	write("users/cmd/main.go", service("/users", "listUsers"))
	write("main.go", service("/health", "health"))
	return dir
}

func TestServicesSplitByEntryPoint(t *testing.T) {
	m, err := loadModule(multiEntryFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))

	got := map[string]JSvc{}
	for _, s := range d.Svcs {
		got[s.N] = s
	}
	if len(got) != 3 {
		t.Fatalf("services = %+v, want jobs, labor and users", d.Svcs)
	}
	for name, routes := range map[string]int{"jobs": 1, "labor": 1, "users": 1} {
		s, ok := got[name]
		if !ok {
			t.Fatalf("missing service %q in %+v", name, d.Svcs)
		}
		if s.Routes != routes {
			t.Fatalf("%s routes = %d, want %d", name, s.Routes, routes)
		}
	}
	if got["jobs"].E != "jobs/cmd/main.go" || got["jobs"].D != "jobs" {
		t.Fatalf("jobs = %+v, want dir jobs and its cmd entry", got["jobs"])
	}

	svcOf := map[string]string{}
	for _, r := range d.Routes {
		svcOf[r.P] = r.Svc
	}
	if svcOf["/jobs"] != "jobs" || svcOf["/users"] != "users" || svcOf["/health"] != "labor" {
		t.Fatalf("routes tagged %+v, want jobs/users/labor", svcOf)
	}
}

func TestSingleEntryStaysUnsplit(t *testing.T) {
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
	write("go.mod", "module example.com/one\n\ngo 1.22\n")
	write("main.go", `package main

type Router struct{}

type Route struct{}

func (r *Router) GET(p string, h func()) *Route { return &Route{} }

func handler() {}

func setup(r *Router) {
	r.GET("/x", handler)
}

func main() {
	setup(&Router{})
}
`)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))
	if len(d.Svcs) != 0 {
		t.Fatalf("svcs = %+v, want none for a single entry point", d.Svcs)
	}
	if len(d.Routes) != 1 || d.Routes[0].Svc != "" {
		t.Fatalf("routes = %+v, want one untagged route", d.Routes)
	}
}

func TestEntryPointWithoutSurfaceIsDropped(t *testing.T) {
	dir := multiEntryFixture(t)
	if err := os.MkdirAll(filepath.Join(dir, "spare/cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spare/cmd/main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))
	attachServices(m, &d, nil)
	if len(d.Svcs) != 3 {
		t.Fatalf("svcs = %+v, want the three entry points that own routes", d.Svcs)
	}
	for _, s := range d.Svcs {
		if s.N == "spare" {
			t.Fatalf("spare has no route or rpc, want it dropped: %+v", d.Svcs)
		}
	}
}

func TestRoutesOutsideEveryEntryKeepOneSurface(t *testing.T) {
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
	write("go.mod", "module example.com/central\n\ngo 1.22\n")
	write("cmd/api/main.go", "package main\n\nfunc main() {}\n")
	write("cmd/worker/main.go", "package main\n\nfunc main() {}\n")
	write("internal/http/routes.go", `package http

type Router struct{}

type Route struct{}

func (r *Router) GET(p string, h func()) *Route { return &Route{} }

func handler() {}

func setup(r *Router) {
	r.GET("/x", handler)
}
`)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))
	if len(d.Routes) != 1 {
		t.Fatalf("routes = %+v, want the one registered route", d.Routes)
	}
	if len(d.Svcs) != 0 {
		t.Fatalf("svcs = %+v, want none: the route belongs to no entry point", d.Svcs)
	}
	if d.Routes[0].Svc != "" {
		t.Fatalf("route svc = %q, want untagged", d.Routes[0].Svc)
	}
}
