package main

import "testing"

// wiringFixture keeps one interface per evidence shape, each with two
// implementations, so a resolved call can only come from the wiring index. Every
// type name is unique: a type that satisfied two interfaces would let one
// scenario's evidence leak into another.
const wiringFixture = `package wiring

type Cache interface{ Get(key string) string }

type Memory struct{}

func (Memory) Get(key string) string { return key }

type Redis struct{}

func (Redis) Get(key string) string { return key }

type CacheServer struct{ cache Cache }

func (s *CacheServer) Lookup(key string) string { return s.cache.Get(key) }

var cacheServer = CacheServer{cache: &Memory{}}

type Queue interface{ Publish(msg string) }

type Kafka struct{}

func (Kafka) Publish(msg string) {}

type NATS struct{}

func (NATS) Publish(msg string) {}

type QueueServer struct{ q Queue }

func (s *QueueServer) Send(msg string) { s.q.Publish(msg) }

var queueA = &QueueServer{q: &Kafka{}}
var queueB = &QueueServer{q: &NATS{}}

type Notifier interface{ Notify(msg string) }

type Email struct{}

func (Email) Notify(msg string) {}

type SMS struct{}

func (SMS) Notify(msg string) {}

func NewNotifier(n Notifier) Notifier { return n }

func notify(n Notifier) { n.Notify("x") }

var notifier = NewNotifier(&Email{})

type Sender interface{ Send(msg string) }

type SenderA struct{}

func (SenderA) Send(msg string) {}

type SenderB struct{}

func (SenderB) Send(msg string) {}

type Bogus struct{}

type SenderHolder struct{ sender Sender }

func (h *SenderHolder) Push(msg string) { h.sender.Send(msg) }

var bogusHolder = &SenderHolder{sender: &Bogus{}}

type Worker interface{ Work() }

type WorkerA struct{}

func (WorkerA) Work() {}

type WorkerB struct{}

func (WorkerB) Work() {}

func runWorker(w Worker) { w.Work() }

type Pinger interface{ Ping() }

type PingerA struct{}

func (PingerA) Ping() {}

type PingerB struct{}

func (PingerB) Ping() {}

type PingerHolder struct{ p Pinger }

func (h *PingerHolder) Run() { h.p.Ping() }

var pingerHolder = &PingerHolder{p: &StubPinger{}}

type Service interface{ Fetch() string }

type MemoryService struct{}

func (MemoryService) Fetch() string { return "memory" }

type RemoteService struct{}

func (RemoteService) Fetch() string { return "remote" }

type Controller struct{ svc Service }

func (c *Controller) Fetch() string { return "controller" }

func (c *Controller) Handle() string { return c.svc.Fetch() }

var controller = &Controller{svc: MemoryService{}}

type Store interface{ Load() }

type DiskStore struct{}

func (DiskStore) Load() {}

type CloudStore struct{}

func (CloudStore) Load() {}

func loadStore(s Store) { s.Load() }

var store Store = &DiskStore{}

type Writer interface{ Write() }

type FileWriter struct{}

func (FileWriter) Write() {}

type NullWriter struct{}

func (NullWriter) Write() {}

func writeAll(w Writer) { w.Write() }

func configureWriter() {
	var w Writer
	w = &FileWriter{}
	_ = w
}

type Handler interface{ Handle() }

type FastHandler struct{}

func (FastHandler) Handle() {}

type SlowHandler struct{}

func (SlowHandler) Handle() {}

func handle(h Handler) { h.Handle() }

func buildHandler() {
	h := Handler(&FastHandler{})
	_ = h
}

type Auditor interface{ Audit() }

type TSAuditor struct{}

func (TSAuditor) Audit() {}

type LogAuditor struct{}

func (LogAuditor) Audit() {}

type AuditServer struct{ auditor Auditor }

func (s *AuditServer) configure() { s.auditor = &TSAuditor{} }

func (s *AuditServer) Run() { s.auditor.Audit() }
`

const wiringStub = `package wiring

type StubPinger struct{}

func (StubPinger) Ping() {}
`

const wiringPkg = "example.com/wiring"

func loadWiringFixture(t *testing.T) *Module {
	t.Helper()
	dir := t.TempDir()
	writeDiagFile(t, dir, "go.mod", "module example.com/wiring\n\ngo 1.22\n")
	writeDiagFile(t, dir, "main.go", wiringFixture)
	writeDiagFile(t, dir, "stub.pb.go", wiringStub)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// wiringCall finds the call by function name and expression. Receiver methods
// are keyed by receiver, and two receivers may share a method name, so the
// expression disambiguates.
func wiringCall(t *testing.T, m *Module, fn, expr string) *Call {
	t.Helper()
	for _, f := range m.Funcs {
		if f.Name != fn || f.File == nil || f.File.PkgPath != wiringPkg {
			continue
		}
		for i := range f.Calls {
			if f.Calls[i].Expr == expr {
				return &f.Calls[i]
			}
		}
	}
	t.Fatalf("%s: call %q not found", fn, expr)
	return nil
}

func wantWired(t *testing.T, m *Module, fn, expr, recv, name string) {
	t.Helper()
	c := wiringCall(t, m, fn, expr)
	if c.Target == nil {
		t.Fatalf("%s: %s is unresolved (%s), want %s.%s", fn, expr, c.Why, recv, name)
	}
	if c.Target.Recv != recv || c.Target.Name != name {
		t.Fatalf("%s: %s resolved to %s.%s, want %s.%s", fn, expr, c.Target.Recv, c.Target.Name, recv, name)
	}
	if c.Why != "resolved" {
		t.Fatalf("%s: %s why = %q, want resolved", fn, expr, c.Why)
	}
}

func wantAmbiguous(t *testing.T, m *Module, fn, expr string) *Call {
	t.Helper()
	c := wiringCall(t, m, fn, expr)
	if c.Target != nil {
		t.Fatalf("%s: %s resolved to %s.%s, want no target", fn, expr, c.Target.Recv, c.Target.Name)
	}
	if c.Why != "ambiguous" {
		t.Fatalf("%s: %s why = %q, want ambiguous", fn, expr, c.Why)
	}
	return c
}

// A field typed as an interface is bound when a struct literal names the
// concrete value assigned to it.
func TestWiringThroughStructLiteral(t *testing.T) {
	m := loadWiringFixture(t)
	wantWired(t, m, "Lookup", "s.cache.Get", "Memory", "Get")
}

// Wiring is global, not per call site: two different implementations assigned
// to the same interface elsewhere in the module keep it ambiguous.
func TestConflictingWiringStaysAmbiguous(t *testing.T) {
	m := loadWiringFixture(t)
	wantAmbiguous(t, m, "Send", "s.q.Publish")
	if got := len(m.Wiring[recvKey(wiringPkg, "Queue")]); got != 2 {
		t.Fatalf("Queue wiring = %d types, want 2", got)
	}
}

// A constructor argument paired with an interface parameter is static call-site
// evidence, even though the constructor result is not followed.
func TestWiringThroughConstructorArgument(t *testing.T) {
	m := loadWiringFixture(t)
	wantWired(t, m, "notify", "n.Notify", "Email", "Notify")
}

// A wired type that does not declare the interface's methods says nothing and
// must not be recorded.
func TestWiringIgnoresNonImplementer(t *testing.T) {
	m := loadWiringFixture(t)
	wantAmbiguous(t, m, "Push", "h.sender.Send")
	if got := len(m.Wiring[recvKey(wiringPkg, "Sender")]); got != 0 {
		t.Fatalf("Sender wiring = %d types, want none", got)
	}
}

// No evidence means the old conservative answer: ambiguous, not a guess.
func TestNoWiringStaysAmbiguous(t *testing.T) {
	m := loadWiringFixture(t)
	wantAmbiguous(t, m, "runWorker", "w.Work")
}

// A generated-only implementation is not a wiring candidate, so it cannot be
// chosen even when it is the only type the code assigns.
func TestWiringSkipsGeneratedOnly(t *testing.T) {
	m := loadWiringFixture(t)
	wantAmbiguous(t, m, "Run", "h.p.Ping")
	if got := len(m.Wiring[recvKey(wiringPkg, "Pinger")]); got != 0 {
		t.Fatalf("Pinger wiring = %d types, want none: generated stubs must not bind", got)
	}
}

// The receiver has a Fetch method of its own. The call goes through its field's
// interface, so it must reach the wired implementation, not the receiver.
func TestWiringDoesNotUseReceiverMethod(t *testing.T) {
	m := loadWiringFixture(t)
	c := wiringCall(t, m, "Handle", "c.svc.Fetch")
	if c.Target == nil {
		t.Fatalf("c.svc.Fetch is unresolved (%s)", c.Why)
	}
	if c.Target.Recv == "Controller" {
		t.Fatal("c.svc.Fetch resolved to the receiver's own method")
	}
	wantWired(t, m, "Handle", "c.svc.Fetch", "MemoryService", "Fetch")
}

func TestWiringThroughExplicitVar(t *testing.T) {
	m := loadWiringFixture(t)
	wantWired(t, m, "loadStore", "s.Load", "DiskStore", "Load")
}

func TestWiringThroughAssignment(t *testing.T) {
	m := loadWiringFixture(t)
	wantWired(t, m, "writeAll", "w.Write", "FileWriter", "Write")
}

func TestWiringThroughConversion(t *testing.T) {
	m := loadWiringFixture(t)
	wantWired(t, m, "handle", "h.Handle", "FastHandler", "Handle")
}

func TestWiringThroughReceiverFieldAssignment(t *testing.T) {
	m := loadWiringFixture(t)
	wantWired(t, m, "Run", "s.auditor.Audit", "TSAuditor", "Audit")
}
