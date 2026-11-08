package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func protocolFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/streams\n\ngo 1.22\n")
	write("streams.go", `package streams

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type ClientMsg struct {
	Action string `+"`json:\"action\"`"+`
}

type ServerMsg struct {
	Type string    `+"`json:\"type\"`"+`
	Data ClientMsg `+"`json:\"data\"`"+`
}

type EventType string

const EventTick EventType = "tick"

type Event struct {
	Type EventType       `+"`json:\"type\"`"+`
	Data json.RawMessage `+"`json:\"data\"`"+`
}

type EventData struct {
	Kind  string `+"`json:\"kind\"`"+`
	Count int    `+"`json:\"count\"`"+`
}

func routes() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler)
	mux.HandleFunc("/events", sseHandler)
	mux.HandleFunc("/events/typed", typedSSE)
	mux.HandleFunc("/health", health)
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var in ClientMsg
	if err := conn.ReadJSON(&in); err != nil {
		return
	}
	_ = conn.WriteJSON(ServerMsg{Type: "connected", Data: in})
	_ = conn.WriteMessage(websocket.TextMessage, []byte("ping"))
	conn.SetPongHandler(func(string) error { return nil })
}

func sseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	f, ok := w.(http.Flusher)
	if !ok {
		return
	}
	data, _ := json.Marshal(EventData{Kind: "tick", Count: 1})
	fmt.Fprintf(w, "event: tick\ndata: %s\n\n", data)
	f.Flush()
}

func typedSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	data, _ := json.Marshal(EventData{Kind: "typed"})
	writeEvent(w, Event{Type: EventTick, Data: data})
	w.(http.Flusher).Flush()
}

func writeEvent(w io.Writer, ev Event) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Data)
}

func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
`)
	return dir
}

func routeAt(d JData, path string) *JRoute {
	for i := range d.Routes {
		if d.Routes[i].P == path {
			return &d.Routes[i]
		}
	}
	return nil
}

func hasEvent(events []string, want string) bool {
	for _, e := range events {
		if e == want {
			return true
		}
	}
	return false
}

func messageOf(messages []JMessage, typ string) *JMessage {
	for i := range messages {
		if messages[i].T == typ {
			return &messages[i]
		}
	}
	return nil
}

func TestClassifiesStreams(t *testing.T) {
	m, err := loadModule(protocolFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	d := buildData(m, buildRoutes(m))

	ws := routeAt(d, "/ws")
	if ws == nil {
		t.Fatal("no /ws route")
	}
	if ws.Pr != "ws" || ws.St == nil || ws.St.K != "ws" {
		t.Fatalf("/ws classified as %q stream=%+v", ws.Pr, ws.St)
	}
	if !ws.St.OC {
		t.Fatal("/ws should report the origin check")
	}
	if !hasEvent(ws.St.Events, "connected") {
		t.Fatalf("/ws events = %v", ws.St.Events)
	}
	if !strings.Contains(ws.St.HB, "ping/pong") {
		t.Fatalf("/ws heartbeat = %q", ws.St.HB)
	}
	send := messageOf(ws.St.Send, "ServerMsg")
	if send == nil {
		t.Fatalf("/ws send = %+v", ws.St.Send)
	}
	if len(send.F) < 2 || send.F[0].N != "type" || send.F[1].N != "data" {
		t.Fatalf("ServerMsg fields = %+v", send.F)
	}
	recv := messageOf(ws.St.Recv, "ClientMsg")
	if recv == nil || len(recv.F) != 1 || recv.F[0].N != "action" {
		t.Fatalf("/ws recv = %+v", ws.St.Recv)
	}

	sse := routeAt(d, "/events")
	if sse == nil {
		t.Fatal("no /events route")
	}
	if sse.Pr != "sse" || sse.St == nil || sse.St.K != "sse" {
		t.Fatalf("/events classified as %q", sse.Pr)
	}
	if !hasEvent(sse.St.Events, "tick") {
		t.Fatalf("/events events = %v", sse.St.Events)
	}
	if payload := messageOf(sse.St.Send, "EventData"); payload == nil || len(payload.F) < 2 {
		t.Fatalf("/events payload = %+v", sse.St.Send)
	}

	typed := routeAt(d, "/events/typed")
	if typed == nil || typed.Pr != "sse" {
		t.Fatalf("/events/typed = %+v", typed)
	}
	if !hasEvent(typed.St.Events, "tick") {
		t.Fatalf("/events/typed events = %v", typed.St.Events)
	}

	health := routeAt(d, "/health")
	if health == nil || health.Pr != "" || health.St != nil {
		t.Fatalf("/health = %+v", health)
	}
}
