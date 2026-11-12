package main

import "testing"

// embedFixture puts every promotion shape in one module: a three-level struct
// chain, pointer and cross-package embeds, an embedded interface, a promoted
// interface implementation, an ambiguous pair and a shadowing method.
const embedFixture = `package embed

import "example.com/embed/base"

type C struct{}

func (C) Deep() {}

type B struct{ C }

type A struct{ B }

func useA(a *A) { a.Deep() }

type Base struct{}

func (Base) Hello() {}

type Ptr struct{ *Base }

type Ext struct{ base.Base }

type Reader interface{ Read() string }

type WithReader struct{ Reader }

type Saver struct{}

func (Saver) Save() {}

type Store interface {
	Save()
	Close()
}

type Service struct{ Saver }

func (Service) Close() {}

type Unrelated struct{}

func (Unrelated) Other() {}

type Left struct{}

func (Left) Clash() {}

type Right struct{}

func (Right) Clash() {}

type Both struct {
	Left
	Right
}

type Inner struct{}

func (Inner) Name() string { return "inner" }

type Outer struct{ Inner }

func (Outer) Name() string { return "outer" }
`

func loadEmbedFixture(t *testing.T) *Module {
	t.Helper()
	dir := t.TempDir()
	writeDiagFile(t, dir, "go.mod", "module example.com/embed\n\ngo 1.22\n")
	writeDiagFile(t, dir, "main.go", embedFixture)
	writeDiagFile(t, dir, "base/base.go", "package base\n\ntype Base struct{}\n\nfunc (Base) World() {}\n")
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMethodPromotedThroughThreeLevels(t *testing.T) {
	m := loadEmbedFixture(t)
	fn := m.methodOn("example.com/embed", "A", "Deep")
	if fn == nil {
		t.Fatal("A.Deep not found; promotion through B and C was not followed")
	}
	if fn.Recv != "C" || fn.Name != "Deep" {
		t.Fatalf("A.Deep = %s.%s, want C.Deep", fn.Recv, fn.Name)
	}
}

// The promoted method must reach a call, not only the method-set lookup.
func TestCallThroughPromotedMethodResolves(t *testing.T) {
	m := loadEmbedFixture(t)
	f := m.Funcs[fnKey("example.com/embed", "", "useA")]
	if f == nil {
		t.Fatal("useA not indexed")
	}
	for _, c := range f.Calls {
		if c.Expr != "a.Deep" {
			continue
		}
		if c.Target == nil {
			t.Fatalf("a.Deep is unresolved (%s)", c.Why)
		}
		if c.Target.Recv != "C" {
			t.Fatalf("a.Deep target = %s.%s, want C.Deep", c.Target.Recv, c.Target.Name)
		}
		return
	}
	t.Fatal("a.Deep call not found")
}

func TestPointerAndQualifiedEmbeds(t *testing.T) {
	m := loadEmbedFixture(t)
	cases := []struct {
		typ, sel, pkg, recv, recvPkg string
	}{
		{"Ptr", "Hello", "example.com/embed", "Base", "example.com/embed"},
		{"Ext", "World", "example.com/embed", "Base", "example.com/embed/base"},
	}
	for _, tc := range cases {
		fn := m.methodOn(tc.pkg, tc.typ, tc.sel)
		if fn == nil {
			t.Fatalf("%s.%s not found through embedding", tc.typ, tc.sel)
		}
		if fn.Recv != tc.recv || fn.PkgPath != tc.recvPkg {
			t.Fatalf("%s.%s = %s|%s, want %s|%s",
				tc.typ, tc.sel, fn.PkgPath, fn.Recv, tc.recvPkg, tc.recv)
		}
	}
}

func TestEmbeddedInterfaceContributesMethods(t *testing.T) {
	m := loadEmbedFixture(t)
	if !m.declaresAll("example.com/embed", "WithReader", []string{"Read"}) {
		t.Fatal("WithReader does not have Read from its embedded Reader")
	}
	if fn := m.methodOn("example.com/embed", "WithReader", "Read"); fn != nil {
		t.Fatalf("WithReader.Read = %s.%s, want no concrete function", fn.PkgPath, fn.Recv)
	}
}

func TestInterfaceSatisfiedThroughPromotedMethod(t *testing.T) {
	m := loadEmbedFixture(t)
	fn, ambiguous := m.ifaceImpl("example.com/embed", "Store", "Save", nil)
	if ambiguous {
		t.Fatal("Store.Save is ambiguous, want the promoted implementation")
	}
	if fn == nil {
		t.Fatal("Store.Save did not bind; Service satisfies Store through embedded Saver")
	}
	if fn.Recv != "Saver" || fn.Name != "Save" {
		t.Fatalf("Store.Save = %s.%s, want Saver.Save", fn.Recv, fn.Name)
	}
	if m.declaresAll("example.com/embed", "Saver", []string{"Save", "Close"}) {
		t.Fatal("Saver alone must not satisfy Store: Close is declared on Service")
	}
}

func TestUnrelatedMethodIsNotPromoted(t *testing.T) {
	m := loadEmbedFixture(t)
	if fn := m.methodOn("example.com/embed", "A", "Other"); fn != nil {
		t.Fatalf("A.Other = %s.%s, want nil", fn.PkgPath, fn.Recv)
	}
}

func TestAmbiguousPromotionHasNoTarget(t *testing.T) {
	m := loadEmbedFixture(t)
	if fn := m.methodOn("example.com/embed", "Both", "Clash"); fn != nil {
		t.Fatalf("Both.Clash = %s.%s, want nil for an ambiguous promotion", fn.PkgPath, fn.Recv)
	}
	if _, _, ambiguous := m.findMethod("example.com/embed", "Both", "Clash"); !ambiguous {
		t.Fatal("Both.Clash was not reported ambiguous")
	}
}

func TestDirectMethodShadowsEmbedded(t *testing.T) {
	m := loadEmbedFixture(t)
	fn := m.methodOn("example.com/embed", "Outer", "Name")
	if fn == nil {
		t.Fatal("Outer.Name not found")
	}
	if fn.Recv != "Outer" {
		t.Fatalf("Outer.Name = %s.Name, want the outer method", fn.Recv)
	}
}
