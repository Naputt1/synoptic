package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDiagFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A file the parser rejects must be listed, not silently dropped.
func TestLoadRecordsParseErrors(t *testing.T) {
	dir := t.TempDir()
	writeDiagFile(t, dir, "go.mod", "module example.com/broken\n\ngo 1.22\n")
	writeDiagFile(t, dir, "ok.go", "package broken\n\nfunc ok() {}\n")
	writeDiagFile(t, dir, "broken.go", "package broken\n\nfunc (\n")

	m, err := loadModule(dir)
	if err != nil {
		t.Fatalf("loadModule crashed on a malformed file: %v", err)
	}
	if m.Diag.Parsed != 1 {
		t.Fatalf("parsed = %d, want 1", m.Diag.Parsed)
	}
	if m.Diag.Skipped != 1 || m.Diag.Parse != 1 {
		t.Fatalf("skipped = %d (parse %d), want 1 parse skip", m.Diag.Skipped, m.Diag.Parse)
	}
	if len(m.Diag.Skip) != 1 {
		t.Fatalf("skip list = %+v, want one entry", m.Diag.Skip)
	}
	if got := m.Diag.Skip[0]; got.Path != "broken.go" || got.Reason != "parse" {
		t.Fatalf("skip = %+v, want broken.go (parse)", got)
	}
}

func diagChainFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeDiagFile(t, dir, "go.mod", "module example.com/chain\n\ngo 1.22\n")
	writeDiagFile(t, dir, "main.go", `package chain

type Router struct{}

func (r *Router) GET(p string, h func()) {}

func e() {}
func d() { e() }
func c() { d() }
func b() { c() }
func a() { b() }

func handler() { a() }

func setup(r *Router) { r.GET("/deep", handler) }

func main() { setup(&Router{}) }
`)
	return dir
}

// A chain deeper than flowDepth stops at the cap; the baked flow says so.
func TestFlowDepthTruncationIsReported(t *testing.T) {
	dir := diagChainFixture(t)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))
	if d.Diag.Truncated == 0 {
		t.Fatalf("truncated = 0, want the depth cap reported: %+v", d.Diag)
	}
	truncated := 0
	for key, jf := range d.Flows {
		if jf.Trunc {
			truncated++
			if !strings.HasPrefix(key, "example.com/chain|") {
				t.Fatalf("unexpected truncated flow %q", key)
			}
		}
	}
	if truncated == 0 {
		t.Fatalf("no flow carries Trunc: %+v", d.Flows)
	}
	if _, ok := d.Flows["example.com/chain|e"]; ok {
		t.Fatal("flow beyond the depth cap was baked; the cap did not cut the chain")
	}
}

// The diagnostics travel with the baked data, so the JSON consumer sees them.
func TestDiagIsMarshalled(t *testing.T) {
	dir := diagChainFixture(t)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))
	blob, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	out := string(blob)
	for _, key := range []string{`"diag"`, `"parsed"`, `"skipped"`, `"calls"`, `"resolved"`, `"unresolved"`, `"truncated"`} {
		if !strings.Contains(out, key) {
			t.Fatalf("json is missing %s: %s", key, out[:min(len(out), 400)])
		}
	}
}

// An in-module call with no target is a blind spot; a stdlib call is not.
func TestUnresolvedInModuleCallsAreCounted(t *testing.T) {
	dir := t.TempDir()
	writeDiagFile(t, dir, "go.mod", "module example.com/calls\n\ngo 1.22\n")
	writeDiagFile(t, dir, "main.go", `package calls

import "strings"

func known() {}

func handler() {
	known()
	missing()
	_ = strings.ToUpper("x")
}
`)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Diag.Calls != 3 {
		t.Fatalf("calls = %d, want 3: %+v", m.Diag.Calls, m.Diag)
	}
	if m.Diag.Resolved != 1 {
		t.Fatalf("resolved = %d, want 1", m.Diag.Resolved)
	}
	if m.Diag.Unresolved != 1 {
		t.Fatalf("unresolved = %d, want 1 (missing): %+v", m.Diag.Unresolved, m.Diag.Unres)
	}
	if len(m.Diag.Unres) != 1 || m.Diag.Unres[0].Expr != "missing" || m.Diag.Unres[0].Func != "handler" {
		t.Fatalf("unresolved list = %+v, want handler: missing", m.Diag.Unres)
	}
}
