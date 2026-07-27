package daemonpb

import (
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// upstream_conformance_test.go checks that everything vendored here matches
// upstream. This file checks the deliberate deviations — the things a
// conformance diff cannot express, because they are the point of the trim.

const upstreamTag = "v1.14.0-beta.2"

func TestOnlyTheSubscriptionRPCIsReachable(t *testing.T) {
	// Upstream's StartedService declares 34 RPCs, including StopService,
	// ReloadService, CloseConnection, CloseAllConnections, SelectOutbound and
	// TriggerDebugCrash. protoc-gen-go-grpc generates client methods from the
	// descriptor, so declaring one RPC is what makes the rest unreachable from
	// BoxFleet code by construction rather than by review discipline. Adding
	// any other RPC here re-arms that.
	services := File_daemonpb_singbox_daemon_connections_proto.Services()
	if services.Len() != 1 {
		t.Fatalf("file declares %d services, want 1", services.Len())
	}
	service := services.Get(0)
	if got := service.Methods().Len(); got != 1 {
		t.Fatalf("StartedService declares %d methods, want only SubscribeConnections", got)
	}
	if got := string(service.Methods().Get(0).Name()); got != "SubscribeConnections" {
		t.Fatalf("method = %s, want SubscribeConnections", got)
	}
}

func TestMethodPathIsUpstreamsExactly(t *testing.T) {
	// gRPC routes on this string alone, and it is derived from the proto
	// package and service name. That is the entire reason both are kept as
	// upstream spells them instead of renamed to something BoxFleet-shaped.
	const want = "/daemon.StartedService/SubscribeConnections"
	if StartedService_SubscribeConnections_FullMethodName != want {
		t.Fatalf("method path = %q, want %q", StartedService_SubscribeConnections_FullMethodName, want)
	}
	if got := string(File_daemonpb_singbox_daemon_connections_proto.Services().Get(0).FullName()); got != "daemon.StartedService" {
		t.Fatalf("service = %q, want daemon.StartedService", got)
	}
}

func TestNeverPopulatedFieldsStayUnreachable(t *testing.T) {
	// Connection.uplink (14) and Connection.downlink (15) exist upstream but
	// are never assigned, so they decode as a constant zero and quietly invite
	// someone to build on them. Reserving keeps the numbers claimed against a
	// future upstream reuse while removing the Go accessors entirely.
	message := File_daemonpb_singbox_daemon_connections_proto.Messages().ByName("Connection")
	if message == nil {
		t.Fatal("Connection message is missing")
	}
	ranges := message.ReservedRanges()
	for _, number := range []protoreflect.FieldNumber{14, 15} {
		if field := message.Fields().ByNumber(number); field != nil {
			t.Errorf("field %d is declared as %q; it must stay reserved", number, field.Name())
		}
		if !ranges.Has(number) {
			t.Errorf("field %d is not reserved on Connection", number)
		}
	}
}

func TestProvenanceIsRecorded(t *testing.T) {
	// Cheap guard against the pin being bumped in one place and not the others.
	// The tag has to appear in the proto header, in the generated files that
	// carry that header through, in the fixture name, and in the README's
	// regeneration recipe.
	for _, path := range []string{
		"singbox_daemon_connections.proto",
		"singbox_daemon_connections.pb.go",
		"singbox_daemon_connections_grpc.pb.go",
		"../README.md",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), upstreamTag) {
			t.Errorf("%s does not record the pinned upstream tag %s", path, upstreamTag)
		}
	}
	if _, err := os.Stat(upstreamDescriptorSet); err != nil {
		t.Errorf("upstream descriptor fixture for %s is missing: %v", upstreamTag, err)
	}
}
