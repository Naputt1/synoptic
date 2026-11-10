package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const errFixture = `package fixture

import (
	"errors"
	"fmt"
	"log"
	"net/http"
)

type Ctx struct{}

func (c *Ctx) JSON(code int, v any)               {}
func (c *Ctx) AbortWithStatusJSON(code int, v any) {}
func (c *Ctx) Cond() error                         { return nil }

type Router struct{}

func (r *Router) GET(p string, h func(*Ctx)) {}

type body map[string]any

var ErrGone = errors.New("gone forever")

func fail(c *Ctx, msg string) {
	c.JSON(422, body{"error": msg})
}

func boom() error {
	return ErrGone
}

func load(id int) error {
	return fmt.Errorf("load %d: %w", id, ErrGone)
}

func handleA(c *Ctx) {
	if c.Cond() != nil {
		c.JSON(400, body{"error": "bad input"})
		return
	}
	if c.Cond() != nil {
		fail(c, "invalid id")
		return
	}
	c.JSON(200, body{"message": "ok"})
}

func handleB(c *Ctx) {
	http.Error(nil, "teapot", 418)
}

func handleC(c *Ctx) {
	if err := boom(); err != nil {
		c.JSON(410, body{"error": err.Error()})
		return
	}
}

func handleD(c *Ctx) {
	if err := load(7); err != nil {
		c.JSON(500, body{"error": "wrapped"})
		return
	}
}

func handleE(c *Ctx) {
	log.Error("cache miss")
	log.Println("failed to open store")
	log.Println("tick")
}

func handleF(c *Ctx) {
	panic("handler exploded")
}

func setup(r *Router) {
	r.GET("/a", handleA)
	r.GET("/b", handleB)
	r.GET("/c", handleC)
	r.GET("/d", handleD)
	r.GET("/e", handleE)
	r.GET("/f", handleF)
}
`

func loadErrFixture(t *testing.T) (*Module, []*Route, JData) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/errs\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "errs.go"), []byte(errFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	routes := buildRoutes(m)
	d := buildData(m, routes)
	attachErrors(m, routes, &d)
	return m, routes, d
}

func findErr(d JData, msg string) *JErr {
	for i := range d.Errs {
		if d.Errs[i].M == msg {
			return &d.Errs[i]
		}
	}
	return nil
}

func TestErrorsExtractMessagesAndStatuses(t *testing.T) {
	_, _, d := loadErrFixture(t)

	cases := []struct {
		msg   string
		kind  string
		codes []string
	}{
		{"bad input", "response", []string{"400"}},
		{"invalid id", "response", []string{"422"}},
		{"teapot", "response", []string{"418"}},
		{"gone forever", "error", nil},
		{"load %d: %w", "error", nil},
		{"cache miss", "log", nil},
		{"failed to open store", "log", nil},
		{"handler exploded", "panic", nil},
	}
	for _, tc := range cases {
		e := findErr(d, tc.msg)
		if e == nil {
			t.Fatalf("missing %q in %+v", tc.msg, d.Errs)
		}
		if e.K != tc.kind {
			t.Errorf("%q kind = %q, want %q", tc.msg, e.K, tc.kind)
		}
		if strings.Join(e.C, ",") != strings.Join(tc.codes, ",") {
			t.Errorf("%q codes = %v, want %v", tc.msg, e.C, tc.codes)
		}
		if tc.msg == "load %d: %w" && !e.X {
			t.Errorf("%q not marked as a template", tc.msg)
		}
		if tc.msg == "invalid id" && e.D != "msg" {
			t.Errorf("%q param = %q, want msg", tc.msg, e.D)
		}
	}

	if e := findErr(d, "ok"); e != nil {
		t.Errorf("captured success message: %+v", e)
	}
	if e := findErr(d, "tick"); e != nil {
		t.Errorf("captured a plain std log line: %+v", e)
	}
	if e := findErr(d, "wrapped"); e == nil {
		t.Error("missed a literal response message next to a dynamic error value")
	}
}

func TestErrorsLinkRoutesAndSteps(t *testing.T) {
	_, routes, d := loadErrFixture(t)

	e := findErr(d, "bad input")
	if e == nil {
		t.Fatal("missing bad input")
	}
	if len(e.R) == 0 {
		t.Fatalf("bad input has no route ref: %+v", e)
	}
	ri := e.R[0].Ri
	if ri < 0 || ri >= len(routes) || ri >= len(d.Routes) {
		t.Fatalf("route ref %d out of range", ri)
	}
	if d.Routes[ri].P != "/a" {
		t.Fatalf("bad input lands on %q, want /a", d.Routes[ri].P)
	}

	sentinel := findErr(d, "gone forever")
	if sentinel == nil {
		t.Fatal("missing sentinel")
	}
	if !strings.Contains(sentinel.Fn, "boom") {
		t.Errorf("sentinel site = %q, want the using function boom", sentinel.Fn)
	}
	if len(sentinel.R) == 0 || d.Routes[sentinel.R[0].Ri].P != "/c" {
		t.Errorf("sentinel refs = %+v, want /c", sentinel.R)
	}

	var exit *Step
	walkSteps(d.Flows["example.com/errs|handleA"].Steps, func(s Step) {
		if s.Kind == "exit" && strings.Contains(s.Text, "Cond") && exit == nil {
			exit = &s
		}
	})
	if exit == nil {
		t.Fatal("missing exit step in handleA")
	}
	if len(exit.Errs) == 0 {
		t.Fatalf("exit step has no error preview: %+v", exit)
	}
	found := false
	for _, ei := range exit.Errs {
		if d.Errs[ei].M == "bad input" {
			found = true
		}
	}
	if !found {
		t.Errorf("exit previews = %+v, want bad input", exit.Errs)
	}

	// The helper's message lives in fail(), but the caller's exit carries it.
	var helperExit *Step
	walkSteps(d.Flows["example.com/errs|handleA"].Steps, func(s Step) {
		if s.Kind == "exit" && strings.Contains(s.Text, "Cond") {
			for _, ei := range s.Errs {
				if d.Errs[ei].M == "invalid id" {
					helperExit = &s
				}
			}
		}
	})
	if helperExit == nil {
		t.Error("exit calling fail() does not preview the helper message")
	}
}
