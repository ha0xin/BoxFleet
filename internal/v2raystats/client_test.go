package v2raystats

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"

	"github.com/haoxin/boxfleet/internal/v2rayapi"
)

func TestQuery(t *testing.T) {
	server := grpc.NewServer()
	v2rayapi.RegisterStatsServiceServer(server, fakeStatsServer{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	go func() {
		_ = server.Serve(listener)
	}()

	stats, err := Query(context.Background(), listener.Addr().String(), []string{"alice"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("len(stats) = %d", len(stats))
	}
	if stats[0].Name != "user>>>alice>>>traffic>>>downlink" || stats[0].Value != 2048 {
		t.Fatalf("stats[0] = %#v", stats[0])
	}
	if stats[1].Name != "user>>>alice>>>traffic>>>uplink" || stats[1].Value != 512 {
		t.Fatalf("stats[1] = %#v", stats[1])
	}
}

type fakeStatsServer struct {
	v2rayapi.UnimplementedStatsServiceServer
}

func (fakeStatsServer) QueryStats(context.Context, *v2rayapi.QueryStatsRequest) (*v2rayapi.QueryStatsResponse, error) {
	return &v2rayapi.QueryStatsResponse{
		Stat: []*v2rayapi.Stat{
			{Name: "user>>>alice>>>traffic>>>uplink", Value: 512},
			{Name: "user>>>alice>>>traffic>>>downlink", Value: 2048},
		},
	}, nil
}
