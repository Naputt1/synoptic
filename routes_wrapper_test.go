package main

import (
	"os"
	"path/filepath"
	"testing"
)

func wrapperFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `package wrapper

import "encoding/json"

type Router struct{}

func (r *Router) GET(path string, h func()) {}

type ServeEvent struct {
	Router *Router
}

type Handler interface {
	OnMessage([]byte) error
}

type Registrar struct{}

func (Registrar) Register(r *Router, path string, h Handler) {
	r.GET(path, func() { serve() })
}

func serve() {
	u := NewUpgrader()
	u.CheckOrigin = func() bool { return true }
	_ = u.Upgrade(nil, nil, nil)
}

type Upgrader struct{}

func NewUpgrader() *Upgrader { return &Upgrader{} }

func (u *Upgrader) Upgrade(w, r, h any) error { return nil }

type MsgType string

const (
	MsgPing  MsgType = "ping"
	MsgQuery MsgType = "query"
)

type Header struct {
	Type MsgType
}

type Pong struct {
	Type string
}

type Conn struct{}

func (Conn) WriteJSON(v any) error { return nil }

type DB struct{}

func (DB) Query(q string) {}

var db DB

type handler struct{}

func (h handler) OnMessage(data []byte) error {
	var m Header
	_ = json.Unmarshal(data, &m)
	switch m.Type {
	case MsgPing:
		return handle(h.ping)
	case MsgQuery:
		return handle(h.query)
	}
	var c Conn
	return c.WriteJSON(map[string]string{"type": "welcome"})
}

func handle(fn func([]byte) error) error { return fn(nil) }

func (h handler) ping([]byte) error {
	db.Query("pong")
	var c Conn
	return c.WriteJSON(Pong{Type: "pong"})
}

func (h handler) query([]byte) error { return nil }

func NewEvent(t MsgType) {}

func publishPing() { NewEvent(MsgPing) }

var reg Registrar

func setup(e *ServeEvent) {
	reg.Register(e.Router, "/ws", handler{})
	reg.Register(e.Router, "/ws-syspipe", handler{})
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/wrapper\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wrapper.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func messageOfEvent(messages []JMessage, event string) *JMessage {
	for i := range messages {
		if messages[i].Ev == event {
			return &messages[i]
		}
	}
	return nil
}

func TestRoutesThroughRegistrationHelper(t *testing.T) {
	m, err := loadModule(wrapperFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	routes := buildRoutes(m)
	byPath := map[string]*Route{}
	for _, route := range routes {
		byPath[route.Path] = route
	}
	for _, path := range []string{"/ws", "/ws-syspipe"} {
		route := byPath[path]
		if route == nil {
			t.Fatalf("missing route %s in %+v", path, routes)
		}
		stream := classifyStream(m, route)
		if stream == nil || stream.K != "ws" {
			t.Fatalf("%s stream = %+v, want ws", path, stream)
		}
		if !stream.OC {
			t.Fatalf("%s origin check not detected", path)
		}
		if !hasEvent(stream.Events, "ping") || !hasEvent(stream.Events, "query") {
			t.Fatalf("%s events = %v, want ping and query", path, stream.Events)
		}
		if len(stream.Recv) == 0 {
			t.Fatalf("%s has no received frames", path)
		}
		if got := messageOfEvent(stream.Recv, "ping"); got == nil {
			t.Fatalf("%s ping recv = %+v", path, stream.Recv)
		}
		if got := messageOf(stream.Send, "Pong"); got == nil || got.Ev != "ping" {
			t.Fatalf("%s send = %+v, want Pong owned by ping", path, stream.Send)
		}
		if got := messageOfEvent(stream.Send, "welcome"); got == nil {
			t.Fatalf("%s send = %+v, want welcome frame attributed from its literal", path, stream.Send)
		}
		var pingDB bool
		for _, c := range stream.Ext {
			if c.G == "db" && c.Ev == "ping" {
				pingDB = true
			}
		}
		if !pingDB {
			t.Fatalf("%s ext = %+v, want a db call owned by ping", path, stream.Ext)
		}
	}

	d := buildData(m, routes)
	for _, route := range routes {
		var ping *JEvent
		for i := range route.Stream.Evs {
			if route.Stream.Evs[i].N == "ping" {
				ping = &route.Stream.Evs[i]
			}
		}
		if ping == nil || ping.Fk == "" {
			t.Fatalf("%s ping event handler = %+v", route.Path, ping)
		}
		if flow, ok := d.Flows[ping.Fk]; !ok || len(flow.Fx) == 0 {
			t.Fatalf("%s ping flow/effects = %+v", route.Path, flow)
		}
		var origin bool
		for _, o := range ping.Or {
			if o.N == "publishPing" {
				origin = true
			}
		}
		if !origin {
			t.Fatalf("%s ping origins = %+v, want publishPing", route.Path, ping.Or)
		}
	}
}
