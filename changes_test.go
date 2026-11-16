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

	changed, files, more, err := changedFilesFor(dir, worktreeSpec("HEAD"))
	if err != nil {
		t.Fatalf("changedFilesFor: %v", err)
	}
	if !changed["main.go"] {
		t.Fatalf("changed = %v, want main.go", changed)
	}
	if len(files) != 1 || files[0].Path != "main.go" || files[0].Status != "M" {
		t.Fatalf("files = %+v, want main.go M", files)
	}
	if more != 0 {
		t.Fatalf("filesMore = %d, want 0", more)
	}

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))
	markChanges(&d, changed, worktreeSpec("HEAD"), files, more, nil, 0)
	if d.Change == nil {
		t.Fatal("no change summary")
	}
	if d.Change.Files != 1 || d.Change.Routes != 1 {
		t.Fatalf("change = %+v, want 1 file / 1 route", d.Change)
	}
	if d.Change.Mode != "worktree" || d.Change.Ref != "HEAD" {
		t.Fatalf("change = %+v, want a working-tree comparison against HEAD", d.Change)
	}
	if len(d.Routes) != 1 || !d.Routes[0].Chg {
		t.Fatalf("route not marked changed: %+v", d.Routes)
	}
	if !d.Routes[0].Chain[len(d.Routes[0].Chain)-1].Chg {
		t.Fatalf("handler stage not marked changed: %+v", d.Routes[0].Chain)
	}
}

// A literal commit range reports only the files committed between the two
// refs, not later uncommitted edits.
func TestChangedFilesRange(t *testing.T) {
	dir := changeFixture(t)
	writeChangeFixture(t, dir, "main.go", `package changed

import "net/http"

func handler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("second"))
}

func main() {
	http.HandleFunc("/ping", handler)
}
`)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "second")

	spec := rangeSpec("HEAD~1", "HEAD")
	changed, files, more, err := changedFilesFor(dir, spec)
	if err != nil {
		t.Fatalf("changedFilesFor: %v", err)
	}
	if !changed["main.go"] || len(changed) != 1 {
		t.Fatalf("changed = %v, want main.go only", changed)
	}
	if len(files) != 1 || files[0].Status != "M" || more != 0 {
		t.Fatalf("files = %+v / %d, want main.go M", files, more)
	}

	commits, commitsMore, err := compareCommits(dir, spec)
	if err != nil {
		t.Fatalf("compareCommits: %v", err)
	}
	if len(commits) != 1 || commits[0].Subject != "second" || commitsMore != 0 {
		t.Fatalf("commits = %+v / %d, want the second commit", commits, commitsMore)
	}

	// An uncommitted edit is outside a commit range.
	writeChangeFixture(t, dir, "main.go", "package changed\n")
	changed, _, _, err = changedFilesFor(dir, rangeSpec("HEAD", "HEAD"))
	if err != nil {
		t.Fatalf("changedFilesFor (HEAD..HEAD): %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("HEAD..HEAD changed = %v, want none", changed)
	}
}

func TestChangedFilesCleanTree(t *testing.T) {
	dir := changeFixture(t)
	changed, files, more, err := changedFilesFor(dir, worktreeSpec("HEAD"))
	if err != nil {
		t.Fatalf("changedFilesFor: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want none", changed)
	}
	if len(files) != 0 || more != 0 {
		t.Fatalf("files = %+v / %d, want none", files, more)
	}
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))
	markChanges(&d, changed, worktreeSpec("HEAD"), files, more, nil, 0)
	if d.Change == nil || d.Change.Routes != 0 {
		t.Fatalf("change = %+v, want zero routes", d.Change)
	}
	if d.Routes[0].Chg {
		t.Fatal("clean route marked changed")
	}
}
