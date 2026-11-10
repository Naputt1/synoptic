package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestBuildDepsAttributesPackagesToServices(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", `module example.com/shop

go 1.22

require (
	github.com/gin-gonic/gin v1.10.0
	golang.org/x/sync v0.7.0 // indirect
)
`)
	write("jobs/main.go", `package main

import (
	"example.com/shop/shared/model"
	"example.com/shop/shared/queue"
)

func main() {
	_ = queue.Push
	_ = model.User{}
}
`)
	write("users/cmd/main.go", `package main

import (
	"example.com/shop/shared/model"
	"example.com/shop/shared/store"

	"github.com/gin-gonic/gin"
)

func main() {
	_ = gin.New()
	_ = store.Get
	_ = model.User{}
}
`)
	write("shared/model/model.go", "package model\n\ntype User struct{}\n")
	write("shared/queue/queue.go", `package queue

import "example.com/shop/shared/model"

func Push(u model.User) {}
`)
	write("shared/store/store.go", `package store

import (
	"example.com/shop/shared/model"
	"golang.org/x/sync/errgroup"
)

var _ = errgroup.Group{}

func Get() model.User { return model.User{} }
`)
	write("tools/tools.go", "package tools\n")

	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	deps := buildDeps(m)
	byPath := map[string]JDep{}
	for _, d := range deps {
		byPath[d.P] = d
	}

	jobs := byPath["example.com/shop/jobs"]
	if !jobs.M || jobs.E != "jobs" {
		t.Fatalf("jobs entry = %+v", jobs)
	}
	users := byPath["example.com/shop/users/cmd"]
	if !users.M || users.E != "users" {
		t.Fatalf("users entry = %+v", users)
	}
	if !wantStrings(users.X, []string{"github.com/gin-gonic/gin"}) {
		t.Fatalf("users third-party = %v", users.X)
	}

	model := byPath["example.com/shop/shared/model"]
	if !wantStrings(model.By, []string{"jobs", "users"}) {
		t.Fatalf("model used by = %v", model.By)
	}
	if !wantStrings(model.Dr, []string{"jobs", "users"}) {
		t.Fatalf("model direct = %v", model.Dr)
	}
	if model.D != "shared/model" || model.F != 1 {
		t.Fatalf("model dir/files = %q/%d", model.D, model.F)
	}

	queue := byPath["example.com/shop/shared/queue"]
	if !wantStrings(queue.By, []string{"jobs"}) || !wantStrings(queue.Dr, []string{"jobs"}) {
		t.Fatalf("queue attribution = %v/%v", queue.By, queue.Dr)
	}
	store := byPath["example.com/shop/shared/store"]
	if !wantStrings(store.By, []string{"users"}) || !wantStrings(store.Dr, []string{"users"}) {
		t.Fatalf("store attribution = %v/%v", store.By, store.Dr)
	}
	if !wantStrings(store.X, []string{"golang.org/x/sync"}) {
		t.Fatalf("store third-party = %v", store.X)
	}

	tools := byPath["example.com/shop/tools"]
	if len(tools.By) != 0 || len(tools.Dr) != 0 {
		t.Fatalf("unused package attributed to %v", tools.By)
	}
}

func TestExternalModulesAndMapping(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", `module example.com/app

require github.com/gin-gonic/gin v1.10.0
`)
	write("nested/go.mod", `module example.com/nested

require (
	rsc.io/quote v1.5.2
	gopkg.in/yaml.v3 v3.0.1
)
`)

	mods := externalModules(dir)
	if !wantStrings(mods, []string{"github.com/gin-gonic/gin", "gopkg.in/yaml.v3", "rsc.io/quote"}) {
		t.Fatalf("modules = %v", mods)
	}
	cases := []struct {
		imp  string
		want string
	}{
		{"github.com/gin-gonic/gin/binding", "github.com/gin-gonic/gin"},
		{"golang.org/x/sync/errgroup", "golang.org/x/sync"},
		{"google.golang.org/grpc/codes", "google.golang.org/grpc"},
		{"gopkg.in/yaml.v3", "gopkg.in/yaml.v3"},
		{"net/http", ""},
		{"fmt", ""},
	}
	for _, c := range cases {
		if got := moduleFor(c.imp, mods); got != c.want {
			t.Errorf("moduleFor(%q) = %q, want %q", c.imp, got, c.want)
		}
	}
}

func wantStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	return reflect.DeepEqual(g, w)
}
