package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBenchFile(b testing.TB, dir, name, body string) {
	b.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		b.Fatal(err)
	}
}

// benchModule writes a synthetic module with 200 handlers, five router groups
// and a few services, spread over the files a real service would have. It
// resembles a small repo instead of a toy file, and exercises the parallel
// parse path.
func benchModule(b testing.TB) string {
	b.Helper()
	dir := b.TempDir()

	var services strings.Builder
	services.WriteString("package bench\n\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&services, "type Service%d struct{}\n\n", i)
		for _, m := range []string{"Create", "Get", "List"} {
			fmt.Fprintf(&services, "func (s *Service%d) %s() {}\n\n", i, m)
		}
		fmt.Fprintf(&services, "var svc%d = &Service%d{}\n\n", i, i)
	}

	var handlers [4]strings.Builder
	for i := range handlers {
		handlers[i].WriteString("package bench\n\nimport \"net/http\"\n\n")
	}
	for i := 0; i < 200; i++ {
		h := &handlers[i/50]
		fmt.Fprintf(h, "func handler%03d(w http.ResponseWriter, r *http.Request) {\n\tsvc%d.Create()\n\tsvc%d.Get()\n\tsvc%d.List()\n}\n\n",
			i, i%8, (i+3)%8, (i+5)%8)
	}

	var routes strings.Builder
	routes.WriteString(`package bench

import "net/http"

type Router struct{ prefix string }

func (r *Router) Group(p string) *Router { return &Router{prefix: r.prefix + p} }

func (r *Router) GET(p string, h http.HandlerFunc) {}

func setup(r *Router) {
`)
	for g := 0; g < 5; g++ {
		fmt.Fprintf(&routes, "\tg%d := r.Group(\"/g%d\")\n", g, g)
		for i := 0; i < 40; i++ {
			n := g*40 + i
			fmt.Fprintf(&routes, "\tg%d.GET(\"/route%03d\", handler%03d)\n", g, n, n)
		}
	}
	routes.WriteString("}\n")

	writeBenchFile(b, dir, "go.mod", "module example.com/bench\n\ngo 1.22\n")
	writeBenchFile(b, dir, "services.go", services.String())
	for i := range handlers {
		writeBenchFile(b, dir, fmt.Sprintf("handlers_%d.go", i+1), handlers[i].String())
	}
	writeBenchFile(b, dir, "routes.go", routes.String())
	writeBenchFile(b, dir, "main.go", "package bench\n\nfunc main() {\n\tsetup(&Router{})\n}\n")
	return dir
}

func BenchmarkLoadModule(b *testing.B) {
	dir := benchModule(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loadModule(dir); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildRoutes(b *testing.B) {
	dir := benchModule(b)
	m, err := loadModule(dir)
	if err != nil {
		b.Fatal(err)
	}
	if got := len(buildRoutes(m)); got != 200 {
		b.Fatalf("routes = %d, want 200", got)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildRoutes(m)
	}
}
