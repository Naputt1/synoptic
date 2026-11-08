package main

import (
	"go/ast"
	"os"
	"path/filepath"
	"testing"
)

// A call on a field of the receiver must follow the field's type, not a
// method with the same name on the receiver itself.
func TestCallOnReceiverFieldResolvesToFieldType(t *testing.T) {
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
	write("go.mod", "module example.com/field\n\ngo 1.22\n")
	write("inner/inner.go", `package inner

type Worker struct{}

func (w *Worker) Run() error { return nil }
`)
	write("outer/outer.go", `package outer

import "example.com/field/inner"

type Server struct {
	worker *inner.Worker
}

func (s *Server) Run() error {
	return s.worker.Run()
}

func (s *Server) Start() error {
	return s.Run()
}
`)

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}

	run := m.Funcs["example.com/field/outer|Server|Run"]
	if run == nil {
		t.Fatal("missing Server.Run")
	}
	if len(run.Calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(run.Calls))
	}
	c := run.Calls[0]
	if c.Target == nil {
		t.Fatalf("s.worker.Run() did not resolve: %+v", c)
	}
	if c.Target.Recv != "Worker" || c.Target.PkgPath != "example.com/field/inner" {
		t.Fatalf("s.worker.Run() resolved to %s|%s, want inner|Worker", c.Target.PkgPath, c.Target.Recv)
	}

	// A plain receiver call still resolves to the receiver's own method.
	start := m.Funcs["example.com/field/outer|Server|Start"]
	if start == nil {
		t.Fatal("missing Server.Start")
	}
	if len(start.Calls) != 1 || start.Calls[0].Target == nil {
		t.Fatalf("s.Run() did not resolve: %+v", start.Calls)
	}
	if got := start.Calls[0].Target; got.Recv != "Server" || got.Name != "Run" {
		t.Fatalf("s.Run() resolved to %s.%s, want Server.Run", got.Recv, got.Name)
	}
}

func TestCallThroughInterfaceResolvesToImplementation(t *testing.T) {
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
	write("go.mod", "module example.com/iface\n\ngo 1.22\n")
	write("domain/store.go", `package domain

type Store interface {
	Get(id string) (string, error)
}
`)
	write("app/app.go", `package app

import "example.com/iface/domain"

type Service struct {
	store domain.Store
}

func (s *Service) Get(id string) (string, error) {
	return s.store.Get(id)
}
`)
	write("mongo/store.go", `package mongo

type Store struct{}

func (s *Store) Get(id string) (string, error) { return id, nil }
`)

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := m.Funcs["example.com/iface/app|Service|Get"]
	if svc == nil {
		t.Fatal("missing Service.Get")
	}
	if len(svc.Calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(svc.Calls))
	}
	c := svc.Calls[0]
	if c.Target == nil {
		t.Fatalf("s.store.Get() did not resolve: %+v", c)
	}
	if c.Target.PkgPath != "example.com/iface/mongo" || c.Target.Recv != "Store" {
		t.Fatalf("resolved to %s|%s, want mongo|Store", c.Target.PkgPath, c.Target.Recv)
	}
	// The return card carries the link, so the flow can be walked further.
	steps := flowSteps(svc, svc.Decl.Body.List)
	if len(steps) != 1 || steps[0].Kind != "ret" {
		t.Fatalf("steps = %+v, want one ret", steps)
	}
	if steps[0].Key != c.Target.Key {
		t.Fatalf("ret step key = %q, want %q", steps[0].Key, c.Target.Key)
	}
}

func TestMongoChainReadsAndWritesTagDatabase(t *testing.T) {
	read := Call{Base: "r", Qual: "r.db.Collection(TABLE_USER_STATUS)", Sel: "FindOne", Expr: "r.db.Collection(TABLE_USER_STATUS).FindOne(...)"}
	if got := tagOf(read); got != "db" {
		t.Fatalf("tagOf = %q, want db", got)
	}
	if k, g := effectKind(read); k != "read" || g != "db" {
		t.Fatalf("effectKind = %q/%q, want read/db", k, g)
	}
	write := Call{Base: "r", Qual: "r.db.Collection(TABLE_USER_STATUS)", Sel: "InsertOne", Expr: "r.db.Collection(TABLE_USER_STATUS).InsertOne(ctx, doc)"}
	if k, g := effectKind(write); k != "write" || g != "db" {
		t.Fatalf("effectKind = %q/%q, want write/db", k, g)
	}
	// A decode on the same chain is still part of the store call, not a method
	// of its own.
	decode := Call{Base: "r", Qual: "r.db.Collection(TABLE_USER_STATUS).FindOne(...)", Sel: "Decode", Expr: "r.db.Collection(TABLE_USER_STATUS).FindOne(...).Decode(&v)"}
	if k, _ := effectKind(decode); k != "" {
		t.Fatalf("effectKind(Decode) = %q, want none", k)
	}
}

func TestLinkRPSStepsPointsAtServiceMethod(t *testing.T) {
	idx := map[string]string{"JobService.GetActive": "1/3"}
	steps := []Step{
		{Kind: "call", Iface: "jobpb.JobServiceClient", Sel: "GetActive", Body: []Step{
			{Kind: "call", Iface: "*jobpb.JobServiceClient", Sel: "GetActive"},
		}},
		{Kind: "call", Iface: "jobpb.JobServiceClient", Sel: "Unknown"},
	}
	linkRPSSteps(steps, idx)
	if steps[0].Rpc != "1/3" {
		t.Fatalf("top-level call rpc = %q, want 1/3", steps[0].Rpc)
	}
	if steps[0].Body[0].Rpc != "1/3" {
		t.Fatalf("nested call rpc = %q, want 1/3", steps[0].Body[0].Rpc)
	}
	if steps[1].Rpc != "" {
		t.Fatalf("unknown method rpc = %q, want empty", steps[1].Rpc)
	}
}

// A field's type is written in the file that declares the struct; the file
// that calls the field may not import that package at all.
func TestFieldTypeResolvesFromDeclaringFile(t *testing.T) {
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
	write("go.mod", "module example.com/split\n\ngo 1.22\n")
	write("worker/worker.go", `package worker

type Pool struct{}

func (p *Pool) Run() error { return nil }
`)
	write("api/types.go", `package api

import "example.com/split/worker"

type Handler struct {
	pool *worker.Pool
}
`)
	write("api/handler.go", `package api

func (h *Handler) Start() error {
	return h.pool.Run()
}
`)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := m.Funcs["example.com/split/api|Handler|Start"]
	if start == nil {
		t.Fatal("missing Handler.Start")
	}
	if len(start.Calls) != 1 || start.Calls[0].Target == nil {
		t.Fatalf("h.pool.Run() did not resolve: %+v", start.Calls)
	}
	if got := start.Calls[0].Target; got.PkgPath != "example.com/split/worker" || got.Recv != "Pool" {
		t.Fatalf("resolved to %s|%s, want worker|Pool", got.PkgPath, got.Recv)
	}
}

// Generic instantiations are calls too: utils.Get[T](r) must not vanish.
func TestGenericCallIsRead(t *testing.T) {
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
	write("go.mod", "module example.com/generic\n\ngo 1.22\n")
	write("util/util.go", `package util

func Get[T any](v T) *T { return &v }
`)
	write("main.go", `package main

import "example.com/generic/util"

type user struct{}

func run(u user) *user {
	return util.Get[user](u)
}

func main() { _ = run(user{}) }
`)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	run := m.Funcs["example.com/generic|run"]
	if run == nil {
		t.Fatal("missing run")
	}
	if len(run.Calls) != 1 {
		t.Fatalf("calls = %+v, want the generic call", run.Calls)
	}
	c := run.Calls[0]
	if c.Sel != "Get" || c.Expr != "util.Get" || c.Target == nil {
		t.Fatalf("generic call = %+v, want util.Get resolved", c)
	}
}

// A handler that answers through a project helper and through http.Error shows
// the statuses those helpers write.
func TestHelperResponsesCarryStatuses(t *testing.T) {
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
	write("go.mod", "module example.com/http\n\ngo 1.22\n")
	write("utils/utils.go", `package utils

import (
	"net/http"
)

func SendJSON(w http.ResponseWriter, statusCode int, data any) {
	w.WriteHeader(statusCode)
	_ = data
}

func Fail(w http.ResponseWriter) {
	http.Error(w, "gone", http.StatusGone)
}
`)
	write("api/handler.go", `package api

import (
	"net/http"

	"example.com/http/utils"
)

type Store struct{}

func (s *Store) Load() (any, error) { return nil, nil }

type Handler struct {
	store *Store
}

func (h *Handler) Get(w http.ResponseWriter) {
	v, err := h.store.Load()
	if err != nil {
		utils.SendJSON(w, http.StatusInternalServerError, nil)
		return
	}
	utils.SendJSON(w, http.StatusOK, v)
}

func (h *Handler) Gone(w http.ResponseWriter) {
	utils.Fail(w)
}
`)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	get := m.Funcs["example.com/http/api|Handler|Get"]
	if get == nil {
		t.Fatal("missing Handler.Get")
	}
	steps := flowSteps(get, get.Decl.Body.List)
	if len(steps) != 2 {
		t.Fatalf("steps = %+v, want the folded exit and the response", steps)
	}
	guard := steps[0]
	if guard.Kind != "exit" || guard.Code != "500" {
		t.Fatalf("guard = %+v, want exit with 500", guard)
	}
	if guard.Key != "example.com/http/api|Store|Load" {
		t.Fatalf("guard key = %q, want the store call", guard.Key)
	}
	resp := steps[1]
	if resp.Kind != "call" || resp.Code != "200" {
		t.Fatalf("response = %+v, want a named response card with 200", resp)
	}
	if resp.Key != "example.com/http/utils|SendJSON" {
		t.Fatalf("response key = %q, want the helper flow", resp.Key)
	}

	gone := m.Funcs["example.com/http/api|Handler|Gone"]
	if gone == nil {
		t.Fatal("missing Handler.Gone")
	}
	calls := map[string]Call{}
	for _, c := range gone.Calls {
		calls[c.Expr] = c
	}
	st := callStep(gone, mustCall(t, gone), calls)
	if st.Kind != "call" || st.Code != "410" {
		t.Fatalf("utils.Fail step = %+v, want a named response card with 410 from http.Error", st)
	}
}

func mustCall(t *testing.T, f *Func) *ast.CallExpr {
	t.Helper()
	var found *ast.CallExpr
	ast.Inspect(f.Decl.Body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if ce, ok := n.(*ast.CallExpr); ok {
			found = ce
			return false
		}
		return true
	})
	if found == nil {
		t.Fatal("no call in body")
	}
	return found
}

// The route document lists the status a response helper answers with, not only
// the exits.
func TestRouteDocIncludesResponseHelperStatus(t *testing.T) {
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
	write("go.mod", "module example.com/docstatus\n\ngo 1.22\n")
	write("api/api.go", `package api

import (
	"net/http"
)

func send(w http.ResponseWriter, code int) { w.WriteHeader(code) }

type Handler struct{}

func (h *Handler) Get(w http.ResponseWriter) {
	if false {
		send(w, http.StatusBadRequest)
		return
	}
	send(w, http.StatusCreated)
}
`)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	get := m.Funcs["example.com/docstatus/api|Handler|Get"]
	if get == nil {
		t.Fatal("missing Handler.Get")
	}
	doc := buildDoc(m, &Route{Method: "GET", Path: "/get", Chain: []Ref{{Name: "Get", Role: "handler", Fn: get}}})
	if doc == nil {
		t.Fatal("no doc")
	}
	codes := map[string]bool{}
	for _, r := range doc.Resp {
		codes[r.Code] = true
	}
	if !codes["201"] || !codes["400"] {
		t.Fatalf("doc responses = %+v, want 201 from the helper and 400 from the exit", doc.Resp)
	}
}
