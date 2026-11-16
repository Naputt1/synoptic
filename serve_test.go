package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func serveFixture(t *testing.T) string {
	t.Helper()
	dir := changeFixture(t)
	gitRun(t, dir, "branch", "-M", "main")
	return dir
}

// serveTestServer isolates PATH to git so the refs handler never reaches out to
// a real gh installation during a test.
func serveTestServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(gitBin, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	srv := httptest.NewServer(newServeApp(dir, false, false, io.Discard).handler())
	t.Cleanup(srv.Close)
	return srv
}

func getJSON(t *testing.T, url string, dst any) *http.Response {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return readJSON(t, res, dst)
}

func postJSON(t *testing.T, url, body string, dst any) *http.Response {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return readJSON(t, res, dst)
}

func readJSON(t *testing.T, res *http.Response, dst any) *http.Response {
	t.Helper()
	if dst != nil {
		if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
			t.Fatalf("decode %s: %v", res.Request.URL, err)
		}
	}
	return res
}

func TestServeRefs(t *testing.T) {
	dir := serveFixture(t)
	srv := serveTestServer(t, dir)

	var refs refsPayload
	res := getJSON(t, srv.URL+"/api/refs", &refs)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/refs status = %d", res.StatusCode)
	}
	if refs.Current != "main" {
		t.Errorf("current = %q, want main", refs.Current)
	}
	if !slices.Contains(refs.Branches, "main") {
		t.Errorf("branches = %v, want main", refs.Branches)
	}
	if len(refs.Commits) == 0 || refs.Commits[0].Subject != "base" {
		t.Errorf("commits = %+v, want the base commit", refs.Commits)
	}
	if refs.PRsError == "" {
		t.Error("prsError is empty with no gh on PATH")
	}
}

func TestServeInitial(t *testing.T) {
	dir := serveFixture(t)
	srv := serveTestServer(t, dir)

	var d JData
	res := getJSON(t, srv.URL+"/api/initial", &d)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/initial status = %d", res.StatusCode)
	}
	if len(d.Routes) != 1 || d.Routes[0].P != "/ping" {
		t.Fatalf("routes = %+v, want GET /ping", d.Routes)
	}
	if d.Change != nil {
		t.Fatalf("initial data has a comparison: %+v", d.Change)
	}
}

func TestServeCompareRange(t *testing.T) {
	dir := serveFixture(t)
	writeChangeFixture(t, dir, "main.go", `package changed

import "net/http"

func handler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("changed"))
}

func main() {
	http.HandleFunc("/ping", handler)
}
`)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "change handler")

	srv := serveTestServer(t, dir)
	var d JData
	res := postJSON(t, srv.URL+"/api/compare", `{"base":"HEAD~1","to":"HEAD"}`, &d)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/compare status = %d", res.StatusCode)
	}
	if d.Change == nil {
		t.Fatal("compare returned no change data")
	}
	if d.Change.Mode != "range" || d.Change.Ref != "HEAD~1" || d.Change.To != "HEAD" {
		t.Fatalf("change = %+v, want range HEAD~1..HEAD", d.Change)
	}
	if len(d.Change.FileList) != 1 || d.Change.FileList[0].Path != "main.go" {
		t.Fatalf("fileList = %+v, want main.go", d.Change.FileList)
	}
	if !strings.Contains(d.Change.FileList[0].Patch, "+++ b/main.go") {
		t.Fatalf("patch is missing its main.go header:\n%s", d.Change.FileList[0].Patch)
	}
}

func TestServeCompareRejectsInvalidRefs(t *testing.T) {
	dir := serveFixture(t)
	srv := serveTestServer(t, dir)

	cases := []struct {
		name string
		body string
	}{
		{name: "missing base commit", body: `{"base":"no-such-ref","to":"HEAD"}`},
		{name: "missing to commit", body: `{"base":"HEAD","to":"no-such-ref"}`},
		{name: "to without base", body: `{"to":"HEAD"}`},
		{name: "option-like ref", body: `{"base":"--help"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := postJSON(t, srv.URL+"/api/compare", c.body, nil)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", res.StatusCode)
			}
		})
	}
}

func TestLoopbackAddr(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "127.0.0.1:8099", want: "127.0.0.1:8099"},
		{in: ":8099", want: "127.0.0.1:8099"},
		{in: "localhost:8099", want: "localhost:8099"},
		{in: "[::1]:8099", want: "[::1]:8099"},
		{in: "0.0.0.0:8099", wantErr: true},
		{in: "example.com:8099", wantErr: true},
		{in: "8099", wantErr: true},
	}
	for _, c := range cases {
		got, err := loopbackAddr(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("loopbackAddr(%q) = %q, want an error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("loopbackAddr(%q): %v", c.in, err)
		} else if got != c.want {
			t.Errorf("loopbackAddr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
