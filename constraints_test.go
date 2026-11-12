package main

import (
	"runtime"
	"testing"
)

// A file excluded by a build constraint must not be parsed: its routes do not
// ship on this platform, and the loader has to say it skipped the file.
func TestBuildConstraintsSkipInactiveFiles(t *testing.T) {
	dir := t.TempDir()
	otherOS := "windows"
	if runtime.GOOS == "windows" {
		otherOS = "linux"
	}

	writeDiagFile(t, dir, "go.mod", "module example.com/constraints\n\ngo 1.22\n")
	writeDiagFile(t, dir, "main.go", `package app

import "net/http"

func home(w http.ResponseWriter, r *http.Request) {}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/base", home)
}
`)
	writeDiagFile(t, dir, "routes_"+otherOS+".go", `package app

import "net/http"

func setupForeignPlatform() {
	mux := http.NewServeMux()
	mux.HandleFunc("/foreign-platform", func(http.ResponseWriter, *http.Request) {})
}
`)
	writeDiagFile(t, dir, "ignored.go", `//go:build ignore

package app

import "net/http"

func setupIgnored() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ignored", func(http.ResponseWriter, *http.Request) {})
}
`)

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Diag.Parsed != 1 {
		t.Fatalf("parsed = %d, want 1 (main.go only): %+v", m.Diag.Parsed, m.Diag)
	}
	if m.Diag.Skipped != 2 || m.Diag.Build != 2 {
		t.Fatalf("skipped = %d (build %d), want 2 build skips: %+v", m.Diag.Skipped, m.Diag.Build, m.Diag)
	}
	want := map[string]bool{"routes_" + otherOS + ".go": true, "ignored.go": true}
	for _, s := range m.Diag.Skip {
		if s.Reason != "build" {
			t.Fatalf("skip reason = %q, want build: %+v", s.Reason, s)
		}
		if !want[s.Path] {
			t.Fatalf("unexpected skipped file %q", s.Path)
		}
		delete(want, s.Path)
	}
	if len(want) != 0 {
		t.Fatalf("missing skip entries: %v (got %+v)", want, m.Diag.Skip)
	}

	got := map[string]bool{}
	for _, r := range buildRoutes(m) {
		got[r.Path] = true
	}
	if !got["/base"] {
		t.Fatalf("base route missing: %v", got)
	}
	for _, phantom := range []string{"/foreign-platform", "/ignored"} {
		if got[phantom] {
			t.Fatalf("route %s came from an inactive file: %v", phantom, got)
		}
	}
}
