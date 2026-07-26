package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/singboxapi/daemonpb"
)

// collectorTestSecret is long enough to pass the server-side length check the
// telemetry row enforces, so the fixtures read like a real rendered config.
const collectorTestSecret = "5PjR2xQvK8mNbT4wZs6HyLd0aGcEuF9i"

var collectorBase = time.Date(2026, 7, 26, 12, 3, 30, 0, time.UTC)

// --- discovery --------------------------------------------------------------

func TestConnectionTelemetryOptionsFromRenderedConfig(t *testing.T) {
	t.Parallel()
	options, err := connectionTelemetryOptions([]byte(`{
	  "log": {"level": "info"},
	  "inbounds": [{"type": "vless", "tag": "vless-in"}],
	  "services": [
	    {"type": "resolved", "tag": "resolved-in"},
	    {"type": "api", "tag": "boxfleet-api", "listen": "127.0.0.1", "listen_port": 9091, "secret": "` + collectorTestSecret + `"}
	  ]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if options.Address != "127.0.0.1:9091" {
		t.Fatalf("Address = %q", options.Address)
	}
	if options.Secret != collectorTestSecret {
		t.Fatalf("Secret = %q", options.Secret)
	}
	if options.Interval != defaultConnectionUpdateInterval {
		t.Fatalf("Interval = %s", options.Interval)
	}
}

func TestConnectionTelemetryOptionsRejectsUnsafeOrAbsentService(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		config     string
		wantAbsent bool
	}{
		// The shape every node in the fleet runs today: a 1.13 config has no
		// services block at all, and the collector must read that as "off",
		// not as an error worth logging every poll.
		"no services block": {config: `{"inbounds": [], "outbounds": []}`, wantAbsent: true},
		"no api service":    {config: `{"services": [{"type": "resolved"}]}`, wantAbsent: true},
		// An empty secret disables sing-box's own authentication, exposing
		// StopService and CloseAllConnections to anything on the listener.
		"empty secret": {config: `{"services": [{"type": "api", "listen": "127.0.0.1", "listen_port": 9091}]}`},
		"no listen":    {config: `{"services": [{"type": "api", "listen_port": 9091, "secret": "` + collectorTestSecret + `"}]}`},
		"no port":      {config: `{"services": [{"type": "api", "listen": "127.0.0.1", "secret": "` + collectorTestSecret + `"}]}`},
		"not json":     {config: `not json at all`},
	} {
		_, err := connectionTelemetryOptions([]byte(testCase.config))
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if got := err == errNoConnectionTelemetry; got != testCase.wantAbsent {
			t.Fatalf("%s: errNoConnectionTelemetry = %v, want %v (err = %v)", name, got, testCase.wantAbsent, err)
		}
	}
}

// --- aggregation ------------------------------------------------------------

func TestConnectionCollectorAggregatesConnectionLifecycle(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()
	opened := collectorBase
	closed := collectorBase.Add(30 * time.Second)

	collector.apply(resetBatch(newEvent("c1", liveConnection("c1", "alice", opened, 0, 0))), opened)
	collector.apply(batch(updateEvent("c1", 100, 900)), opened.Add(10*time.Second))
	// The final totals on CLOSED close the gap between the last traffic tick and
	// the connection's death: 50 up and 300 down that no UPDATE ever carried.
	closedConnection := liveConnection("c1", "alice", opened, 150, 1200)
	closedConnection.ClosedAt = closed.UnixMilli()
	collector.apply(batch(closedEvent("c1", closed, closedConnection)), closed)

	buckets, coverage, _, ok := collector.Drain(closed.Add(time.Second))
	if !ok {
		t.Fatal("Drain reported nothing to report")
	}
	if len(buckets) != 1 {
		t.Fatalf("len(buckets) = %d: %+v", len(buckets), buckets)
	}
	bucket := buckets[0]
	if bucket.BucketStart != "2026-07-26T12:00:00.000Z" {
		t.Fatalf("BucketStart = %q", bucket.BucketStart)
	}
	if bucket.AuthName != "alice" || bucket.TargetHost != "example.com" || bucket.TargetPort != 443 {
		t.Fatalf("dimensions = %q %q %d", bucket.AuthName, bucket.TargetHost, bucket.TargetPort)
	}
	if bucket.SourceIP != "203.0.113.7" {
		t.Fatalf("SourceIP = %q (the ephemeral source port must be dropped)", bucket.SourceIP)
	}
	if bucket.UplinkBytes != 150 || bucket.DownlinkBytes != 1200 {
		t.Fatalf("bytes = %d/%d, want the connection's final totals", bucket.UplinkBytes, bucket.DownlinkBytes)
	}
	if bucket.ConnectionsOpened != 1 || bucket.ConnectionsClosed != 1 {
		t.Fatalf("connections = %d opened / %d closed", bucket.ConnectionsOpened, bucket.ConnectionsClosed)
	}
	if bucket.DurationMsTotal != 30_000 {
		t.Fatalf("DurationMsTotal = %d", bucket.DurationMsTotal)
	}
	if bucket.WindowStart != "2026-07-26T12:03:30.000Z" || bucket.WindowEnd != "2026-07-26T12:04:00.000Z" {
		t.Fatalf("window = %q..%q", bucket.WindowStart, bucket.WindowEnd)
	}
	want := model.ConnectionCoverage{
		ConnectionsObserved:   1,
		ConnectionsAttributed: 1,
		BytesObserved:         1350,
		BytesAttributed:       1350,
	}
	if coverage != want {
		t.Fatalf("coverage = %+v, want %+v", coverage, want)
	}
}

func TestConnectionCollectorSeparatesDimensionsAndAttribution(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()

	alice := liveConnection("c1", "alice", collectorBase, 10, 20)
	// Single-user Shadowsocks never populates `user`; the bytes are still
	// recorded, just against nothing.
	anonymous := liveConnection("c2", "", collectorBase, 5, 5)
	anonymous.Destination = "cdn.example.net:80"
	collector.apply(resetBatch(newEvent("c1", alice), newEvent("c2", anonymous)), collectorBase)

	buckets, coverage, _, ok := collector.Drain(collectorBase.Add(time.Minute))
	if !ok {
		t.Fatal("Drain reported nothing to report")
	}
	if len(buckets) != 2 {
		t.Fatalf("len(buckets) = %d", len(buckets))
	}
	want := model.ConnectionCoverage{
		ConnectionsObserved:     2,
		ConnectionsAttributed:   1,
		ConnectionsUnattributed: 1,
		BytesObserved:           40,
		BytesAttributed:         30,
	}
	if coverage != want {
		t.Fatalf("coverage = %+v, want %+v", coverage, want)
	}
	if ratio := coverage.ConnectionAttributionRatio(); ratio != 0.75 {
		t.Fatalf("ConnectionAttributionRatio = %v", ratio)
	}
}

func TestConnectionCollectorDrainResetsWindowAndCounters(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()
	collector.apply(resetBatch(newEvent("c1", liveConnection("c1", "alice", collectorBase, 10, 10))), collectorBase)
	if _, _, _, ok := collector.Drain(collectorBase.Add(time.Minute)); !ok {
		t.Fatal("first Drain reported nothing")
	}
	// An idle window must not report at all, and a still-live connection must
	// not be counted a second time for merely existing.
	if _, _, _, ok := collector.Drain(collectorBase.Add(2 * time.Minute)); ok {
		t.Fatal("an idle window produced a report")
	}
	collector.apply(batch(updateEvent("c1", 7, 0)), collectorBase.Add(2*time.Minute))
	_, coverage, windowStart, ok := collector.Drain(collectorBase.Add(3 * time.Minute))
	if !ok {
		t.Fatal("Drain reported nothing after an update")
	}
	if !windowStart.Equal(collectorBase.Add(2 * time.Minute)) {
		t.Fatalf("windowStart = %s", windowStart)
	}
	if coverage.ConnectionsObserved != 1 || coverage.BytesObserved != 7 {
		t.Fatalf("coverage = %+v", coverage)
	}
}

// --- resets, reconnects, restarts -------------------------------------------

func TestConnectionCollectorResetSweepsConnectionsSingBoxNoLongerKnows(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()
	collector.apply(resetBatch(
		newEvent("c1", liveConnection("c1", "alice", collectorBase, 0, 0)),
		newEvent("c2", liveConnection("c2", "bob", collectorBase, 0, 0)),
	), collectorBase)
	if got := collector.liveCount(); got != 2 {
		t.Fatalf("live = %d", got)
	}

	// A second full-state replay that mentions only c2 proves c1 is gone: it
	// closed while the subscription was down and fell out of the ring, or
	// sing-box restarted.
	later := collectorBase.Add(time.Minute)
	collector.apply(resetBatch(newEvent("c2", liveConnection("c2", "bob", collectorBase, 0, 0))), later)
	if got := collector.liveCount(); got != 1 {
		t.Fatalf("live after sweep = %d, want only c2", got)
	}
	if !collector.tracks("c2") {
		t.Fatal("the replayed connection was swept instead of retained")
	}
	_, coverage, _, _ := collector.Drain(later.Add(time.Second))
	if coverage.StreamResets != 1 {
		t.Fatalf("StreamResets = %d, want 1 (the first subscription is not a lost-continuity event)", coverage.StreamResets)
	}
}

func TestConnectionCollectorReplayDoesNotDoubleCount(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()
	collector.apply(resetBatch(newEvent("c1", liveConnection("c1", "alice", collectorBase, 0, 0))), collectorBase)
	collector.apply(batch(updateEvent("c1", 100, 900)), collectorBase.Add(10*time.Second))

	// Reconnect: sing-box replays c1 as NEW carrying the totals we have already
	// recorded, plus 60 bytes that accrued while the subscription was down.
	reconnect := collectorBase.Add(20 * time.Second)
	collector.apply(resetBatch(newEvent("c1", liveConnection("c1", "alice", collectorBase, 130, 930))), reconnect)

	_, coverage, _, ok := collector.Drain(reconnect.Add(time.Second))
	if !ok {
		t.Fatal("Drain reported nothing")
	}
	if coverage.BytesObserved != 1060 {
		t.Fatalf("BytesObserved = %d, want 1060 (1000 recorded + a 60-byte residual, not 2060)", coverage.BytesObserved)
	}
	if coverage.ConnectionsObserved != 1 {
		t.Fatalf("ConnectionsObserved = %d, want the replayed connection counted once", coverage.ConnectionsObserved)
	}
	if coverage.StreamResets != 1 {
		t.Fatalf("StreamResets = %d", coverage.StreamResets)
	}
}

func TestConnectionCollectorRecoversClosedConnectionsFromTheReplayRing(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()
	collector.apply(resetBatch(), collectorBase)

	// A connection that opened and closed entirely while the subscription was
	// down comes back on the next subscription as NEW with a non-zero ClosedAt.
	closedAt := collectorBase.Add(15 * time.Second)
	replayed := liveConnection("c9", "carol", collectorBase, 300, 700)
	replayed.ClosedAt = closedAt.UnixMilli()
	reconnect := collectorBase.Add(20 * time.Second)
	collector.apply(resetBatch(newEvent("c9", replayed)), reconnect)

	buckets, coverage, _, ok := collector.Drain(reconnect.Add(time.Second))
	if !ok {
		t.Fatal("Drain reported nothing")
	}
	if len(buckets) != 1 || buckets[0].ConnectionsOpened != 1 || buckets[0].ConnectionsClosed != 1 {
		t.Fatalf("buckets = %+v", buckets)
	}
	if buckets[0].UplinkBytes != 300 || buckets[0].DownlinkBytes != 700 {
		t.Fatalf("recovered bytes = %d/%d", buckets[0].UplinkBytes, buckets[0].DownlinkBytes)
	}
	if buckets[0].DurationMsTotal != 15_000 {
		t.Fatalf("DurationMsTotal = %d", buckets[0].DurationMsTotal)
	}
	if coverage.ConnectionsOrphaned != 1 {
		t.Fatalf("ConnectionsOrphaned = %d, want the recovery reported as a gap", coverage.ConnectionsOrphaned)
	}

	// Replaying the same closed connection again — the ring keeps it for up to
	// 1000 closes — must be recognised, not counted twice.
	collector.apply(resetBatch(newEvent("c9", replayed)), reconnect.Add(30*time.Second))
	if _, coverage, _, ok := collector.Drain(reconnect.Add(time.Minute)); ok && coverage.BytesObserved != 0 {
		t.Fatalf("a second replay recorded %d bytes", coverage.BytesObserved)
	}
}

func TestConnectionCollectorSkipsClosesAlreadyReportedBeforeAnAgentRestart(t *testing.T) {
	t.Parallel()
	closedAt := collectorBase.Add(15 * time.Second)
	collector := newTestCollector()
	// A fresh agent process has an empty id ring, so the durable high-water mark
	// is the only thing standing between it and re-reporting the whole ring.
	collector.setStartupCloseHighWater(closedAt.UnixMilli())

	stale := liveConnection("c9", "carol", collectorBase, 300, 700)
	stale.ClosedAt = closedAt.UnixMilli()
	fresh := liveConnection("c10", "carol", collectorBase, 5, 6)
	fresh.ClosedAt = closedAt.Add(time.Second).UnixMilli()
	collector.apply(resetBatch(newEvent("c9", stale), newEvent("c10", fresh)), closedAt.Add(5*time.Second))

	buckets, coverage, _, ok := collector.Drain(closedAt.Add(10 * time.Second))
	if !ok {
		t.Fatal("Drain reported nothing")
	}
	if len(buckets) != 1 || buckets[0].UplinkBytes != 5 || buckets[0].DownlinkBytes != 6 {
		t.Fatalf("buckets = %+v, want only the close newer than the high-water mark", buckets)
	}
	if coverage.BytesObserved != 11 {
		t.Fatalf("BytesObserved = %d", coverage.BytesObserved)
	}
	if got := collector.CloseHighWaterMs(); got != fresh.ClosedAt {
		t.Fatalf("CloseHighWaterMs = %d, want %d", got, fresh.ClosedAt)
	}
}

func TestConnectionCollectorSurvivesASingBoxRestart(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()
	collector.apply(resetBatch(newEvent("old-1", liveConnection("old-1", "alice", collectorBase, 0, 0))), collectorBase)
	collector.apply(batch(updateEvent("old-1", 1_000_000, 2_000_000)), collectorBase.Add(10*time.Second))
	if _, _, _, ok := collector.Drain(collectorBase.Add(20 * time.Second)); !ok {
		t.Fatal("Drain reported nothing before the restart")
	}

	// sing-box restarts: connection ids are fresh uuids and every total starts
	// again from zero. A collector that carried the old accounting forward would
	// either double count or, worse, subtract.
	restarted := collectorBase.Add(30 * time.Second)
	collector.apply(resetBatch(newEvent("new-1", liveConnection("new-1", "alice", restarted, 0, 0))), restarted)
	collector.apply(batch(updateEvent("new-1", 40, 60)), restarted.Add(5*time.Second))

	_, coverage, _, ok := collector.Drain(restarted.Add(10 * time.Second))
	if !ok {
		t.Fatal("Drain reported nothing after the restart")
	}
	if coverage.BytesObserved != 100 {
		t.Fatalf("BytesObserved = %d, want only the post-restart traffic", coverage.BytesObserved)
	}
	if coverage.StreamResets != 1 {
		t.Fatalf("StreamResets = %d", coverage.StreamResets)
	}
	if collector.liveCount() != 1 || !collector.tracks("new-1") {
		t.Fatal("the pre-restart connection was not swept")
	}
}

// --- gap detection ----------------------------------------------------------

func TestConnectionCollectorReportsOrphanedCloses(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()
	collector.apply(resetBatch(), collectorBase)

	// A NEW dropped by sing-box's 64-slot listener buffer leaves the collector
	// with a close it never saw open. The close carries full identity and final
	// totals, so the traffic is recovered in full.
	closedAt := collectorBase.Add(10 * time.Second)
	connection := liveConnection("ghost", "alice", collectorBase, 11, 22)
	connection.ClosedAt = closedAt.UnixMilli()
	collector.apply(batch(closedEvent("ghost", closedAt, connection)), closedAt)

	// A close with no Connection at all cannot be attributed to anything, so it
	// is counted as a gap and nothing else.
	collector.apply(batch(closedEvent("phantom", closedAt, nil)), closedAt)

	buckets, coverage, _, ok := collector.Drain(closedAt.Add(time.Second))
	if !ok {
		t.Fatal("Drain reported nothing")
	}
	if len(buckets) != 1 || buckets[0].UplinkBytes != 11 || buckets[0].DownlinkBytes != 22 {
		t.Fatalf("buckets = %+v", buckets)
	}
	if coverage.ConnectionsOrphaned != 2 {
		t.Fatalf("ConnectionsOrphaned = %d, want both unmatched closes", coverage.ConnectionsOrphaned)
	}
	if coverage.ConnectionsObserved != 1 {
		t.Fatalf("ConnectionsObserved = %d, want only the close that carried identity", coverage.ConnectionsObserved)
	}
}

func TestConnectionCollectorIgnoresUpdatesForUntrackedConnections(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()
	collector.apply(resetBatch(), collectorBase)
	// An UPDATE is an id plus two deltas. With no NEW there is no identity to
	// attribute the bytes to; they must be dropped rather than invented.
	collector.apply(batch(updateEvent("unknown", 500, 500)), collectorBase)
	if _, _, _, ok := collector.Drain(collectorBase.Add(time.Second)); ok {
		t.Fatal("an untracked UPDATE produced a report")
	}
}

// --- bounded memory ---------------------------------------------------------

func TestConnectionCollectorEnforcesTheBucketCapAndCountsIt(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()
	overflow := 5
	events := make([]*daemonpb.ConnectionEvent, 0, maxPendingConnectionBuckets+overflow)
	for i := range maxPendingConnectionBuckets + overflow {
		id := fmt.Sprintf("c%d", i)
		connection := liveConnection(id, "alice", collectorBase, 1, 1)
		// A distinct destination per connection is a distinct dimension tuple,
		// which is a distinct bucket.
		connection.Destination = fmt.Sprintf("host%d.example.com:443", i)
		events = append(events, newEvent(id, connection))
	}
	collector.apply(resetBatch(events...), collectorBase)

	buckets, coverage, _, ok := collector.Drain(collectorBase.Add(time.Second))
	if !ok {
		t.Fatal("Drain reported nothing")
	}
	if len(buckets) != maxPendingConnectionBuckets {
		t.Fatalf("len(buckets) = %d, want the map capped at %d", len(buckets), maxPendingConnectionBuckets)
	}
	if coverage.DroppedBuckets != int64(overflow) {
		t.Fatalf("DroppedBuckets = %d, want %d — dropping without counting would defeat the coverage design", coverage.DroppedBuckets, overflow)
	}
	// Dropped bytes are not claimed as observed: the denominator has to stay
	// honest about what was actually recorded.
	if coverage.BytesObserved != int64(maxPendingConnectionBuckets*2) {
		t.Fatalf("BytesObserved = %d", coverage.BytesObserved)
	}
}

func TestConnectionCollectorEnforcesTheLiveConnectionCapAndCountsIt(t *testing.T) {
	t.Parallel()
	collector := newTestCollector()
	overflow := 3
	events := make([]*daemonpb.ConnectionEvent, 0, maxTrackedConnections+overflow)
	for i := range maxTrackedConnections + overflow {
		id := fmt.Sprintf("c%d", i)
		// Identical dimensions on purpose: this test is about the identity map,
		// not the bucket map, so everything folds into one bucket.
		events = append(events, newEvent(id, liveConnection(id, "alice", collectorBase, 0, 0)))
	}
	collector.apply(resetBatch(events...), collectorBase)

	if got := collector.liveCount(); got != maxTrackedConnections {
		t.Fatalf("live = %d, want the identity map capped at %d", got, maxTrackedConnections)
	}
	buckets, coverage, _, ok := collector.Drain(collectorBase.Add(time.Second))
	if !ok {
		t.Fatal("Drain reported nothing")
	}
	if coverage.DroppedBuckets != int64(overflow) {
		t.Fatalf("DroppedBuckets = %d, want %d", coverage.DroppedBuckets, overflow)
	}
	if len(buckets) != 1 || buckets[0].ConnectionsOpened != int64(maxTrackedConnections) {
		t.Fatalf("buckets = %+v, want only the tracked opens counted", buckets)
	}

	// The refused connections are not lost: their CLOSED events carry identity
	// and final totals, so the orphan path recovers them in full — and a refused
	// connection was never counted as opened, so recovering it cannot double it.
	refused := fmt.Sprintf("c%d", maxTrackedConnections)
	closedAt := collectorBase.Add(time.Minute)
	connection := liveConnection(refused, "alice", collectorBase, 90, 10)
	connection.ClosedAt = closedAt.UnixMilli()
	collector.apply(batch(closedEvent(refused, closedAt, connection)), closedAt)
	buckets, coverage, _, ok = collector.Drain(closedAt.Add(time.Second))
	if !ok {
		t.Fatal("Drain reported nothing after the recovery")
	}
	if len(buckets) != 1 || buckets[0].ConnectionsOpened != 1 || buckets[0].ConnectionsClosed != 1 {
		t.Fatalf("buckets = %+v", buckets)
	}
	if coverage.BytesObserved != 100 {
		t.Fatalf("BytesObserved = %d", coverage.BytesObserved)
	}
}

func TestClosedIDRingEvictsInFIFOOrder(t *testing.T) {
	t.Parallel()
	ring := newClosedIDRing(3)
	for _, id := range []string{"a", "b", "c", "d"} {
		ring.add(id)
	}
	if ring.contains("a") {
		t.Fatal("the oldest id was not evicted; the ring is unbounded")
	}
	for _, id := range []string{"b", "c", "d"} {
		if !ring.contains(id) {
			t.Fatalf("%q was evicted early", id)
		}
	}
	// Re-adding must not consume a slot, or a hot connection id would evict the
	// whole ring.
	ring.add("d")
	if !ring.contains("b") {
		t.Fatal("a duplicate add evicted a live entry")
	}
}

// --- the stream, end to end -------------------------------------------------

func TestConnectionCollectorRunConsumesTheStreamAndReconnects(t *testing.T) {
	t.Parallel()
	opened := collectorBase
	closedAt := collectorBase.Add(20 * time.Second)
	replayed := liveConnection("c1", "alice", opened, 400, 600)
	replayed.ClosedAt = closedAt.UnixMilli()

	var subscriptions int
	var mu sync.Mutex
	address := startFakeSingBoxDaemon(t, collectorTestSecret, func(_ *daemonpb.SubscribeConnectionsRequest, stream grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error {
		mu.Lock()
		subscriptions++
		attempt := subscriptions
		mu.Unlock()
		if attempt == 1 {
			// First subscription: the connection opens and reports some traffic,
			// then the daemon ends the stream mid-connection.
			if err := stream.Send(resetBatch(newEvent("c1", liveConnection("c1", "alice", opened, 0, 0)))); err != nil {
				return err
			}
			return stream.Send(batch(updateEvent("c1", 100, 300)))
		}
		// Second subscription: sing-box replays its state, and the connection
		// has since closed with higher totals. Only the residual may be added.
		if err := stream.Send(resetBatch(newEvent("c1", replayed))); err != nil {
			return err
		}
		<-stream.Context().Done()
		return stream.Context().Err()
	})

	collector := newConnectionCollector(ConnectionTelemetryOptions{
		Address:  address,
		Secret:   collectorTestSecret,
		Interval: time.Second,
	})
	collector.minBackoff, collector.maxBackoff = time.Millisecond, 10*time.Millisecond
	// NEW and UPDATE are bucketed at arrival time, so the clock is pinned to keep
	// them in the same bucket as the close the fixture dates explicitly.
	collector.now = func() time.Time { return opened }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		collector.Run(ctx)
	}()

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return subscriptions >= 2
	}, "the collector did not resubscribe after the stream ended")
	// The reconnect's replay is applied inside Recv, so wait for the close to
	// land rather than racing it.
	waitFor(t, func() bool { return collector.CloseHighWaterMs() == closedAt.UnixMilli() }, "the replayed close was never applied")

	cancel()
	<-done

	buckets, coverage, _, ok := collector.Drain(closedAt.Add(time.Minute))
	if !ok {
		t.Fatal("Drain reported nothing")
	}
	if len(buckets) != 1 {
		t.Fatalf("len(buckets) = %d: %+v", len(buckets), buckets)
	}
	if buckets[0].UplinkBytes != 400 || buckets[0].DownlinkBytes != 600 {
		t.Fatalf("bytes = %d/%d, want the connection's final totals counted exactly once", buckets[0].UplinkBytes, buckets[0].DownlinkBytes)
	}
	if buckets[0].ConnectionsOpened != 1 || buckets[0].ConnectionsClosed != 1 {
		t.Fatalf("connections = %d/%d", buckets[0].ConnectionsOpened, buckets[0].ConnectionsClosed)
	}
	if coverage.StreamResets != 1 {
		t.Fatalf("StreamResets = %d, want the reconnect reported", coverage.StreamResets)
	}
	if coverage.ConnectionsObserved != 1 {
		t.Fatalf("ConnectionsObserved = %d", coverage.ConnectionsObserved)
	}
}

func TestConnectionCollectorRunIsAQuietNoOpWithoutADaemon(t *testing.T) {
	t.Parallel()
	// The 1.13 case: the node is opted in but its sing-box serves nothing on the
	// endpoint. Run must keep retrying without ever returning an error into the
	// agent's supervision path.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	collector := newConnectionCollector(ConnectionTelemetryOptions{Address: address, Secret: collectorTestSecret})
	collector.minBackoff, collector.maxBackoff = time.Millisecond, 5*time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	collector.Run(ctx)

	if _, _, _, ok := collector.Drain(time.Now().UTC()); ok {
		t.Fatal("a collector that never connected produced a report")
	}
}

func TestConnectionCollectorRunRefusesAnUnsafeEndpoint(t *testing.T) {
	t.Parallel()
	// An empty secret disables authentication in sing-box itself. Dial refuses
	// it, and Run must give up rather than spin: retrying cannot fix a config.
	collector := newConnectionCollector(ConnectionTelemetryOptions{Address: "127.0.0.1:9091"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	collector.Run(ctx)
	if ctx.Err() != nil {
		t.Fatal("Run kept retrying a configuration error instead of returning")
	}
}

// --- agent integration ------------------------------------------------------

func TestReportConnectionsRetriesFromDurableState(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var received []model.ConnectionReport
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/node/connections" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var report model.ConnectionReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Errorf("decode: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if fail {
			fail = false
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		received = append(received, report)
	}))
	defer server.Close()

	agent := newCollectorTestAgent(t, server.URL)
	collector := newTestCollector()
	agent.installCollector(collector)
	collector.apply(resetBatch(newEvent("c1", liveConnection("c1", "alice", collectorBase, 12, 34))), collectorBase)

	if err := agent.ReportConnections(context.Background()); err == nil {
		t.Fatal("the failed POST was not surfaced")
	}
	state, err := agent.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.PendingConnections == nil {
		t.Fatal("the report was not staged durably before the POST, so it is lost")
	}
	if state.PendingConnections.Sequence != 1 || state.PendingConnections.AgentBootID != state.BootID {
		t.Fatalf("idempotency key = %d/%q", state.PendingConnections.Sequence, state.PendingConnections.AgentBootID)
	}
	if state.LastConnectionCloseMs != 0 {
		t.Fatalf("LastConnectionCloseMs = %d, want 0 with no close observed", state.LastConnectionCloseMs)
	}

	// The retry ships the staged report unchanged, and the sequence does not
	// advance: the server dedups on (boot id, sequence) and skips whole batches.
	if err := agent.ReportConnections(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err = agent.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.PendingConnections != nil {
		t.Fatal("the staged report was not cleared after a successful POST")
	}
	if state.ConnectionSequence != 1 {
		t.Fatalf("ConnectionSequence = %d, want the retry to reuse the key", state.ConnectionSequence)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("len(received) = %d", len(received))
	}
	if len(received[0].Buckets) != 1 || received[0].Buckets[0].TargetHost != "example.com" {
		t.Fatalf("buckets = %+v", received[0].Buckets)
	}
	if received[0].Coverage.BytesObserved != 46 {
		t.Fatalf("BytesObserved = %d", received[0].Coverage.BytesObserved)
	}
}

func TestReportConnectionsDoesNotDrainWhileAReportIsStuck(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	agent := newCollectorTestAgent(t, server.URL)
	collector := newTestCollector()
	agent.installCollector(collector)
	collector.apply(resetBatch(newEvent("c1", liveConnection("c1", "alice", collectorBase, 12, 34))), collectorBase)
	if err := agent.ReportConnections(context.Background()); err == nil {
		t.Fatal("the failed POST was not surfaced")
	}

	collector.apply(batch(updateEvent("c1", 5, 5)), collectorBase.Add(time.Second))
	if err := agent.ReportConnections(context.Background()); err == nil {
		t.Fatal("the failed retry was not surfaced")
	}
	state, err := agent.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.ConnectionSequence != 1 {
		t.Fatalf("ConnectionSequence = %d: a second report was minted while the first was unacknowledged", state.ConnectionSequence)
	}
	// The newer traffic stays in the collector rather than being dropped or
	// merged into a report the server may already hold.
	if _, coverage, _, ok := collector.Drain(collectorBase.Add(time.Minute)); !ok || coverage.BytesObserved != 10 {
		t.Fatalf("the collector lost the window it kept: ok = %v, coverage = %+v", ok, coverage)
	}
}

func TestReportConnectionsIsANoOpWithoutACollector(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("a node without connection telemetry posted to %s", r.URL.Path)
	}))
	defer server.Close()
	agent := newCollectorTestAgent(t, server.URL)
	if err := agent.ReportConnections(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFlushConnectionsToStateStagesTheFinalWindow(t *testing.T) {
	t.Parallel()
	agent := newCollectorTestAgent(t, "https://server.invalid")
	collector := newTestCollector()
	agent.installCollector(collector)
	closedAt := collectorBase.Add(5 * time.Second)
	connection := liveConnection("c1", "alice", collectorBase, 7, 8)
	connection.ClosedAt = closedAt.UnixMilli()
	collector.apply(resetBatch(newEvent("c1", connection)), collectorBase)
	collector.apply(batch(closedEvent("c1", closedAt, connection)), closedAt)

	agent.flushConnectionsToState()

	state, err := agent.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.PendingConnections == nil {
		t.Fatal("shutdown discarded the collector's window instead of staging it")
	}
	if len(state.PendingConnections.Buckets) != 1 {
		t.Fatalf("buckets = %+v", state.PendingConnections.Buckets)
	}
	if state.LastConnectionCloseMs != closedAt.UnixMilli() {
		t.Fatalf("LastConnectionCloseMs = %d, want the close high-water mark persisted", state.LastConnectionCloseMs)
	}
	if agent.connectionCollector() != nil {
		t.Fatal("the collector was not stopped")
	}
}

func TestReconcileConnectionCollectorFollowsTheAppliedConfig(t *testing.T) {
	t.Parallel()
	address := startFakeSingBoxDaemon(t, collectorTestSecret, func(_ *daemonpb.SubscribeConnectionsRequest, stream grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error {
		if err := stream.Send(resetBatch()); err != nil {
			return err
		}
		<-stream.Context().Done()
		return stream.Context().Err()
	})
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}

	agent := newCollectorTestAgent(t, "https://server.invalid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A 1.13 config carries no services block, so nothing starts. This is the
	// state of every node in the fleet.
	writeFile(t, agent.Config.SingBoxConfig, `{"inbounds": [], "outbounds": []}`)
	agent.reconcileConnectionCollector(ctx)
	if agent.connectionCollector() != nil {
		t.Fatal("a config without an api service started a collector")
	}

	writeFile(t, agent.Config.SingBoxConfig, fmt.Sprintf(
		`{"services": [{"type": "api", "listen": %q, "listen_port": %s, "secret": %q}]}`,
		host, port, collectorTestSecret))
	agent.reconcileConnectionCollector(ctx)
	collector := agent.connectionCollector()
	if collector == nil {
		t.Fatal("an opted-in config did not start a collector")
	}

	// Reconciling an unchanged config must not churn the subscription.
	agent.reconcileConnectionCollector(ctx)
	if agent.connectionCollector() != collector {
		t.Fatal("an unchanged config restarted the collector")
	}

	// Disabling the node re-renders a config without the block, which stops it.
	writeFile(t, agent.Config.SingBoxConfig, `{"inbounds": []}`)
	agent.reconcileConnectionCollector(ctx)
	if agent.connectionCollector() != nil {
		t.Fatal("removing the api service did not stop the collector")
	}
}

func TestAgentCapabilitiesAdvertiseConnectionTelemetry(t *testing.T) {
	t.Parallel()
	for _, capability := range agentCapabilities() {
		if capability == model.CapabilityConnectionTelemetryV1 {
			return
		}
	}
	t.Fatalf("agentCapabilities() = %v, missing %q", agentCapabilities(), model.CapabilityConnectionTelemetryV1)
}

// --- helpers ----------------------------------------------------------------

func newTestCollector() *ConnectionCollector {
	return newConnectionCollector(ConnectionTelemetryOptions{Address: "127.0.0.1:9091", Secret: collectorTestSecret})
}

func (c *ConnectionCollector) liveCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.live)
}

func (c *ConnectionCollector) tracks(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.live[id]
	return ok
}

// installCollector attaches a collector without starting a goroutine, so report
// and shutdown paths can be driven from hand-fed events.
func (a *Agent) installCollector(collector *ConnectionCollector) {
	done := make(chan struct{})
	close(done)
	a.connectionMu.Lock()
	defer a.connectionMu.Unlock()
	a.connections = &connectionCollectorHandle{
		collector: collector,
		options:   collector.options,
		cancel:    func() {},
		done:      done,
	}
}

func newCollectorTestAgent(t *testing.T, serverURL string) *Agent {
	t.Helper()
	dir := t.TempDir()
	return &Agent{
		Config: Config{
			NodeName:               "azus",
			Token:                  "secret",
			ServerURL:              serverURL,
			StatePath:              filepath.Join(dir, "agent-state.json"),
			SingBoxConfig:          filepath.Join(dir, "sing-box.json"),
			AgentConfigPath:        filepath.Join(dir, "agent.json"),
			AllowInsecureTransport: true,
		},
		Client: &http.Client{Timeout: 10 * time.Second},
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := atomicWrite(path, []byte(content), defaultRuntimeFilePerm); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(message)
}

// liveConnection builds the shape BoxFleet actually sees: no sniff action, so
// Domain is empty and the destination host lives in Destination.
func liveConnection(id, user string, createdAt time.Time, uplinkTotal, downlinkTotal int64) *daemonpb.Connection {
	return &daemonpb.Connection{
		Id:            id,
		Inbound:       "vless-in",
		InboundType:   "vless",
		IpVersion:     4,
		Network:       "tcp",
		Source:        "203.0.113.7:51544",
		Destination:   "example.com:443",
		Protocol:      "tls",
		User:          user,
		CreatedAt:     createdAt.UnixMilli(),
		UplinkTotal:   uplinkTotal,
		DownlinkTotal: downlinkTotal,
		Rule:          "default",
		Outbound:      "direct",
		OutboundType:  "direct",
	}
}

func newEvent(id string, connection *daemonpb.Connection) *daemonpb.ConnectionEvent {
	return &daemonpb.ConnectionEvent{
		Type:       daemonpb.ConnectionEventType_CONNECTION_EVENT_NEW,
		Id:         id,
		Connection: connection,
	}
}

func updateEvent(id string, uplinkDelta, downlinkDelta int64) *daemonpb.ConnectionEvent {
	return &daemonpb.ConnectionEvent{
		Type:          daemonpb.ConnectionEventType_CONNECTION_EVENT_UPDATE,
		Id:            id,
		UplinkDelta:   uplinkDelta,
		DownlinkDelta: downlinkDelta,
	}
}

func closedEvent(id string, closedAt time.Time, connection *daemonpb.Connection) *daemonpb.ConnectionEvent {
	return &daemonpb.ConnectionEvent{
		Type:       daemonpb.ConnectionEventType_CONNECTION_EVENT_CLOSED,
		Id:         id,
		Connection: connection,
		ClosedAt:   closedAt.UnixMilli(),
	}
}

func batch(events ...*daemonpb.ConnectionEvent) *daemonpb.ConnectionEvents {
	return &daemonpb.ConnectionEvents{Events: events}
}

func resetBatch(events ...*daemonpb.ConnectionEvent) *daemonpb.ConnectionEvents {
	return &daemonpb.ConnectionEvents{Events: events, Reset_: true}
}

// startFakeSingBoxDaemon runs an in-process gRPC server that answers
// SubscribeConnections and authenticates exactly as sing-box's daemon does
// (daemon/server.go:39-66). No sing-box binary, no network beyond loopback, so
// the whole collector runs in CI.
func startFakeSingBoxDaemon(t *testing.T, secret string, handle func(*daemonpb.SubscribeConnectionsRequest, grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.ChainStreamInterceptor(func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		if secret == "" {
			return next(srv, stream)
		}
		md, ok := metadata.FromIncomingContext(stream.Context())
		if !ok {
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
		return next(srv, stream)
	}))
	daemonpb.RegisterStartedServiceServer(server, fakeSingBoxDaemon{handle: handle})
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	return listener.Addr().String()
}

type fakeSingBoxDaemon struct {
	daemonpb.UnimplementedStartedServiceServer
	handle func(*daemonpb.SubscribeConnectionsRequest, grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error
}

func (f fakeSingBoxDaemon) SubscribeConnections(request *daemonpb.SubscribeConnectionsRequest, stream grpc.ServerStreamingServer[daemonpb.ConnectionEvents]) error {
	return f.handle(request, stream)
}
