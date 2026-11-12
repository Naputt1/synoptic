package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// materializeRef checks the ref out into a temporary worktree, maps a
// subdirectory root into the same relative path, and cleans everything up.
func TestMaterializeRef(t *testing.T) {
	dir := changeFixture(t)
	writeChangeFixture(t, dir, "app/go.mod", "module example.com/app\n\ngo 1.22\n")
	writeChangeFixture(t, dir, "app/main.go", "package app\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "app")

	base, cleanup, err := materializeRef(dir, "HEAD")
	if err != nil {
		t.Fatalf("materializeRef: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "main.go")); err != nil {
		t.Fatalf("base tree missing main.go: %v", err)
	}

	sub, subCleanup, err := materializeRef(filepath.Join(dir, "app"), "HEAD")
	if err != nil {
		t.Fatalf("materializeRef subdir: %v", err)
	}
	if filepath.Base(sub) != "app" {
		t.Fatalf("base root = %s, want the app subdirectory", sub)
	}
	if _, err := os.Stat(filepath.Join(sub, "main.go")); err != nil {
		t.Fatalf("subdir base tree missing main.go: %v", err)
	}
	subCleanup()
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatalf("subdir worktree still exists after cleanup: %v", err)
	}

	cleanup()
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists after cleanup: %v", err)
	}

	if _, _, err := materializeRef(dir, "no-such-ref"); err == nil {
		t.Fatal("materializeRef accepted an invalid ref")
	}
}

func syntheticStage(name, role, key string) JStage {
	return JStage{Name: name, Role: role, K: key}
}

func syntheticRoute(method, path string, chain ...JStage) JRoute {
	return JRoute{M: method, P: path, Chain: chain}
}

// A route that keeps its identity becomes modified; one that appears or
// disappears is added or removed; an unchanged route stays out of the diff.
func TestDiffRoutesSynthetic(t *testing.T) {
	base := JData{Routes: []JRoute{
		syntheticRoute("GET", "/same",
			syntheticStage("auth", "middleware", "pkg|auth|Wrap"),
			syntheticStage("same", "handler", "pkg|same|Handle")),
		syntheticRoute("GET", "/gone", syntheticStage("gone", "handler", "pkg|gone|Handle")),
		syntheticRoute("GET", "/mw",
			syntheticStage("old", "middleware", "pkg|old|Wrap"),
			syntheticStage("mw", "handler", "pkg|mw|Handle")),
		syntheticRoute("GET", "/handler", syntheticStage("old", "handler", "pkg|old|Handle")),
		syntheticRoute("GET", "/stream", syntheticStage("stream", "handler", "pkg|stream|Handle")),
	}}
	head := JData{Routes: []JRoute{
		syntheticRoute("GET", "/same",
			syntheticStage("auth", "middleware", "pkg|auth|Wrap"),
			syntheticStage("same", "handler", "pkg|same|Handle")),
		syntheticRoute("GET", "/new", syntheticStage("new", "handler", "pkg|new|Handle")),
		syntheticRoute("GET", "/mw",
			syntheticStage("next", "middleware", "pkg|next|Wrap"),
			syntheticStage("mw", "handler", "pkg|mw|Handle")),
		syntheticRoute("GET", "/handler", syntheticStage("new", "handler", "pkg|new|Handle")),
		{M: "GET", P: "/stream", Pr: "sse", Chain: []JStage{
			syntheticStage("stream", "handler", "pkg|stream|Handle"),
		}},
	}}

	c := diffRoutes(base, head)
	if len(c.Added) != 1 || c.Added[0].P != "/new" {
		t.Fatalf("added = %+v, want GET /new", c.Added)
	}
	if len(c.Removed) != 1 || c.Removed[0].P != "/gone" {
		t.Fatalf("removed = %+v, want GET /gone", c.Removed)
	}
	why := map[string][]string{}
	for _, m := range c.Modified {
		why[m.P] = m.Why
	}
	if got := strings.Join(why["/mw"], ","); got != "middleware" {
		t.Fatalf("/mw why = %q, want middleware", got)
	}
	if got := strings.Join(why["/handler"], ","); got != "handler" {
		t.Fatalf("/handler why = %q, want handler", got)
	}
	if got := strings.Join(why["/stream"], ","); got != "stream" {
		t.Fatalf("/stream why = %q, want stream", got)
	}
	if _, ok := why["/same"]; ok {
		t.Fatal("identical route reported as modified")
	}
	if c.Routes != 4 {
		t.Fatalf("routes = %d, want 4 new or modified", c.Routes)
	}

	applyRouteDiff(&head, base)
	mark := map[string]JRoute{}
	for _, r := range head.Routes {
		mark[r.P] = r
	}
	if !mark["/new"].New || !mark["/new"].Chg {
		t.Fatalf("/new = %+v, want new and changed", mark["/new"])
	}
	if !mark["/mw"].Chg || !mark["/mw"].Chain[0].Chg {
		t.Fatalf("/mw = %+v, want route and middleware marked", mark["/mw"])
	}
	if mark["/same"].Chg || mark["/same"].New {
		t.Fatalf("/same = %+v, want no mark", mark["/same"])
	}
}

// End to end: the base worktree and the edited working tree are analyzed with
// the same pipeline, and the diff describes the edit.
func TestRouteDiffEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeChangeFixture(t, dir, "go.mod", "module example.com/diff\n\ngo 1.22\n")
	writeChangeFixture(t, dir, "main.go", `package diff

import "net/http"

type Router struct{}

func (r *Router) Use(mw func(http.Handler) http.Handler) {}
func (r *Router) Get(path string, h http.HandlerFunc)  {}

func ping(w http.ResponseWriter, r *http.Request)   { w.Write([]byte("pong")) }
func item(w http.ResponseWriter, r *http.Request)   { w.Write([]byte("item")) }
func create(w http.ResponseWriter, r *http.Request) { w.Write([]byte("new")) }
func logger(next http.Handler) http.Handler         { return next }

func setup(r *Router) {
	r.Get("/ping", ping)
	r.Get("/item", item)
}
`)
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "test")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "base")

	writeChangeFixture(t, dir, "main.go", `package diff

import "net/http"

type Router struct{}

func (r *Router) Use(mw func(http.Handler) http.Handler) {}
func (r *Router) Get(path string, h http.HandlerFunc)  {}

func ping(w http.ResponseWriter, r *http.Request)   { w.Write([]byte("pong")) }
func item(w http.ResponseWriter, r *http.Request)   { w.Write([]byte("item")) }
func create(w http.ResponseWriter, r *http.Request) { w.Write([]byte("new")) }
func logger(next http.Handler) http.Handler         { return next }

func setup(r *Router) {
	r.Use(logger)
	r.Get("/ping", ping)
	r.Get("/new", create)
}
`)

	baseRoot, cleanup, err := materializeRef(dir, "HEAD")
	if err != nil {
		t.Fatalf("materializeRef: %v", err)
	}
	defer cleanup()

	base, err := analyzeRoot(baseRoot)
	if err != nil {
		t.Fatalf("analyze base: %v", err)
	}
	head, err := analyzeRoot(dir)
	if err != nil {
		t.Fatalf("analyze head: %v", err)
	}
	if len(base.Routes) != 2 || len(head.Routes) != 2 {
		t.Fatalf("route counts base=%d head=%d, want 2 each: %+v / %+v",
			len(base.Routes), len(head.Routes), base.Routes, head.Routes)
	}

	c := diffRoutes(base, head)
	if len(c.Added) != 1 || c.Added[0].P != "/new" {
		t.Fatalf("added = %+v, want GET /new", c.Added)
	}
	if len(c.Removed) != 1 || c.Removed[0].P != "/item" {
		t.Fatalf("removed = %+v, want GET /item", c.Removed)
	}
	if len(c.Modified) != 1 || c.Modified[0].P != "/ping" {
		t.Fatalf("modified = %+v, want GET /ping", c.Modified)
	}
	if got := strings.Join(c.Modified[0].Why, ","); got != "middleware" {
		t.Fatalf("/ping why = %q, want middleware", got)
	}
	if c.Routes != 2 {
		t.Fatalf("routes = %d, want 2 new or modified", c.Routes)
	}
}

// A root that is not a git work tree warns and keeps the file-touch view; the
// route data still describes the current tree.
func TestNoGitKeepsFileView(t *testing.T) {
	dir := t.TempDir()
	writeChangeFixture(t, dir, "go.mod", "module example.com/nogit\n\ngo 1.22\n")
	writeChangeFixture(t, dir, "main.go", `package nogit

import "net/http"

func handler(w http.ResponseWriter, r *http.Request) {}

func main() {
	http.HandleFunc("/ping", handler)
}
`)

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))
	var warn bytes.Buffer
	applyBaseView(&d, dir, "HEAD", &warn)

	if len(d.Routes) != 1 {
		t.Fatalf("routes = %d, want the head route", len(d.Routes))
	}
	if d.Routes[0].Chg {
		t.Fatal("route marked changed without a base")
	}
	if d.Change != nil {
		t.Fatalf("change = %+v, want none outside git", d.Change)
	}
	if !strings.Contains(warn.String(), "base:") {
		t.Fatalf("warning = %q, want a base warning", warn.String())
	}
}
