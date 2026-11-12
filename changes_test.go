package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeChangeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func changeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeChangeFixture(t, dir, "go.mod", "module example.com/changed\n\ngo 1.22\n")
	writeChangeFixture(t, dir, "main.go", `package changed

import "net/http"

func handler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

func main() {
	http.HandleFunc("/ping", handler)
}
`)
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "test")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "base")
	return dir
}

// A handler edited since the ref marks its route, and the counters agree.
func TestChangedFilesMarksRoutes(t *testing.T) {
	dir := changeFixture(t)
	writeChangeFixture(t, dir, "main.go", `package changed

import "net/http"

func handler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("changed"))
}

func main() {
	http.HandleFunc("/ping", handler)
}
`)

	changed, err := changedFiles(dir, "HEAD")
	if err != nil {
		t.Fatalf("changedFiles: %v", err)
	}
	if !changed["main.go"] {
		t.Fatalf("changed = %v, want main.go", changed)
	}

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))
	markChanges(&d, changed, "HEAD")
	if d.Change == nil {
		t.Fatal("no change summary")
	}
	if d.Change.Files != 1 || d.Change.Routes != 1 {
		t.Fatalf("change = %+v, want 1 file / 1 route", d.Change)
	}
	if len(d.Routes) != 1 || !d.Routes[0].Chg {
		t.Fatalf("route not marked changed: %+v", d.Routes)
	}
	if !d.Routes[0].Chain[len(d.Routes[0].Chain)-1].Chg {
		t.Fatalf("handler stage not marked changed: %+v", d.Routes[0].Chain)
	}
}

// A clean tree has nothing to mark, but the run still reports the ref.
func TestChangedFilesCleanTree(t *testing.T) {
	dir := changeFixture(t)
	changed, err := changedFiles(dir, "HEAD")
	if err != nil {
		t.Fatalf("changedFiles: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want none", changed)
	}
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))
	markChanges(&d, changed, "HEAD")
	if d.Change == nil || d.Change.Routes != 0 {
		t.Fatalf("change = %+v, want zero routes", d.Change)
	}
	if d.Routes[0].Chg {
		t.Fatal("clean route marked changed")
	}
}
