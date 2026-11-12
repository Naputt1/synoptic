package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decodeOpenAPI(t *testing.T, d JData) map[string]any {
	t.Helper()
	blob, err := buildOpenAPI(d)
	if err != nil {
		t.Fatalf("buildOpenAPI: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("OpenAPI is not valid JSON: %v\n%s", err, blob)
	}
	return doc
}

func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want an object, got %T (%v)", v, v)
	}
	return m
}

func arr(t *testing.T, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("want an array, got %T (%v)", v, v)
	}
	return a
}

func pathItem(t *testing.T, doc map[string]any, path string) map[string]any {
	t.Helper()
	return obj(t, obj(t, doc["paths"])[path])
}

func TestOpenAPIPathConversion(t *testing.T) {
	cases := []struct {
		in       string
		want     string
		wildcard bool
	}{
		{"/users/:id", "/users/{id}", false},
		{"/users/{id}", "/users/{id}", false},
		{"/files/*path", "/files/{path}", true},
		{"/users/:id/files/*path", "/users/{id}/files/{path}", true},
		{"/static", "/static", false},
	}
	for _, c := range cases {
		got, wildcard := openAPIPath(c.in)
		if got != c.want || wildcard != c.wildcard {
			t.Errorf("openAPIPath(%q) = %q, %v; want %q, %v", c.in, got, wildcard, c.want, c.wildcard)
		}
	}
}

func exportFixture() JData {
	return JData{
		Repo: "demo",
		Diag: Diag{Unresolved: 3, Unknown: 7, Skipped: 2},
		Routes: []JRoute{
			{
				M: "GET", P: "/users/:id", F: "user.go", L: 20, Svc: "api",
				Chain: []JStage{
					{Name: "auth", Role: "middleware", File: "mw.go", Line: 5},
					{Name: "getUser", Role: "handler", File: "user.go", Line: 20, Doc: "Get a user — Reads one user"},
				},
				Doc: &JDoc{
					Path:  []JField{{N: "id", T: "int"}},
					Query: []JField{{N: "verbose", T: "bool"}, {N: "q", T: "string", Req: true}},
					Resp:  []JResp{{Code: "200", Keys: []string{"id", "name"}}, {Code: "404"}},
					Auth:  true,
				},
			},
			{
				M: "POST", P: "/users", Svc: "api",
				Chain: []JStage{{Name: "createUser", Role: "handler", File: "user.go", Line: 40}},
				Doc:   &JDoc{Body: []JField{{N: "name", T: "string"}}, Resp: []JResp{{Code: "201", Keys: []string{"id"}}}},
			},
			{
				M: "GET", P: "/unproven/{id}",
				Chain: []JStage{{Name: "unproven", Role: "handler"}},
				Doc:   &JDoc{},
			},
			{
				M: "GET", P: "/files/*path",
				Chain: []JStage{{Name: "file", Role: "handler"}},
				Doc:   &JDoc{Path: []JField{{N: "path", T: "string"}}},
			},
			{
				M: "DELETE", P: "/nobody",
				Chain: []JStage{{Name: "nobody", Role: "handler"}},
			},
			{
				M: "ANY", P: "/users/:id",
				Chain: []JStage{{Name: "anyUser", Role: "handler"}},
			},
			{
				M: "ANY", P: "/any-only",
				Chain: []JStage{{Name: "anyOnly", Role: "handler"}},
			},
			{
				M: "GET", P: "/a-b",
				Chain: []JStage{{Name: "dash", Role: "handler"}},
			},
			{
				M: "GET", P: "/a_b",
				Chain: []JStage{{Name: "underscore", Role: "handler"}},
			},
			{
				M: "GET", P: "/live",
				Chain: []JStage{{Name: "live", Role: "handler"}},
				St:    &JStream{K: "ws", Events: []string{"join", "leave"}},
			},
			{
				M: "GET", P: "/events",
				Chain: []JStage{{Name: "events", Role: "handler"}},
				St:    &JStream{K: "sse"},
			},
		},
	}
}

func TestOpenAPIShape(t *testing.T) {
	doc := decodeOpenAPI(t, exportFixture())

	if doc["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v, want 3.1.0", doc["openapi"])
	}
	info := obj(t, doc["info"])
	if info["title"] != "demo API" {
		t.Errorf("title = %v, want demo API", info["title"])
	}
	if info["version"] != version {
		t.Errorf("version = %v, want %q", info["version"], version)
	}
	if !strings.Contains(info["description"].(string), "Evidence-based") {
		t.Errorf("description = %v, want the evidence note", info["description"])
	}
	if _, ok := doc["servers"]; ok {
		t.Error("servers must be left for the user")
	}

	// Path and query parameters keep their proven types.
	get := obj(t, pathItem(t, doc, "/users/{id}")["get"])
	params := arr(t, get["parameters"])
	seen := map[string]map[string]any{}
	for _, p := range params {
		pm := obj(t, p)
		seen[pm["name"].(string)] = pm
	}
	if p := seen["id"]; p["in"] != "path" || p["required"] != true || obj(t, p["schema"])["type"] != "integer" {
		t.Errorf("id parameter = %v, want required integer path", p)
	}
	if p := seen["verbose"]; p["in"] != "query" || p["required"] != nil || obj(t, p["schema"])["type"] != "boolean" {
		t.Errorf("verbose parameter = %v, want optional boolean query", p)
	}
	if p := seen["q"]; p["required"] != true || obj(t, p["schema"])["type"] != "string" {
		t.Errorf("q parameter = %v, want required string query", p)
	}
	if get["summary"] != "Get a user" || get["description"] != "Reads one user" {
		t.Errorf("summary/description = %v / %v", get["summary"], get["description"])
	}
	if get["operationId"] != "get_users_id" {
		t.Errorf("operationId = %v, want get_users_id", get["operationId"])
	}

	// Body only where the source proved one.
	if _, ok := get["requestBody"]; ok {
		t.Error("GET carries a request body it never proved")
	}
	post := obj(t, pathItem(t, doc, "/users")["post"])
	body := obj(t, obj(t, post["requestBody"])["content"])
	schema := obj(t, obj(t, body["application/json"])["schema"])
	if props := obj(t, schema["properties"]); obj(t, props["name"])["type"] != "string" {
		t.Errorf("body property name = %v, want string", props["name"])
	}

	// Responses come from observed codes and literal keys.
	responses := obj(t, get["responses"])
	ok := obj(t, responses["200"])
	if ok["description"] != "status observed in the handler" {
		t.Errorf("200 description = %v", ok["description"])
	}
	content := obj(t, obj(t, ok["content"])["application/json"])
	props := obj(t, obj(t, content["schema"])["properties"])
	for _, k := range []string{"id", "name"} {
		if _, ok := props[k]; !ok {
			t.Errorf("200 body is missing %q", k)
		}
	}
	if _, ok := obj(t, responses["404"])["content"]; ok {
		t.Error("404 invented a body")
	}
	noBody := obj(t, pathItem(t, doc, "/nobody")["delete"])
	if _, ok := obj(t, noBody["responses"])["default"]; !ok {
		t.Error("a route with no observed status needs a default response")
	}

	// Handler, middleware, service and auth carry the proven chain.
	x := obj(t, get["x-synoptic"])
	handler := obj(t, x["handler"])
	if handler["name"] != "getUser" || handler["file"] != "user.go" || handler["line"] != float64(20) {
		t.Errorf("handler = %v", handler)
	}
	mw := arr(t, x["middleware"])
	if len(mw) != 1 || mw[0] != "auth" {
		t.Errorf("middleware = %v, want [auth]", mw)
	}
	if x["service"] != "api" || x["auth"] != true {
		t.Errorf("x-synoptic service/auth = %v / %v", x["service"], x["auth"])
	}

	// Wildcards and unproven names stay visible as extensions.
	if obj(t, pathItem(t, doc, "/files/{path}")["x-synoptic"])["wildcard"] != true {
		t.Error("wildcard path item is not marked")
	}
	unproven := obj(t, obj(t, pathItem(t, doc, "/unproven/{id}")["get"])["x-synoptic"])
	if unproven["unprovenPathParams"] != true {
		t.Error("an unproven path parameter must be marked")
	}

	// ANY is skipped next to a concrete method, kept when it is the only one,
	// and reported rather than dropped.
	top := obj(t, doc["x-synoptic"])
	skipped := arr(t, top["skipped"])
	if len(skipped) != 1 || obj(t, skipped[0])["path"] != "/users/:id" {
		t.Errorf("skipped = %v, want the ANY route at /users/:id", skipped)
	}
	if _, ok := pathItem(t, doc, "/any-only")["x-synoptic-any"]; !ok {
		t.Error("an ANY-only path lost its evidence")
	}

	// Operation ids are sanitized and unique.
	dash := obj(t, pathItem(t, doc, "/a-b")["get"])["operationId"]
	under := obj(t, pathItem(t, doc, "/a_b")["get"])["operationId"]
	if dash == under || under != "get_a_b_2" {
		t.Errorf("operation ids = %v / %v, want a unique pair", dash, under)
	}

	// Streams add their protocol response and keep the event names.
	ws := obj(t, pathItem(t, doc, "/live")["get"])
	stream := obj(t, obj(t, ws["x-synoptic"])["stream"])
	if stream["kind"] != "ws" {
		t.Errorf("ws stream = %v", stream)
	}
	if events := arr(t, stream["events"]); len(events) != 2 || events[0] != "join" {
		t.Errorf("ws events = %v", events)
	}
	if _, ok := obj(t, ws["responses"])["101"]; !ok {
		t.Error("ws route has no 101 response")
	}
	sse := obj(t, pathItem(t, doc, "/events")["get"])
	if _, ok := obj(t, obj(t, sse["responses"])["200"])["content"].(map[string]any)["text/event-stream"]; !ok {
		t.Error("sse route has no text/event-stream response")
	}

	diag := obj(t, top["diag"])
	if diag["blind"] != float64(3) || diag["unknown"] != float64(7) || diag["skipped"] != float64(2) {
		t.Errorf("diag = %v", diag)
	}
}

func TestOpenAPIDeterministic(t *testing.T) {
	d := exportFixture()
	a, err := buildOpenAPI(d)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildOpenAPI(d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two OpenAPI renders differ")
	}
}

func markdownFixture() JData {
	return JData{
		Repo: "demo",
		Diag: Diag{Unresolved: 3, Unknown: 7, Skipped: 2},
		Change: &JChange{
			Ref:      "main",
			Files:    3,
			Added:    []JRouteKey{{M: "GET", P: "/new"}},
			Removed:  []JRouteKey{{M: "POST", P: "/old"}},
			Modified: []JRouteMod{{M: "GET", P: "/x", Why: []string{"middleware", "handler"}}},
		},
		Routes: []JRoute{
			{
				M: "GET", P: "/x", Svc: "api",
				Chain: []JStage{
					{Name: "mw|one", Role: "middleware"},
					{Name: "handle", Role: "handler"},
				},
			},
		},
	}
}

func TestMarkdownChanges(t *testing.T) {
	md := string(buildMarkdown(markdownFixture()))
	for _, want := range []string{
		"# demo — route inventory",
		"## Changes vs main",
		"1 added · 1 removed · 1 modified · 3 files touched",
		"### Added\n\n| Method | Path | Why |\n| --- | --- | --- |\n| GET | /new | new |",
		"### Removed\n\n| Method | Path | Why |\n| --- | --- | --- |\n| POST | /old | removed |",
		"### Modified\n\n| Method | Path | Why |\n| --- | --- | --- |\n| GET | /x | middleware, handler |",
		"<details><summary>All routes</summary>",
		"| GET | /x | handle | mw\\|one |  | api |",
		"Blind spots: 3 · Unknown calls: 7 · Skipped files: 2",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown is missing %q\n%s", want, md)
		}
	}
}

func TestMarkdownWithoutChange(t *testing.T) {
	d := markdownFixture()
	d.Change = nil
	md := string(buildMarkdown(d))
	if strings.Contains(md, "## Changes") {
		t.Errorf("markdown invented a change section:\n%s", md)
	}
	if strings.Contains(md, "<details>") {
		t.Errorf("route table is collapsed without a change section:\n%s", md)
	}
	if !strings.Contains(md, "## Routes\n\n| Method | Path | Handler | Middleware | Stream | Service |") {
		t.Errorf("route table is missing:\n%s", md)
	}
}

func TestMarkdownDeterministic(t *testing.T) {
	d := markdownFixture()
	if !bytes.Equal(buildMarkdown(d), buildMarkdown(d)) {
		t.Error("two Markdown renders differ")
	}
}

func captureStdout(t *testing.T, fn func()) []byte {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	defer func() { os.Stdout = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	return <-done
}

func TestWriteExportStdoutMatchesFile(t *testing.T) {
	blob := buildMarkdown(markdownFixture())
	stdout := captureStdout(t, func() {
		if err := writeExport("-", blob); err != nil {
			t.Fatalf("writeExport(-): %v", err)
		}
	})
	path := filepath.Join(t.TempDir(), "nested", "routes.md")
	if err := writeExport(path, blob); err != nil {
		t.Fatalf("writeExport(file): %v", err)
	}
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout, file) || !bytes.Equal(file, blob) {
		t.Errorf("stdout and file differ: stdout %d bytes, file %d bytes, source %d bytes",
			len(stdout), len(file), len(blob))
	}
}
