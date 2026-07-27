package singboxapi

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/haoxin/boxfleet/internal/singboxapi/daemonpb"
)

const testSecret = "5PjR2xQvK8mNbT4wZs6HyLd0aGcEuF9i"

func TestSubscribeDeliversEvents(t *testing.T) {
	requests := make(chan *daemonpb.SubscribeConnectionsRequest, 1)
	addr := startFakeDaemon(t, testSecret, func(request *daemonpb.SubscribeConnectionsRequest, stream grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error {
		requests <- request
		err := stream.Send(&daemonpb.ConnectionEvents{
			Reset_: true,
			Events: []*daemonpb.ConnectionEvent{{
				Type: daemonpb.ConnectionEventType_CONNECTION_EVENT_NEW,
				Id:   "3f7c1d2e-0000-4000-8000-000000000001",
				Connection: &daemonpb.Connection{
					Id:            "3f7c1d2e-0000-4000-8000-000000000001",
					Inbound:       "vless-in",
					InboundType:   "vless",
					Network:       "tcp",
					Source:        "203.0.113.7:51544",
					Destination:   "example.com:443",
					User:          "alice",
					CreatedAt:     1753500000000,
					UplinkTotal:   512,
					DownlinkTotal: 2048,
				},
			}},
		})
		if err != nil {
			return err
		}
		return stream.Send(&daemonpb.ConnectionEvents{
			Events: []*daemonpb.ConnectionEvent{{
				Type:          daemonpb.ConnectionEventType_CONNECTION_EVENT_UPDATE,
				Id:            "3f7c1d2e-0000-4000-8000-000000000001",
				UplinkDelta:   64,
				DownlinkDelta: 128,
			}},
		})
	})

	client := dialTest(t, Options{Address: addr, Secret: testSecret, Interval: 2 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}

	first, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if !first.GetReset_() {
		t.Fatal("first batch must carry Reset; a resubscribing collector relies on it to drop stale state")
	}
	if len(first.GetEvents()) != 1 {
		t.Fatalf("len(first.Events) = %d", len(first.GetEvents()))
	}
	connection := first.GetEvents()[0].GetConnection()
	if connection.GetUser() != "alice" {
		t.Fatalf("user = %q", connection.GetUser())
	}
	if connection.GetUplinkTotal() != 512 || connection.GetDownlinkTotal() != 2048 {
		t.Fatalf("totals = %d/%d", connection.GetUplinkTotal(), connection.GetDownlinkTotal())
	}
	if host, port := Endpoint(connection); host != "example.com" || port != 443 {
		t.Fatalf("Endpoint = %q, %d", host, port)
	}

	second, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if second.GetReset_() {
		t.Fatal("only the first batch may carry Reset")
	}
	update := second.GetEvents()[0]
	if update.GetType() != daemonpb.ConnectionEventType_CONNECTION_EVENT_UPDATE {
		t.Fatalf("type = %v", update.GetType())
	}
	if update.GetUplinkDelta() != 64 || update.GetDownlinkDelta() != 128 {
		t.Fatalf("deltas = %d/%d", update.GetUplinkDelta(), update.GetDownlinkDelta())
	}
	if update.GetConnection() != nil {
		t.Fatal("UPDATE events carry no Connection; the fake must not imply otherwise")
	}

	// The server evaluates Interval as time.Duration, so it must arrive in
	// nanoseconds. Sending 2 here instead of 2e9 would request a 2ns tick.
	request := <-requests
	if request.GetInterval() != int64(2*time.Second) {
		t.Fatalf("interval = %d, want %d nanoseconds", request.GetInterval(), int64(2*time.Second))
	}
}

func TestDialRejectsEmptySecret(t *testing.T) {
	// The guard has to hold even against a daemon that would happily accept the
	// anonymous call, because that is exactly the misconfiguration it catches.
	addr := startFakeDaemon(t, "", sendNothingAndClose)

	client, err := Dial(Options{Address: addr, Secret: ""})
	if !errors.Is(err, ErrEmptySecret) {
		if client != nil {
			_ = client.Close()
		}
		t.Fatalf("err = %v, want ErrEmptySecret", err)
	}
	if client != nil {
		t.Fatal("Dial returned a client alongside an error")
	}
}

func TestEmptyDaemonSecretAcceptsAnything(t *testing.T) {
	// Pins the upstream behaviour that motivates ErrEmptySecret: sing-box's
	// authenticate() returns nil immediately when its configured secret is
	// empty (daemon/server.go:50), so an empty-secret node serves its whole
	// control plane unauthenticated. If this test ever fails against a
	// refreshed copy of the interceptor, upstream fixed the bypass and the
	// rationale in ErrEmptySecret needs revisiting — the refusal itself stays.
	addr := startFakeDaemon(t, "", sendNothingAndClose)

	connection, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := daemonpb.NewStartedServiceClient(connection).SubscribeConnections(ctx, &daemonpb.SubscribeConnectionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF from an unauthenticated stream", err)
	}
}

func TestDialRejectsNonLoopbackAddress(t *testing.T) {
	for _, address := range []string{
		"198.51.100.10:9090",
		"[2001:db8::1]:9090",
		"node.example.com:9090",
	} {
		client, err := Dial(Options{Address: address, Secret: testSecret})
		if client != nil {
			_ = client.Close()
		}
		if !errors.Is(err, ErrNonLoopbackAddress) {
			t.Fatalf("Dial(%q) err = %v, want ErrNonLoopbackAddress", address, err)
		}
	}
}

func TestDialAcceptsLoopbackForms(t *testing.T) {
	for _, address := range []string{"127.0.0.1:9090", "127.0.0.53:9090", "[::1]:9090", "localhost:9090"} {
		client, err := Dial(Options{Address: address, Secret: testSecret})
		if err != nil {
			t.Fatalf("Dial(%q) = %v", address, err)
		}
		_ = client.Close()
	}
}

func TestDialRejectsMalformedOptions(t *testing.T) {
	for name, options := range map[string]Options{
		"empty address":     {Address: "", Secret: testSecret},
		"no port":           {Address: "127.0.0.1", Secret: testSecret},
		"negative interval": {Address: "127.0.0.1:9090", Secret: testSecret, Interval: -time.Second},
		// An IPv6 literal without brackets is not a host:port at all, so it
		// fails as malformed rather than as non-loopback.
		"unbracketed ipv6": {Address: "2001:db8::1:9090", Secret: testSecret},
	} {
		client, err := Dial(options)
		if client != nil {
			_ = client.Close()
		}
		if err == nil {
			t.Fatalf("Dial(%s) succeeded", name)
		}
	}
}

func TestSubscribeRejectsWrongSecret(t *testing.T) {
	addr := startFakeDaemon(t, testSecret, sendNothingAndClose)
	client := dialTest(t, Options{Address: addr, Secret: "wrong-secret"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.Subscribe(ctx)
	if err != nil {
		// A stream RPC may surface the rejection at either point depending on
		// how far the handshake got, so both are accepted, and both must be
		// Unauthenticated.
		assertCode(t, err, codes.Unauthenticated)
		return
	}
	_, err = stream.Recv()
	assertCode(t, err, codes.Unauthenticated)
}

func TestStreamEndsWhenDaemonClosesIt(t *testing.T) {
	addr := startFakeDaemon(t, testSecret, func(_ *daemonpb.SubscribeConnectionsRequest, stream grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error {
		return stream.Send(&daemonpb.ConnectionEvents{Reset_: true})
	})
	client := dialTest(t, Options{Address: addr, Secret: testSecret})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF on a cleanly closed stream", err)
	}
}

func TestSubscribeStopsOnContextCancel(t *testing.T) {
	handlerReturned := make(chan error, 1)
	addr := startFakeDaemon(t, testSecret, func(_ *daemonpb.SubscribeConnectionsRequest, stream grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error {
		if err := stream.Send(&daemonpb.ConnectionEvents{Reset_: true}); err != nil {
			handlerReturned <- err
			return err
		}
		<-stream.Context().Done()
		err := stream.Context().Err()
		handlerReturned <- err
		return err
	})
	client := dialTest(t, Options{Address: addr, Secret: testSecret})

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.Subscribe(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		cancel()
		t.Fatal(err)
	}

	cancel()
	if _, err := stream.Recv(); !errors.Is(err, context.Canceled) {
		assertCode(t, err, codes.Canceled)
	}

	// Cancellation must reach the daemon too: a collector that restarts its
	// subscription in a loop would otherwise leak a server-side goroutine and a
	// tracker subscription per restart.
	select {
	case <-handlerReturned:
	case <-time.After(10 * time.Second):
		t.Fatal("daemon handler did not observe the cancellation")
	}
}

func TestEndpointPrefersDestinationOverEmptyDomain(t *testing.T) {
	for name, testCase := range map[string]struct {
		connection *daemonpb.Connection
		wantHost   string
		wantPort   uint16
	}{
		// The shape BoxFleet actually sees: no sniff action, so Domain is empty
		// and the host is only in Destination.
		"empty domain": {
			connection: &daemonpb.Connection{Destination: "example.com:443"},
			wantHost:   "example.com",
			wantPort:   443,
		},
		"domain set wins": {
			connection: &daemonpb.Connection{Destination: "93.184.216.34:443", Domain: "example.com"},
			wantHost:   "example.com",
			wantPort:   443,
		},
		"ipv4 destination": {
			connection: &daemonpb.Connection{Destination: "93.184.216.34:80"},
			wantHost:   "93.184.216.34",
			wantPort:   80,
		},
		"ipv6 destination loses its brackets": {
			connection: &daemonpb.Connection{Destination: "[2606:2800:220:1:248:1893:25c8:1946]:443"},
			wantHost:   "2606:2800:220:1:248:1893:25c8:1946",
			wantPort:   443,
		},
		"destination without a port": {
			connection: &daemonpb.Connection{Destination: "example.com"},
			wantHost:   "example.com",
			wantPort:   0,
		},
		"empty connection": {
			connection: &daemonpb.Connection{},
			wantHost:   "",
			wantPort:   0,
		},
		"nil connection": {
			connection: nil,
			wantHost:   "",
			wantPort:   0,
		},
	} {
		host, port := Endpoint(testCase.connection)
		if host != testCase.wantHost || port != testCase.wantPort {
			t.Errorf("%s: Endpoint = %q, %d; want %q, %d", name, host, port, testCase.wantHost, testCase.wantPort)
		}
	}
}

// --- fake daemon ------------------------------------------------------------

type subscribeHandler func(*daemonpb.SubscribeConnectionsRequest, grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error

func sendNothingAndClose(*daemonpb.SubscribeConnectionsRequest, grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error {
	return nil
}

type fakeDaemon struct {
	daemonpb.UnimplementedStartedServiceServer
	handle subscribeHandler
}

func (f fakeDaemon) SubscribeConnections(request *daemonpb.SubscribeConnectionsRequest, stream grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error {
	return f.handle(request, stream)
}

// startFakeDaemon runs an in-process gRPC server on loopback that answers
// SubscribeConnections and authenticates exactly as sing-box does. No real
// sing-box, no network beyond 127.0.0.1.
func startFakeDaemon(t *testing.T, secret string, handle subscribeHandler) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.ChainStreamInterceptor(fakeStreamAuthInterceptor(secret)))
	daemonpb.RegisterStartedServiceServer(server, fakeDaemon{handle: handle})
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	return listener.Addr().String()
}

// fakeStreamAuthInterceptor mirrors daemon/server.go:39-66 at v1.14.0-beta.2,
// including the empty-secret bypass. Reproducing the real check — rather than a
// convenient approximation — is what makes the credential tests meaningful:
// they assert the client speaks the header sing-box actually reads.
func fakeStreamAuthInterceptor(secret string) grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if secret != "" {
			md, loaded := metadata.FromIncomingContext(stream.Context())
			if !loaded {
				return status.Error(codes.Unauthenticated, "missing metadata")
			}
			values := md.Get("authorization")
			if len(values) == 0 {
				return status.Error(codes.Unauthenticated, "missing authorization")
			}
			token, isBearer := strings.CutPrefix(values[0], "Bearer ")
			if !isBearer || token != secret {
				return status.Error(codes.Unauthenticated, "invalid authorization")
			}
		}
		return handler(server, stream)
	}
}

func dialTest(t *testing.T, options Options) *Client {
	t.Helper()
	client, err := Dial(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("status.Code(%v) = %s, want %s", err, got, want)
	}
}
