package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeIfaceFile(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInterfaceImplementersRequireFullSignature(t *testing.T) {
	dir := t.TempDir()
	writeIfaceFile(t, dir, "go.mod", "module example.com/shop\n\ngo 1.22\n")
	writeIfaceFile(t, dir, "shop.go", `package shop

type Item struct{}

type Repository interface {
	Get(id string) (*Item, error)
	Save(Item) error
}

type Store struct{}

func (s *Store) Get(id string) (*Item, error) { return nil, nil }
func (s *Store) Save(i Item) error            { return nil }

type MemStore struct{}

func (s *MemStore) Get(id string) (*Item, error) { return nil, nil }
func (s *MemStore) Save(i Item) error            { return nil }

type Partial struct{}

func (p *Partial) Get(id string) (*Item, error) { return nil, nil }

type BadSig struct{}

func (b *BadSig) Get(id int) (*Item, error) { return nil, nil }
func (b *BadSig) Save(i Item) error         { return nil }

type Handler struct {
	repo Repository
}

func (h *Handler) Do() { _ = h.repo.Get("x") }
`)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := "example.com/shop|Repository"
	if m.implements(key, "example.com/shop", "Store") != true {
		t.Fatal("Store should implement Repository")
	}
	if m.implements(key, "example.com/shop", "Partial") {
		t.Fatal("Partial is missing Save and must not implement Repository")
	}
	if m.implements(key, "example.com/shop", "BadSig") {
		t.Fatal("BadSig.Get has the wrong parameter type and must not implement Repository")
	}
	impls := m.implementers(key, "Get", nil)
	if len(impls) != 2 {
		t.Fatalf("implementers = %+v, want MemStore and Store", impls)
	}
	if impls[0].Recv != "MemStore" || impls[1].Recv != "Store" {
		t.Fatalf("implementers = %+v, want MemStore and Store", impls)
	}
	if one := m.ifaceImpl("example.com/shop", "Repository", "Get", nil); one != nil {
		t.Fatalf("ifaceImpl = %+v, want nil for an ambiguous interface", one)
	}

	types := buildTypeCatalog(m)
	cat := types[key]
	if len(cat.Impls) != 2 {
		t.Fatalf("catalog impls = %+v, want MemStore and Store only", cat.Impls)
	}
	for _, impl := range cat.Impls {
		n := types[impl].N
		if n != "Store" && n != "MemStore" {
			t.Fatalf("catalog listed %q, which does not fully implement Repository", n)
		}
	}

	step := Step{IfaceKey: key, Sel: "Get"}
	annotateStep(&step, types, m)
	if len(step.Impls) != 2 {
		t.Fatalf("step impls = %+v, want two choices", step.Impls)
	}
	if step.Ik != key {
		t.Fatalf("step interface = %q, want %q", step.Ik, key)
	}

	flows := map[string]JFunc{}
	indexFunc(m, flows, m.Funcs["example.com/shop|Handler|Do"], 0)
	for _, want := range []string{"example.com/shop|MemStore|Get", "example.com/shop|Store|Get"} {
		if _, ok := flows[want]; !ok {
			t.Fatalf("ambiguous implementation %s was not baked; have %v", want, flows)
		}
	}
}

func TestInterfaceImplementerMatchesAcrossImportAliases(t *testing.T) {
	dir := t.TempDir()
	writeIfaceFile(t, dir, "go.mod", "module example.com/shop\n\ngo 1.22\n")
	writeIfaceFile(t, dir, "model/item.go", "package model\n\ntype Item struct{}\n")
	writeIfaceFile(t, dir, "repo/repo.go", `package repo

import "example.com/shop/model"

type Repository interface {
	Get() (*model.Item, error)
}
`)
	writeIfaceFile(t, dir, "store/store.go", `package store

import m "example.com/shop/model"

type Store struct{}

func (s *Store) Get() (*m.Item, error) { return nil, nil }
`)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	impls := m.implementers("example.com/shop/repo|Repository", "Get", nil)
	if len(impls) != 1 || impls[0].Recv != "Store" {
		t.Fatalf("implementers = %+v, want Store despite the import alias", impls)
	}
}
