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
