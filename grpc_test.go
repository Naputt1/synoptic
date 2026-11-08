package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func grpcFixture(t *testing.T) string {
	t.Helper()
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
	write("go.mod", "module example.com/grpc\n\ngo 1.22\n")
	write("proto/echo.pb.go", `package proto

import (
	"context"

	"google.golang.org/grpc"
)

type PingRequest struct {
	Message string `+"`json:\"message\"`"+`
}

type PingResponse struct {
	Reply string `+"`json:\"reply\"`"+`
}

type EchoServer interface {
	Ping(context.Context, *PingRequest) (*PingResponse, error)
	Stream(*PingRequest, grpc.ServerStreamingServer[PingResponse]) error
}

type UnimplementedEchoServer struct{}

func (UnimplementedEchoServer) Ping(context.Context, *PingRequest) (*PingResponse, error) {
	return nil, nil
}

func (UnimplementedEchoServer) Stream(*PingRequest, grpc.ServerStreamingServer[PingResponse]) error {
	return nil
}

var Echo_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "echo.Echo",
	Methods: []grpc.MethodDesc{
		{MethodName: "Ping"},
	},
	Streams: []grpc.StreamDesc{
		{StreamName: "Stream", ServerStreams: true},
	},
}

func RegisterEchoServer(s grpc.ServiceRegistrar, srv EchoServer) {}
`)
	write("main.go", `package main

import (
	"context"

	"google.golang.org/grpc"

	pb "example.com/grpc/proto"
)

type EchoGRPCServer struct{}

func (s *EchoGRPCServer) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingResponse, error) {
	return &pb.PingResponse{Reply: req.Message}, nil
}

func (s *EchoGRPCServer) Stream(req *pb.PingRequest, srv pb.Echo_StreamServer) error {
	return nil
}

func logging(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	return handler(ctx, req)
}

func main() {
	s := grpc.NewServer(grpc.ChainUnaryInterceptor(logging))
	pb.RegisterEchoServer(s, &EchoGRPCServer{})
}
`)
	return dir
}

func TestServicesFromGeneratedDescriptor(t *testing.T) {
	dir := grpcFixture(t)
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	services := buildServices(m)
	if len(services) != 1 {
		t.Fatalf("services = %d, want 1", len(services))
	}
	svc := services[0]
	if svc.N != "echo.Echo" {
		t.Fatalf("service = %q, want echo.Echo", svc.N)
	}
	if len(svc.Mw) != 1 || svc.Mw[0].Name != "logging" {
		t.Fatalf("interceptors = %+v, want logging", svc.Mw)
	}
	byName := map[string]JServiceMethod{}
	for _, method := range svc.Methods {
		byName[method.N] = method
	}
	ping := byName["Ping"]
	if ping.K != "unary" {
		t.Fatalf("Ping kind = %q, want unary", ping.K)
	}
	if ping.Rq != "PingRequest" || ping.Rs != "PingResponse" {
		t.Fatalf("Ping types = %q -> %q", ping.Rq, ping.Rs)
	}
	if !strings.Contains(ping.Fk, "EchoGRPCServer|Ping") {
		t.Fatalf("Ping flow = %q", ping.Fk)
	}
	stream := byName["Stream"]
	if stream.K != "server stream" {
		t.Fatalf("Stream kind = %q, want server stream", stream.K)
	}
	if stream.Rq != "PingRequest" || stream.Rs != "PingResponse" {
		t.Fatalf("Stream types = %q -> %q", stream.Rq, stream.Rs)
	}
	if ping.Curl == "" {
		t.Fatal("missing grpcurl example")
	}
}

// multiModuleFixture mirrors a workspace where the generated proto and the
// implementation live in different modules, and the factory returns the
// service interface rather than the concrete type.
func multiModuleFixture(t *testing.T) string {
	t.Helper()
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
	write("go.mod", "module example.com/app\n\ngo 1.22\n")
	write("shared/go.mod", "module shared\n\ngo 1.22\n")
	write("jobs/go.mod", "module jobs\n\ngo 1.22\n")
	write("shared/proto/laborpb/labor.pb.go", `package laborpb

import (
	"context"

	"google.golang.org/grpc"
)

type ShiftRequest struct{}

type ShiftResponse struct{}

type LaborServer interface {
	ClockIn(context.Context, *ShiftRequest) (*ShiftResponse, error)
	mustEmbedUnimplementedLaborServer()
}

type UnimplementedLaborServer struct{}

func (UnimplementedLaborServer) ClockIn(context.Context, *ShiftRequest) (*ShiftResponse, error) {
	return nil, nil
}

func (UnimplementedLaborServer) mustEmbedUnimplementedLaborServer() {}

type laborClient struct{ cc grpc.ClientConnInterface }

func (c *laborClient) ClockIn(ctx context.Context, in *ShiftRequest, opts ...grpc.CallOption) (*ShiftResponse, error) {
	return nil, nil
}

var Labor_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "labor.Labor",
	Methods: []grpc.MethodDesc{
		{MethodName: "ClockIn"},
	},
}

func RegisterLaborServer(s grpc.ServiceRegistrar, srv LaborServer) {}
`)
	write("jobs/cmd/main.go", `package main

import (
	"context"

	"google.golang.org/grpc"

	"shared/proto/laborpb"
)

type LaborGRPCServer struct {
	laborpb.UnimplementedLaborServer
}

func (s *LaborGRPCServer) ClockIn(ctx context.Context, req *laborpb.ShiftRequest) (*laborpb.ShiftResponse, error) {
	return &laborpb.ShiftResponse{}, nil
}

func NewLaborGRPCServer() laborpb.LaborServer {
	return &LaborGRPCServer{}
}

func main() {
	s := grpc.NewServer()
	laborpb.RegisterLaborServer(s, NewLaborGRPCServer())
}
`)
	return dir
}

func TestServicesInMultiModuleWorkspace(t *testing.T) {
	m, err := loadModule(multiModuleFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	services := buildServices(m)
	if len(services) != 1 {
		t.Fatalf("services = %d, want 1", len(services))
	}
	svc := services[0]
	if svc.N != "labor.Labor" {
		t.Fatalf("service = %q, want labor.Labor", svc.N)
	}
	if len(svc.Methods) != 1 {
		t.Fatalf("methods = %d, want 1", len(svc.Methods))
	}
	clockIn := svc.Methods[0]
	if clockIn.Rq != "ShiftRequest" || clockIn.Rs != "ShiftResponse" {
		t.Fatalf("types = %q -> %q", clockIn.Rq, clockIn.Rs)
	}
	if !strings.Contains(clockIn.Fk, "LaborGRPCServer|ClockIn") {
		t.Fatalf("flow = %q, want the server implementation", clockIn.Fk)
	}
	if strings.Contains(clockIn.Fk, "laborClient") {
		t.Fatalf("flow = %q, resolved to the generated client", clockIn.Fk)
	}
}
