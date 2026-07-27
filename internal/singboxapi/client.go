// Package singboxapi is BoxFleet's client for sing-box 1.14's daemon gRPC
// connection stream, exposed on a node by the `service.api` config block.
//
// The package is deliberately narrow. daemonpb vendors only the
// SubscribeConnections RPC (see daemonpb/singbox_daemon_connections.proto and
// README.md), and this wrapper is the only thing agent code is expected to
// touch: it enforces the two safety properties the endpoint demands — loopback
// binding and a non-empty Bearer secret — and hands back the event stream.
//
// This is telemetry, not accounting. The stream drops events silently under
// three documented loss modes, so the bytes it yields are a best-effort
// estimate. Per-user billing stays on the V2Ray counters
// (internal/v2raystats). See docs/adr/0001-network-event-telemetry-source.md.
package singboxapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/haoxin/boxfleet/internal/singboxapi/daemonpb"
)

// maxEventBatchBytes bounds a single ConnectionEvents message. The first
// message of a subscription carries the whole tracked state — every live
// connection plus up to 1000 entries from the closed-connection ring — so it is
// the one message that scales with node load. 4 MiB is roughly 14000
// connections at the observed encoded size; a node tracking that many is
// already past the point where sing-box's own ~1 KB-per-connection tracker is
// the larger problem. Kept explicit rather than inherited from gRPC's identical
// default so the bound is a decision, and so raising it is a visible one.
const maxEventBatchBytes = 4 << 20

// ErrEmptySecret is returned by Dial when Options.Secret is empty.
//
// This is not defensive tidiness. sing-box's authenticate() opens with
// `if secret == "" { return nil }` (daemon/server.go:50), so a node configured
// with an empty secret serves its entire control plane — StopService,
// ReloadService, CloseAllConnections, SelectOutbound, TriggerDebugCrash — to
// anything that can reach the listener, unauthenticated. A client that tolerates
// an empty secret makes that misconfiguration invisible.
var ErrEmptySecret = errors.New("singboxapi: empty secret would disable sing-box daemon authentication")

// ErrNonLoopbackAddress is returned by Dial for an address outside 127.0.0.0/8,
// ::1 or localhost. The daemon endpoint is a control plane guarded by a single
// shared secret and no transport security, so BoxFleet only ever speaks to one
// on the node's own loopback, where the agent and sing-box both live.
var ErrNonLoopbackAddress = errors.New("singboxapi: daemon address must be loopback")

// Options configures a Client.
type Options struct {
	// Address is the sing-box daemon endpoint as host:port. Must be loopback.
	Address string

	// Secret is the Bearer token from the node's rendered `service.api.secret`.
	// Must not be empty; see ErrEmptySecret.
	Secret string

	// Interval is how often the server recomputes per-connection byte deltas
	// and emits UPDATE events. Zero uses the server's one-second default.
	//
	// Lifecycle events (NEW, CLOSED) are pushed as they happen and are not
	// affected by this; it only paces traffic updates for live connections.
	Interval time.Duration
}

func (o Options) validate() error {
	if o.Secret == "" {
		return ErrEmptySecret
	}
	if o.Address == "" {
		return errors.New("singboxapi: empty address")
	}
	host, _, err := net.SplitHostPort(o.Address)
	if err != nil {
		return fmt.Errorf("singboxapi: parse address %q: %w", o.Address, err)
	}
	if !isLoopbackHost(host) {
		return fmt.Errorf("%w: %q", ErrNonLoopbackAddress, o.Address)
	}
	if o.Interval < 0 {
		return fmt.Errorf("singboxapi: negative interval %s", o.Interval)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback()
}

// Client is a connected sing-box daemon client. It is safe for concurrent use;
// each Subscribe call opens an independent stream.
type Client struct {
	conn     *grpc.ClientConn
	stub     daemonpb.StartedServiceClient
	interval time.Duration
}

// Dial prepares a client for the sing-box daemon at opts.Address.
//
// The connection is established lazily, on the first Subscribe: an agent
// normally starts alongside sing-box and must not fail because the daemon has
// not finished binding yet. Dial therefore reports configuration errors only —
// ErrEmptySecret, ErrNonLoopbackAddress, an unparseable address.
//
// The caller owns Close.
func Dial(opts Options) (*Client, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	// No client keepalive is configured, deliberately. gRPC servers police ping
	// frequency against keepalive.EnforcementPolicy.MinTime, which sing-box
	// leaves at the 5-minute default (daemon/server.go:17 sets no policy), so
	// any useful ping interval earns a GOAWAY after two strikes and kills the
	// stream. Detection of a dead daemon rests on loopback TCP surfacing the
	// peer's exit as a Recv error, which it does immediately.
	conn, err := grpc.NewClient(
		opts.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(bearerCredentials{secret: opts.Secret}),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxEventBatchBytes)),
	)
	if err != nil {
		return nil, fmt.Errorf("singboxapi: dial %s: %w", opts.Address, err)
	}
	return &Client{
		conn:     conn,
		stub:     daemonpb.NewStartedServiceClient(conn),
		interval: opts.Interval,
	}, nil
}

// Close releases the underlying connection. Streams derived from this client
// fail afterwards.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Subscribe opens a connection event stream.
//
// The stream lives until ctx is cancelled, the daemon stops, or Recv reports an
// error; cancelling ctx is the only way to stop it early. The first batch
// always has Reset set and replays the whole tracked state, so a caller
// resubscribing after an error must discard what it accumulated before.
func (c *Client) Subscribe(ctx context.Context) (*Stream, error) {
	stream, err := c.stub.SubscribeConnections(ctx, &daemonpb.SubscribeConnectionsRequest{
		// The server reads this as time.Duration, i.e. nanoseconds
		// (daemon/started_service.go:736). Sending seconds here would ask for a
		// nanosecond tick.
		Interval: int64(c.interval),
	})
	if err != nil {
		return nil, fmt.Errorf("singboxapi: subscribe connections: %w", err)
	}
	return &Stream{stream: stream}, nil
}

// Stream yields batches of connection events from a subscription.
type Stream struct {
	stream grpc.ServerStreamingClient[daemonpb.ConnectionEvents]
}

// Recv blocks for the next batch. It returns io.EOF when the daemon closes the
// stream cleanly, and a gRPC status error otherwise — including
// codes.Canceled once the subscription context is cancelled.
func (s *Stream) Recv() (*daemonpb.ConnectionEvents, error) {
	return s.stream.Recv()
}

// Endpoint reports the destination host and port of a connection.
//
// It exists because the obvious field is the wrong one. Connection.Domain is
// empty on every config BoxFleet renders: buildConnectionProto copies
// metadata.Metadata.Domain with no fallback to Destination.Fqdn
// (daemon/started_service.go:985), and BoxFleet renders no sniff action, so
// nothing populates it. The host arrives in Connection.Destination instead.
// Domain is still preferred when present, so a future renderer that does sniff
// keeps working.
//
// host is a hostname or a bare IP literal, with IPv6 brackets stripped. port is
// 0 when the destination carries none or an unparseable one.
func Endpoint(conn *daemonpb.Connection) (host string, port uint16) {
	if conn == nil {
		return "", 0
	}
	destinationHost, destinationPort := splitEndpoint(conn.GetDestination())
	if domain := conn.GetDomain(); domain != "" {
		return domain, destinationPort
	}
	return destinationHost, destinationPort
}

func splitEndpoint(destination string) (host string, port uint16) {
	if destination == "" {
		return "", 0
	}
	host, portText, err := net.SplitHostPort(destination)
	if err != nil {
		// A destination without a port is not a shape sing-box produces today
		// (M3.Socksaddr.String always appends one), but a bare host is the only
		// sensible reading of it if that changes.
		return destination, 0
	}
	parsed, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return host, 0
	}
	return host, uint16(parsed)
}

// bearerCredentials attaches the daemon secret to every RPC, matching what
// sing-box's authenticate() reads: an "authorization" metadata key holding
// "Bearer " + secret (daemon/server.go:57-64).
type bearerCredentials struct {
	secret string
}

func (c bearerCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + c.secret}, nil
}

// RequireTransportSecurity reports false because the daemon endpoint is
// loopback-only and sing-box's api service is rendered without TLS. gRPC would
// otherwise refuse to send per-RPC credentials over an insecure transport. Dial
// enforces the loopback half of that bargain.
func (bearerCredentials) RequireTransportSecurity() bool { return false }
