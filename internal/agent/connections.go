package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/singboxapi"
	"github.com/haoxin/boxfleet/internal/singboxapi/daemonpb"
)

// The connection collector is the node side of the sing-box 1.14 daemon gRPC
// telemetry path. It is a *second* network-event producer that coexists with the
// journalctl scraper (ReportLogs); it never disables it. The production fleet
// runs sing-box 1.13, where the `service.api` config block does not parse at all,
// so this whole path is opt-in per node and off unless the server rendered the
// block into the config the agent applied.
//
// Everything here is best-effort telemetry, not accounting. Per-user billing
// stays on the V2Ray counters (ReportTraffic). See
// docs/adr/0001-network-event-telemetry-source.md.

const (
	// singBoxAPIServiceType is the `type` of sing-box's api service, the one that
	// exposes the daemon gRPC endpoint on a headless node.
	singBoxAPIServiceType = "api"

	// defaultConnectionUpdateInterval paces UPDATE events for live connections.
	// sing-box defaults to one second, which on a node carrying thousands of
	// connections is thousands of proto messages per second for data that is
	// bucketed at five-minute resolution anyway. Five seconds cuts that by 5x.
	//
	// A longer interval does not lose bytes on its own: a connection's final
	// totals arrive on its CLOSED event, so the tail between the last tick and
	// the close is recovered. It only widens the window in which a connection
	// that dies *without* a close (sing-box restart, ring eviction) loses its
	// unreported tail.
	defaultConnectionUpdateInterval = 5 * time.Second

	// maxTrackedConnections bounds the live-connection identity map. Node memory
	// is a hard constraint, so the collector refuses to track beyond this and
	// counts the refusal in ConnectionCoverage.DroppedBuckets rather than
	// silently dropping. sing-box's own tracker holds roughly 1 KB per live
	// connection, so a node at this cap is already spending ~4 MB upstream; the
	// collector's own share is roughly 400 bytes per entry.
	//
	// A refused connection is not necessarily lost traffic: its CLOSED event
	// carries full identity and final totals, so it is recovered through the
	// orphan path.
	maxTrackedConnections = 4096

	// maxPendingConnectionBuckets bounds the aggregation map. It is deliberately
	// equal to the server's per-report bucket cap so that a drain always ships
	// the complete window — a report that had to be split would either carry
	// coverage counters describing buckets it does not contain, or repeat them.
	maxPendingConnectionBuckets = 2000

	// maxAccountedCloseIDs bounds the memory of connections whose close has been
	// accounted for. It is larger than sing-box's 1000-entry closed-connection
	// ring on purpose: within one agent run, anything the ring can replay is
	// still in this set, so a replay after a reconnect can never double count.
	maxAccountedCloseIDs = 2048

	connectionSubscribeMinBackoff = time.Second
	connectionSubscribeMaxBackoff = 30 * time.Second
)

// errNoConnectionTelemetry reports that the applied sing-box config carries no
// api service, which is the normal state for every node in the fleet.
var errNoConnectionTelemetry = errors.New("no sing-box api service configured")

// ConnectionTelemetryOptions is the collector's whole configuration surface. It
// is discovered from the sing-box config the agent applied rather than from
// agent.json: that file is the authoritative statement of what sing-box is
// actually running, it already arrives over the authenticated config channel,
// and it makes enable/disable a pure renderer decision with no second
// distribution path to keep in sync.
type ConnectionTelemetryOptions struct {
	// Address is the daemon endpoint as host:port. singboxapi.Dial rejects
	// anything that is not loopback.
	Address string
	// Secret is the Bearer token from the rendered `service.api.secret`. An
	// empty secret disables authentication in sing-box itself, so
	// singboxapi.Dial refuses it.
	Secret string
	// Interval paces traffic UPDATE events; see defaultConnectionUpdateInterval.
	Interval time.Duration
}

// singBoxServicesConfig is the subset of the rendered sing-box config the
// collector reads. Unknown fields are ignored, so renderer additions elsewhere
// in the config cannot break discovery.
type singBoxServicesConfig struct {
	Services []struct {
		Type       string `json:"type"`
		Listen     string `json:"listen"`
		ListenPort int    `json:"listen_port"`
		Secret     string `json:"secret"`
	} `json:"services"`
}

// connectionTelemetryOptions extracts the daemon endpoint from a rendered
// sing-box config. It returns errNoConnectionTelemetry when the node is not
// opted in, which is the expected outcome on every 1.13 node.
func connectionTelemetryOptions(raw []byte) (ConnectionTelemetryOptions, error) {
	var parsed singBoxServicesConfig
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ConnectionTelemetryOptions{}, fmt.Errorf("parse sing-box config: %w", err)
	}
	for _, service := range parsed.Services {
		if service.Type != singBoxAPIServiceType {
			continue
		}
		if service.Listen == "" {
			// sing-box's own default listen address is not loopback, and the
			// daemon endpoint is a full control plane. Refuse rather than guess.
			return ConnectionTelemetryOptions{}, errors.New("sing-box api service has no listen address")
		}
		if service.ListenPort <= 0 || service.ListenPort > 65535 {
			return ConnectionTelemetryOptions{}, fmt.Errorf("sing-box api service has an invalid listen_port %d", service.ListenPort)
		}
		if service.Secret == "" {
			return ConnectionTelemetryOptions{}, errors.New("sing-box api service has an empty secret, which disables its authentication")
		}
		return ConnectionTelemetryOptions{
			Address:  net.JoinHostPort(service.Listen, strconv.Itoa(service.ListenPort)),
			Secret:   service.Secret,
			Interval: defaultConnectionUpdateInterval,
		}, nil
	}
	return ConnectionTelemetryOptions{}, errNoConnectionTelemetry
}

// trackedConnection is the collector's per-connection identity and accounting
// high-water mark. Identity is needed because UPDATE events carry only an id and
// a pair of deltas; the totals are needed because every other event carries
// absolute totals, and recording `total - accounted` is what makes a replayed
// connection idempotent.
type trackedConnection struct {
	// template holds the normalised dimensions of the connection's bucket. Only
	// BucketStart, the measures and the window bounds are filled in per event.
	template model.ConnectionBucket
	// attributed records whether the connection carried a `user`. VLESS and
	// multi-user Shadowsocks populate it; single-user Shadowsocks never does.
	attributed  bool
	createdAtMs int64
	// uplink and downlink are the absolute totals already recorded.
	uplink   int64
	downlink int64
	// epoch is the reset generation in which this entry was last seen. A
	// full-state replay that does not mention it proves it is gone — the same
	// trick State.CounterEpoch plays for v2ray counters.
	epoch int64
	// counted marks that the connection has already been counted into the
	// current report window's ConnectionsObserved. Reset on every drain.
	counted bool
}

func (t *trackedConnection) advance(uplinkTotal, downlinkTotal int64) (uplink, downlink int64) {
	if uplinkTotal > t.uplink {
		uplink, t.uplink = uplinkTotal-t.uplink, uplinkTotal
	}
	if downlinkTotal > t.downlink {
		downlink, t.downlink = downlinkTotal-t.downlink, downlinkTotal
	}
	return uplink, downlink
}

// ConnectionCollector subscribes to the sing-box daemon connection stream and
// aggregates it into ConnectionBuckets for the agent to report.
//
// The receive goroutine does no I/O at all: it decodes a batch and folds it into
// in-memory maps under a mutex held for microseconds. That is deliberate.
// sing-box's observable.Subscriber.Emit drops events silently when a listener's
// 64-slot buffer fills (common/trafficcontrol/manager.go:65), so anything slow
// in the receive path — a disk write, an HTTP POST, even a channel handoff to a
// goroutine that does either — destroys data with no error and no counter. There
// is deliberately no internal queue between Recv and aggregation, because a
// queue is just a second place to drop.
type ConnectionCollector struct {
	options ConnectionTelemetryOptions
	now     func() time.Time
	// minBackoff and maxBackoff bound the resubscribe delay. Fields rather than
	// constants so tests can drive a reconnect without waiting a real second.
	minBackoff time.Duration
	maxBackoff time.Duration

	// quiet suppresses repeated stream-error logging. On sing-box 1.13 the
	// endpoint does not exist, so a misconfigured node would otherwise print a
	// connection error every backoff interval forever.
	quiet atomic.Bool

	mu        sync.Mutex
	buckets   map[string]*model.ConnectionBucket
	live      map[string]*trackedConnection
	accounted *closedIDRing
	coverage  model.ConnectionCoverage
	// epoch increments on every full-state replay.
	epoch int64
	// subscribed records that at least one subscription has delivered its
	// initial state, so the *next* replay can be counted as a lost-continuity
	// event rather than as ordinary startup.
	subscribed bool
	// startupCloseHighWaterMs is the closedAt of the last close this node
	// reported before the agent restarted. Closed connections replayed from
	// sing-box's ring at or below it were already reported and must be skipped;
	// the in-memory accounted ring covers everything inside one agent run.
	startupCloseHighWaterMs int64
	closeHighWaterMs        int64
	windowStart             time.Time
}

func newConnectionCollector(options ConnectionTelemetryOptions) *ConnectionCollector {
	collector := &ConnectionCollector{
		options:    options,
		now:        func() time.Time { return time.Now().UTC() },
		minBackoff: connectionSubscribeMinBackoff,
		maxBackoff: connectionSubscribeMaxBackoff,
		buckets:    make(map[string]*model.ConnectionBucket),
		live:       make(map[string]*trackedConnection),
		accounted:  newClosedIDRing(maxAccountedCloseIDs),
	}
	collector.windowStart = collector.now()
	return collector
}

// setStartupCloseHighWater seeds the cross-restart replay guard from durable
// agent state.
func (c *ConnectionCollector) setStartupCloseHighWater(closedAtMs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startupCloseHighWaterMs = closedAtMs
	if closedAtMs > c.closeHighWaterMs {
		c.closeHighWaterMs = closedAtMs
	}
}

// CloseHighWaterMs is the newest closedAt the collector has accounted for. The
// agent persists it so an agent restart cannot re-report closed connections that
// are still sitting in sing-box's replay ring.
func (c *ConnectionCollector) CloseHighWaterMs() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeHighWaterMs
}

// Run consumes the stream until ctx is cancelled. It never returns an error:
// collector failure must never disturb sing-box supervision or the config-apply
// loop, and on a 1.13 node every subscription attempt fails by design.
func (c *ConnectionCollector) Run(ctx context.Context) {
	client, err := singboxapi.Dial(singboxapi.Options{
		Address:  c.options.Address,
		Secret:   c.options.Secret,
		Interval: c.options.Interval,
	})
	if err != nil {
		// A configuration error, not a runtime one: retrying cannot fix a
		// non-loopback address or an empty secret, so this is reported once and
		// the collector stays down until the config changes.
		fmt.Fprintf(os.Stderr, "boxfleet-agent connection telemetry disabled: %v\n", err)
		return
	}
	defer client.Close()

	backoff := c.minBackoff
	for ctx.Err() == nil {
		err := c.consume(ctx, client)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.noteStreamError(err)
			backoff = min(backoff*2, c.maxBackoff)
		} else {
			// A clean end-of-stream means sing-box shut the subscription down —
			// a reload or a stop. Reconnect promptly; the replay recovers what
			// is still in the ring.
			backoff = c.minBackoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func (c *ConnectionCollector) consume(ctx context.Context, client *singboxapi.Client) error {
	streamCtx, cancel := context.WithCancel(ctx)
	// Cancelling on return releases sing-box's server-side subscription; a
	// reconnect loop that leaked one per attempt would starve the tracker's
	// observer.
	defer cancel()
	stream, err := client.Subscribe(streamCtx)
	if err != nil {
		return err
	}
	for {
		batch, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		c.quiet.Store(false)
		c.apply(batch, c.now())
	}
}

// noteStreamError logs the first failure after a working subscription and stays
// silent until the next one succeeds. An opted-in node whose sing-box does not
// speak the API — the 1.13 case — prints exactly one line, not one per backoff.
func (c *ConnectionCollector) noteStreamError(err error) {
	if c.quiet.Swap(true) {
		return
	}
	fmt.Fprintf(os.Stderr, "boxfleet-agent connection stream failed, retrying: %v\n", err)
}

// apply folds one batch into the aggregate. It is the only writer of the
// collector's maps besides Drain.
func (c *ConnectionCollector) apply(batch *daemonpb.ConnectionEvents, now time.Time) {
	if batch == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	replay := batch.GetReset_()
	if replay {
		c.epoch++
		if c.subscribed {
			// Continuity was lost: an unknown amount of traffic went unobserved
			// between the last event and this replay.
			c.coverage.StreamResets++
		}
		c.subscribed = true
	}
	for _, event := range batch.GetEvents() {
		switch event.GetType() {
		case daemonpb.ConnectionEventType_CONNECTION_EVENT_NEW:
			c.applyNew(event, now)
		case daemonpb.ConnectionEventType_CONNECTION_EVENT_UPDATE:
			c.applyUpdate(event, now)
		case daemonpb.ConnectionEventType_CONNECTION_EVENT_CLOSED:
			c.applyClosed(event, now)
		}
	}
	if replay {
		c.sweepStale()
	}
}

// applyNew handles the NEW event, which is also how a subscription replays its
// initial state — including connections that already closed, which arrive as NEW
// with a non-zero Connection.ClosedAt.
func (c *ConnectionCollector) applyNew(event *daemonpb.ConnectionEvent, now time.Time) {
	connection := event.GetConnection()
	if connection == nil {
		return
	}
	id := connectionEventID(event, connection)
	if id == "" {
		return
	}
	closedAtMs := connection.GetClosedAt()

	if tracked, ok := c.live[id]; ok {
		// Already known: this is a replay after a reconnect. Record only what the
		// totals grew by, so the replay cannot double count.
		tracked.epoch = c.epoch
		c.touch(tracked)
		uplink, downlink := tracked.advance(connection.GetUplinkTotal(), connection.GetDownlinkTotal())
		if closedAtMs != 0 {
			c.closeTracked(id, tracked, closedAtMs, uplink, downlink, 0)
			return
		}
		c.record(tracked, now, uplink, downlink, 0, 0, 0)
		return
	}

	if closedAtMs != 0 {
		// A connection replayed out of sing-box's closed ring that we are not
		// tracking. Either it closed while the subscription was down — real
		// recovered traffic — or it was already accounted for and must be
		// skipped.
		if c.accounted.contains(id) || closedAtMs <= c.startupCloseHighWaterMs {
			return
		}
		tracked, ok := c.newTracked(connection)
		if !ok {
			return
		}
		c.touch(tracked)
		c.coverage.ConnectionsOrphaned++
		uplink, downlink := tracked.advance(connection.GetUplinkTotal(), connection.GetDownlinkTotal())
		// Opened and closed both land in the close bucket. Splitting the open
		// across to the createdAt bucket would fragment the aggregate for a
		// zero-byte row.
		c.closeTracked(id, tracked, closedAtMs, uplink, downlink, 1)
		return
	}

	if len(c.live) >= maxTrackedConnections {
		// The identity map is full. Drop the connection entirely rather than
		// record an open we can never complete: its CLOSED event carries full
		// identity and final totals, so the orphan path recovers it in full and
		// counting the open here would double it.
		c.coverage.DroppedBuckets++
		return
	}
	tracked, ok := c.newTracked(connection)
	if !ok {
		return
	}
	c.live[id] = tracked
	c.touch(tracked)
	uplink, downlink := tracked.advance(connection.GetUplinkTotal(), connection.GetDownlinkTotal())
	c.record(tracked, now, uplink, downlink, 1, 0, 0)
}

// applyUpdate handles a traffic tick. UPDATE carries an id and two deltas and
// nothing else, so identity has to come from the NEW that preceded it.
func (c *ConnectionCollector) applyUpdate(event *daemonpb.ConnectionEvent, now time.Time) {
	tracked, ok := c.live[event.GetId()]
	if !ok {
		// No identity to attribute these bytes to. They are not lost for good:
		// the connection's CLOSED event carries absolute totals, and the orphan
		// path records them in full.
		return
	}
	tracked.epoch = c.epoch
	c.touch(tracked)
	uplink, downlink := max(event.GetUplinkDelta(), 0), max(event.GetDownlinkDelta(), 0)
	tracked.uplink += uplink
	tracked.downlink += downlink
	c.record(tracked, now, uplink, downlink, 0, 0, 0)
}

func (c *ConnectionCollector) applyClosed(event *daemonpb.ConnectionEvent, now time.Time) {
	id := event.GetId()
	if id == "" {
		return
	}
	connection := event.GetConnection()
	closedAtMs := event.GetClosedAt()
	if closedAtMs == 0 {
		closedAtMs = connection.GetClosedAt()
	}
	if closedAtMs == 0 {
		closedAtMs = now.UnixMilli()
	}
	if tracked, ok := c.live[id]; ok {
		tracked.epoch = c.epoch
		c.touch(tracked)
		var uplink, downlink int64
		if connection != nil {
			// The final totals close the gap between the last traffic tick and
			// the connection's death, which is the single largest recoverable
			// slice of the under-count.
			uplink, downlink = tracked.advance(connection.GetUplinkTotal(), connection.GetDownlinkTotal())
		}
		c.closeTracked(id, tracked, closedAtMs, uplink, downlink, 0)
		return
	}
	if c.accounted.contains(id) {
		// A close already accounted for is not a new gap.
		return
	}
	// A close for a connection whose NEW never arrived: the subscription started
	// mid-flight, the NEW was dropped by a full listener buffer, or the identity
	// map was full when it opened.
	c.coverage.ConnectionsOrphaned++
	if connection == nil {
		return
	}
	tracked, ok := c.newTracked(connection)
	if !ok {
		return
	}
	c.touch(tracked)
	uplink, downlink := tracked.advance(connection.GetUplinkTotal(), connection.GetDownlinkTotal())
	c.closeTracked(id, tracked, closedAtMs, uplink, downlink, 1)
}

// closeTracked records a connection's final contribution in the bucket that
// holds its close, retires it from the live map, and remembers its id so a
// replay out of sing-box's ring is recognised rather than counted again.
func (c *ConnectionCollector) closeTracked(id string, tracked *trackedConnection, closedAtMs, uplink, downlink, opened int64) {
	closedAt := time.UnixMilli(closedAtMs).UTC()
	var durationMs int64
	if tracked.createdAtMs > 0 && closedAtMs > tracked.createdAtMs {
		durationMs = closedAtMs - tracked.createdAtMs
	}
	c.record(tracked, closedAt, uplink, downlink, opened, 1, durationMs)
	delete(c.live, id)
	c.accounted.add(id)
	if closedAtMs > c.closeHighWaterMs {
		c.closeHighWaterMs = closedAtMs
	}
}

// sweepStale drops entries a full-state replay did not mention. They are gone
// from sing-box's view: either they closed while the subscription was down and
// were evicted from the 1000-entry ring, or sing-box restarted and minted new
// ids. Their unreported tail is unrecoverable, and no close is recorded because
// none was observed — the StreamResets counter is what tells an operator that
// this window has an unknown gap in it.
func (c *ConnectionCollector) sweepStale() {
	for id, tracked := range c.live {
		if tracked.epoch != c.epoch {
			delete(c.live, id)
		}
	}
}

// touch counts a connection into the current window exactly once, however many
// events it produces.
func (c *ConnectionCollector) touch(tracked *trackedConnection) {
	if tracked.counted {
		return
	}
	tracked.counted = true
	c.coverage.ConnectionsObserved++
	if tracked.attributed {
		c.coverage.ConnectionsAttributed++
	} else {
		c.coverage.ConnectionsUnattributed++
	}
}

// record folds one measurement into the aggregation map at the bucket holding
// `at`.
func (c *ConnectionCollector) record(tracked *trackedConnection, at time.Time, uplink, downlink, opened, closed, durationMs int64) {
	if uplink == 0 && downlink == 0 && opened == 0 && closed == 0 {
		return
	}
	at = at.UTC()
	instant := at.Format(model.ConnectionInstantLayout)
	bucket := tracked.template
	bucket.BucketStart = at.Truncate(model.ConnectionBucketInterval).Format(model.ConnectionInstantLayout)
	bucket.WindowStart, bucket.WindowEnd = instant, instant
	bucket.UplinkBytes, bucket.DownlinkBytes = uplink, downlink
	bucket.ConnectionsOpened, bucket.ConnectionsClosed = opened, closed
	bucket.DurationMsTotal = durationMs

	key := bucket.DimensionKey()
	existing, ok := c.buckets[key]
	if !ok {
		if len(c.buckets) >= maxPendingConnectionBuckets {
			// The node produces more distinct dimension tuples per report window
			// than the collector is sized for. Dropping is visible by design:
			// the alternative is an unbounded map on a memory-constrained node.
			c.coverage.DroppedBuckets++
			return
		}
		copied := bucket
		c.buckets[key] = &copied
	} else {
		existing.Merge(bucket)
	}
	total := uplink + downlink
	c.coverage.BytesObserved += total
	if tracked.attributed {
		c.coverage.BytesAttributed += total
	}
}

// newTracked builds a connection's normalised bucket dimensions once, so the
// per-event path is a map lookup and a few additions. It reports false for a
// connection whose dimensions cannot be normalised (no usable destination host),
// which no real sing-box connection produces.
func (c *ConnectionCollector) newTracked(connection *daemonpb.Connection) (*trackedConnection, bool) {
	host, port := singboxapi.Endpoint(connection)
	template := model.ConnectionBucket{
		// Any valid instant will do: Normalize only needs to parse it, and
		// record overwrites it per event.
		BucketStart:  time.UnixMilli(0).UTC().Format(model.ConnectionInstantLayout),
		AuthName:     connection.GetUser(),
		TargetHost:   host,
		TargetPort:   int64(port),
		Domain:       connection.GetDomain(),
		Network:      connection.GetNetwork(),
		IPVersion:    int64(connection.GetIpVersion()),
		Protocol:     connection.GetProtocol(),
		Inbound:      connection.GetInbound(),
		InboundType:  connection.GetInboundType(),
		Rule:         connection.GetRule(),
		Outbound:     connection.GetOutbound(),
		OutboundType: connection.GetOutboundType(),
		Chain:        connection.GetChainList(),
	}
	// The source port is ephemeral and pure key cardinality, so only the host is
	// kept.
	if sourceIP, _, ok := model.SplitConnectionAddress(connection.GetSource()); ok {
		template.SourceIP = sourceIP
	}
	normalized, ok := template.Normalize()
	if !ok {
		c.coverage.DroppedBuckets++
		return nil, false
	}
	return &trackedConnection{
		template:    normalized,
		attributed:  normalized.AuthName != "",
		createdAtMs: connection.GetCreatedAt(),
		epoch:       c.epoch,
	}, true
}

// Drain removes and returns everything aggregated since the previous drain. ok
// is false when there is nothing to report, which is the steady state on a node
// whose sing-box does not serve the endpoint.
func (c *ConnectionCollector) Drain(now time.Time) (buckets []model.ConnectionBucket, coverage model.ConnectionCoverage, windowStart time.Time, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	windowStart, c.windowStart = c.windowStart, now.UTC()
	if len(c.buckets) == 0 && c.coverage == (model.ConnectionCoverage{}) {
		return nil, model.ConnectionCoverage{}, windowStart, false
	}
	// Sorted by the map key — which is the dimension key already — so a report is
	// byte-for-byte reproducible from the same events, without re-deriving a
	// sort key per comparison.
	keys := make([]string, 0, len(c.buckets))
	for key := range c.buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	buckets = make([]model.ConnectionBucket, 0, len(keys))
	for _, key := range keys {
		buckets = append(buckets, *c.buckets[key])
	}
	coverage, c.coverage = c.coverage, model.ConnectionCoverage{}
	c.buckets = make(map[string]*model.ConnectionBucket)
	for _, tracked := range c.live {
		tracked.counted = false
	}
	return buckets, coverage, windowStart, true
}

// closedIDRing is a bounded set with FIFO eviction: the ids of connections whose
// close has already been accounted for. Bounded because node memory is a hard
// constraint, FIFO because sing-box's own replay ring is FIFO, so the oldest id
// is also the first that can no longer be replayed.
type closedIDRing struct {
	ids   map[string]struct{}
	order []string
	next  int
}

func newClosedIDRing(limit int) *closedIDRing {
	return &closedIDRing{ids: make(map[string]struct{}, limit), order: make([]string, limit)}
}

func (r *closedIDRing) contains(id string) bool {
	_, ok := r.ids[id]
	return ok
}

func (r *closedIDRing) add(id string) {
	if _, ok := r.ids[id]; ok {
		return
	}
	if evicted := r.order[r.next]; evicted != "" {
		delete(r.ids, evicted)
	}
	r.order[r.next] = id
	r.next = (r.next + 1) % len(r.order)
	r.ids[id] = struct{}{}
}

// connectionEventID prefers the event's id; sing-box sets both, and the
// connection's own id is the fallback for a malformed event.
func connectionEventID(event *daemonpb.ConnectionEvent, connection *daemonpb.Connection) string {
	if id := event.GetId(); id != "" {
		return id
	}
	return connection.GetId()
}

// --- agent integration ------------------------------------------------------

// connectionCollectorHandle owns a running collector's goroutine.
type connectionCollectorHandle struct {
	collector *ConnectionCollector
	options   ConnectionTelemetryOptions
	cancel    context.CancelFunc
	done      chan struct{}
}

func (h *connectionCollectorHandle) stop() {
	h.cancel()
	<-h.done
}

// reconcileConnectionCollector starts, stops or restarts the collector to match
// the sing-box config currently on disk. That file is the authority: the server
// enables the path by rendering an api service into it, and disables it by
// rendering none.
//
// Every failure here is logged and swallowed. Telemetry must never interfere
// with the config-apply loop or sing-box supervision.
func (a *Agent) reconcileConnectionCollector(ctx context.Context) {
	options, err := a.discoverConnectionTelemetry()
	if err != nil && !errors.Is(err, errNoConnectionTelemetry) {
		fmt.Fprintf(os.Stderr, "boxfleet-agent connection telemetry not started: %v\n", err)
	}
	enabled := err == nil

	a.connectionMu.Lock()
	defer a.connectionMu.Unlock()
	if a.connections != nil {
		if enabled && a.connections.options == options {
			return
		}
		a.connections.stop()
		a.connections = nil
	}
	if !enabled {
		return
	}
	collector := newConnectionCollector(options)
	if state, err := a.LoadState(); err == nil {
		collector.setStartupCloseHighWater(state.LastConnectionCloseMs)
	} else {
		fmt.Fprintf(os.Stderr, "boxfleet-agent connection telemetry load state failed: %v\n", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	handle := &connectionCollectorHandle{
		collector: collector,
		options:   options,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	go func() {
		defer close(handle.done)
		collector.Run(runCtx)
	}()
	a.connections = handle
}

func (a *Agent) discoverConnectionTelemetry() (ConnectionTelemetryOptions, error) {
	raw, err := os.ReadFile(a.Config.SingBoxConfig)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ConnectionTelemetryOptions{}, errNoConnectionTelemetry
		}
		return ConnectionTelemetryOptions{}, err
	}
	return connectionTelemetryOptions(raw)
}

func (a *Agent) connectionCollector() *ConnectionCollector {
	a.connectionMu.Lock()
	defer a.connectionMu.Unlock()
	if a.connections == nil {
		return nil
	}
	return a.connections.collector
}

// stopConnectionCollector stops the collector and hands back what it aggregated,
// so a shutdown can stage the final window instead of discarding it.
func (a *Agent) stopConnectionCollector() *ConnectionCollector {
	a.connectionMu.Lock()
	handle := a.connections
	a.connections = nil
	a.connectionMu.Unlock()
	if handle == nil {
		return nil
	}
	handle.stop()
	return handle.collector
}

// ReportConnections ships one aggregation window, staging it durably before the
// POST exactly as ReportTraffic does with PendingTraffic.
//
// A staged report that has not been accepted blocks the next drain rather than
// being overwritten: the server's idempotency key is (boot id, sequence) and it
// skips a whole batch on replay, so mutating a report the server may already
// have received would silently drop the merged-in window. While a report is
// stuck the collector keeps aggregating in memory, bounded, and reports what it
// had to drop.
func (a *Agent) ReportConnections(ctx context.Context) error {
	state, err := a.LoadState()
	if err != nil {
		return err
	}
	if state.PendingConnections != nil {
		if err := a.postJSON(ctx, "/api/node/connections", state.PendingConnections); err != nil {
			return err
		}
		state.PendingConnections = nil
		if err := a.SaveState(state); err != nil {
			return err
		}
	}
	collector := a.connectionCollector()
	if collector == nil {
		return nil
	}
	now := time.Now().UTC()
	report, ok := a.buildConnectionReport(collector, &state, now)
	if !ok {
		return a.SaveState(state)
	}
	state.PendingConnections = &report
	if err := a.SaveState(state); err != nil {
		return err
	}
	if err := a.postJSON(ctx, "/api/node/connections", report); err != nil {
		return err
	}
	state.PendingConnections = nil
	return a.SaveState(state)
}

// buildConnectionReport drains the collector into a report and advances the
// durable replay guard. It mutates state without saving it; the caller decides
// when that is safe.
func (a *Agent) buildConnectionReport(collector *ConnectionCollector, state *State, now time.Time) (model.ConnectionReport, bool) {
	buckets, coverage, windowStart, ok := collector.Drain(now)
	if high := collector.CloseHighWaterMs(); high > state.LastConnectionCloseMs {
		state.LastConnectionCloseMs = high
	}
	if !ok {
		return model.ConnectionReport{}, false
	}
	state.ConnectionSequence++
	return model.ConnectionReport{
		Sequence:    state.ConnectionSequence,
		AgentBootID: state.BootID,
		WindowStart: windowStart.UTC().Format(model.ConnectionInstantLayout),
		WindowEnd:   now.UTC().Format(model.ConnectionInstantLayout),
		ReportedAt:  now.UTC().Format(time.RFC3339Nano),
		Coverage:    coverage,
		Buckets:     buckets,
	}, true
}

// flushConnectionsToState stops the collector on shutdown and stages its final
// window durably. Unlike v2ray counters, connection deltas exist nowhere but in
// this process, so a shutdown between two poll cycles would otherwise cost the
// whole interval.
func (a *Agent) flushConnectionsToState() {
	collector := a.stopConnectionCollector()
	if collector == nil {
		return
	}
	// Run cancels the poll context before calling this, but a cycle already in
	// flight is still writing agent state. maintenanceMu is what serialises a
	// poll cycle against everything else, so the final flush takes it too rather
	// than racing a report into a lost update.
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	state, err := a.LoadState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "boxfleet-agent connection flush load state failed: %v\n", err)
		return
	}
	report, ok := a.buildConnectionReport(collector, &state, time.Now().UTC())
	switch {
	case !ok:
	case state.PendingConnections != nil:
		// An earlier report is staged and unsent. Overwriting it would drop an
		// interval the server never acknowledged, which is worse than dropping
		// this one, so the older report keeps the slot.
		fmt.Fprintln(os.Stderr, "boxfleet-agent dropped a final connection window: an earlier report is still unsent")
	default:
		state.PendingConnections = &report
	}
	if err := a.SaveState(state); err != nil {
		fmt.Fprintf(os.Stderr, "boxfleet-agent connection flush save state failed: %v\n", err)
	}
}
