package model

import (
	"strconv"
	"strings"
	"time"
)

// Connection telemetry is the second network-event producer: sing-box 1.14's
// daemon gRPC SubscribeConnections stream, collected by the agent and shipped
// here. It does not replace the journalctl scraper — the production fleet runs
// 1.13, where the `service.api` config block does not parse — so both producers
// coexist and this one is opt-in per node.
//
// This file is the whole wire contract, imported by both internal/agent (the
// collector) and internal/server/db (ingest). Aggregation happens on the node:
// see the volume note on the connection_events table in
// migrations/026_connection_telemetry.sql for why raw per-connection rows are
// not shipped.

const (
	// ConnectionBucketInterval is the aggregation grain, applied identically on
	// both sides: the agent keys its in-memory map by the truncated bucket and
	// the server re-truncates whatever a node sends. Five minutes rather than
	// one is a roughly 4x row reduction, and per-minute resolution is not
	// needed here — the per-minute connection-count chart stays on log_events.
	ConnectionBucketInterval = 5 * time.Minute

	// ConnectionChainSeparator flattens Connection.chainList for storage and
	// display. The chain is never joined against, so it stays one column.
	ConnectionChainSeparator = ">"

	// ConnectionDimensionSeparator delimits the fields of a bucket's dimension
	// tuple. NUL cannot appear in any dimension, so the joined form is
	// unambiguous and safe to hash.
	ConnectionDimensionSeparator = "\x00"

	// ConnectionInstantLayout is fixed-width on purpose. window_start and
	// window_end are folded with SQL MIN()/MAX() over TEXT, and RFC3339Nano
	// trims trailing zeros, so "…:00Z" would sort after "…:00.5Z" ('Z' > '.')
	// and a merge would pick the wrong extreme. Millisecond precision matches
	// what every other table's created_at strftime produces.
	ConnectionInstantLayout = "2006-01-02T15:04:05.000Z"
)

// CapabilityConnectionTelemetryV1 is advertised on the heartbeat by agents that
// can collect the 1.14 stream. The server must not render a `service.api` block
// for a node that has not advertised it, and must not expect reports from one.
const CapabilityConnectionTelemetryV1 = "telemetry.connections.v1"

// ConnectionReport ships one collection window of aggregated connection
// telemetry. NodeName is decorative and server-overwritten from the bearer
// token, as on every other *Report. (AgentBootID, Sequence) is the idempotency
// key: the server's unique constraint collides on a retried POST and the whole
// batch is skipped, mirroring TrafficReport exactly — bytes here are summed on
// ingest, so a partially applied replay would silently inflate totals.
type ConnectionReport struct {
	NodeName    string             `json:"node_name"`
	Sequence    int64              `json:"sequence"`
	AgentBootID string             `json:"agent_boot_id"`
	WindowStart string             `json:"window_start"`
	WindowEnd   string             `json:"window_end"`
	ReportedAt  string             `json:"reported_at"`
	Coverage    ConnectionCoverage `json:"coverage"`
	Buckets     []ConnectionBucket `json:"buckets"`
}

// ConnectionCoverage carries what the collector could NOT account for, so the
// server can publish an attribution figure next to every byte total instead of
// implying a precision this source does not have. Three loss modes are
// structural in sing-box: observable.Subscriber.Emit drops silently when a
// listener's 64-slot buffer fills, the closed-connection ring evicts at 1000,
// and connection ids plus in-flight totals reset on restart. A fourth is ours:
// the agent's aggregation map is bounded, because node memory is a hard
// constraint, and reports what it had to discard.
type ConnectionCoverage struct {
	// ConnectionsObserved counts connections the collector saw close (or
	// flushed while still open) during the window — the denominator.
	ConnectionsObserved int64 `json:"connections_observed"`
	// ConnectionsAttributed carries a non-empty `user`. VLESS and multi-user
	// Shadowsocks populate it; single-user Shadowsocks never does.
	ConnectionsAttributed int64 `json:"connections_attributed"`
	// ConnectionsUnattributed arrived with an empty `user`. Their bytes are
	// still recorded, against a NULL proxy_user_id.
	ConnectionsUnattributed int64 `json:"connections_unattributed"`
	// ConnectionsOrphaned saw a CLOSED without a preceding NEW: either the
	// stream began mid-flight or the NEW was dropped. Their totals are counted
	// in full because no earlier delta was ever emitted for them, which is the
	// right call for an under-count.
	ConnectionsOrphaned int64 `json:"connections_orphaned"`
	// StreamResets counts resubscribes in this window, whether from a server
	// restart or the stream's own reset flag. Every reset is an unknown amount
	// of missed traffic.
	StreamResets int64 `json:"stream_resets"`
	// DroppedBuckets counts aggregation buckets the agent discarded on hitting
	// its own map cap. Non-zero means the node is busier than the collector is
	// sized for.
	DroppedBuckets int64 `json:"dropped_buckets"`
	// BytesObserved and BytesAttributed are uplink+downlink over the same
	// populations as the connection counters, so coverage can be expressed by
	// volume as well as by count — a handful of unattributed bulk transfers
	// matters more than many unattributed idle connections.
	BytesObserved   int64 `json:"bytes_observed"`
	BytesAttributed int64 `json:"bytes_attributed"`
}

// ConnectionAttributionRatio is the fraction of observed bytes that carried a
// user, in [0,1]. An empty window reports 1: nothing was observed, so nothing
// was lost, and rendering 0% coverage for an idle node would be misleading.
func (c ConnectionCoverage) ConnectionAttributionRatio() float64 {
	if c.BytesObserved <= 0 {
		return 1
	}
	if c.BytesAttributed >= c.BytesObserved {
		return 1
	}
	if c.BytesAttributed <= 0 {
		return 0
	}
	return float64(c.BytesAttributed) / float64(c.BytesObserved)
}

// ConnectionBucket is one (bucket_start, dimensions) aggregate. Every string
// field maps to a field of sing-box's Connection message; see DimensionKey for
// which of them participate in aggregation.
type ConnectionBucket struct {
	// BucketStart is an RFC3339 UTC instant truncated to
	// ConnectionBucketInterval. The server re-truncates it — a node is not
	// trusted to place its own rows on the time axis.
	BucketStart string `json:"bucket_start"`
	// AuthName is Connection.user: the credential name, not the BoxFleet user.
	// Empty for single-user Shadowsocks, which cannot attribute.
	AuthName string `json:"auth_name"`
	// SourceIP is the host part of Connection.source; the source port is
	// deliberately dropped, being ephemeral and pure key cardinality.
	SourceIP string `json:"source_ip"`
	// TargetHost is Domain when non-empty, else the host part of
	// Connection.destination, lowercased. BoxFleet renders no sniff action and
	// buildConnectionProto has no Destination.Fqdn fallback, so in practice
	// Domain is empty and this is the destination host.
	TargetHost string `json:"target_host"`
	TargetPort int64  `json:"target_port"`
	// Domain is Connection.domain verbatim (lowercased). Kept even though it is
	// expected to be empty: it makes TargetHost's provenance derivable, and it
	// populates for free if sniffing is ever turned on.
	Domain       string   `json:"domain,omitempty"`
	Network      string   `json:"network,omitempty"`
	IPVersion    int64    `json:"ip_version,omitempty"`
	Protocol     string   `json:"protocol,omitempty"`
	Inbound      string   `json:"inbound,omitempty"`
	InboundType  string   `json:"inbound_type,omitempty"`
	Rule         string   `json:"rule,omitempty"`
	Outbound     string   `json:"outbound,omitempty"`
	OutboundType string   `json:"outbound_type,omitempty"`
	Chain        []string `json:"chain,omitempty"`
	// ConnectionsOpened counts NEW events in this bucket; ConnectionsClosed
	// counts CLOSED. They are separate because a long-lived connection
	// contributes bytes to several consecutive buckets — summing "connections"
	// over a range must use ConnectionsOpened or one session is counted many
	// times.
	ConnectionsOpened int64 `json:"connections_opened"`
	ConnectionsClosed int64 `json:"connections_closed"`
	// UplinkBytes and DownlinkBytes are summed deltas of Connection.uplinkTotal
	// and .downlinkTotal (proto fields 16/17). Fields 14/15 are never populated
	// server-side by sing-box; nothing is built on them.
	UplinkBytes   int64 `json:"uplink_bytes"`
	DownlinkBytes int64 `json:"downlink_bytes"`
	// DurationMsTotal sums the lifetime of connections that closed in this
	// bucket, giving a mean session duration when divided by ConnectionsClosed.
	// No existing source can produce this at all: the scraper never observes a
	// close.
	DurationMsTotal int64 `json:"duration_ms_total"`
	// WindowStart and WindowEnd are the observed extremes inside the bucket,
	// kept because a bucket may be filled by several report windows.
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
}

// NormalizeConnectionHost lowercases and trims a host and strips IPv6 brackets
// and any trailing root dot, so "[2001:DB8::1]" and "EXAMPLE.com." collapse
// onto one key. Hosts are normalised once, on write; log_events skipped this
// and forced lower() into every read of target_host.
func NormalizeConnectionHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.Trim(host, "[]")
	if len(host) > 1 && strings.HasSuffix(host, ".") {
		host = strings.TrimSuffix(host, ".")
	}
	return host
}

// SplitConnectionAddress splits a sing-box Socksaddr string ("host:port",
// "[v6]:port") into a normalised host and port. ok is false when the value does
// not carry a usable port, which is how a caller decides to drop the bucket
// rather than record a zero port.
func SplitConnectionAddress(value string) (string, int64, bool) {
	value = strings.TrimSpace(value)
	idx := strings.LastIndex(value, ":")
	if idx <= 0 || idx == len(value)-1 {
		return "", 0, false
	}
	port, err := strconv.ParseInt(value[idx+1:], 10, 64)
	if err != nil || port < 0 || port > 65535 {
		return "", 0, false
	}
	host := NormalizeConnectionHost(value[:idx])
	if host == "" {
		return "", 0, false
	}
	return host, port, true
}

// ConnectionChainString flattens Connection.chainList for storage.
func ConnectionChainString(chain []string) string {
	if len(chain) == 0 {
		return ""
	}
	parts := make([]string, 0, len(chain))
	for _, hop := range chain {
		hop = strings.TrimSpace(hop)
		if hop == "" {
			continue
		}
		parts = append(parts, hop)
	}
	return strings.Join(parts, ConnectionChainSeparator)
}

// TruncateConnectionBucket places an RFC3339 instant on the bucket grid. An
// unparseable value returns "", which callers treat as a rejected bucket.
func TruncateConnectionBucket(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return parsed.UTC().Truncate(ConnectionBucketInterval).Format(ConnectionInstantLayout)
}

// NormalizeConnectionInstant re-renders an RFC3339 instant at
// ConnectionInstantLayout so stored timestamps compare lexicographically.
// Unparseable input returns "".
func NormalizeConnectionInstant(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(ConnectionInstantLayout)
}

// Normalize canonicalises a bucket in place-safe fashion and reports whether it
// is usable. Both sides call it: the agent so its map keys collapse correctly,
// the server because a node's normalisation is never trusted. Running it twice
// must be a no-op, which is what TestConnectionBucketNormalizeIsIdempotent
// pins.
func (b ConnectionBucket) Normalize() (ConnectionBucket, bool) {
	b.BucketStart = TruncateConnectionBucket(b.BucketStart)
	b.AuthName = strings.TrimSpace(b.AuthName)
	b.SourceIP = NormalizeConnectionHost(b.SourceIP)
	b.Domain = NormalizeConnectionHost(b.Domain)
	b.TargetHost = NormalizeConnectionHost(b.TargetHost)
	// Domain wins when present: it is the sniffed name, and the destination is
	// then an IP literal that would fragment the host axis. In BoxFleet's
	// rendered config Domain is empty, so this branch is dormant until a sniff
	// action is ever added.
	if b.Domain != "" {
		b.TargetHost = b.Domain
	}
	b.Network = strings.ToLower(strings.TrimSpace(b.Network))
	b.Protocol = strings.ToLower(strings.TrimSpace(b.Protocol))
	b.Inbound = strings.TrimSpace(b.Inbound)
	b.InboundType = strings.TrimSpace(b.InboundType)
	b.Rule = strings.TrimSpace(b.Rule)
	b.Outbound = strings.TrimSpace(b.Outbound)
	b.OutboundType = strings.TrimSpace(b.OutboundType)
	if b.IPVersion != 4 && b.IPVersion != 6 {
		b.IPVersion = 0
	}
	if flattened := ConnectionChainString(b.Chain); flattened == "" {
		b.Chain = nil
	} else {
		b.Chain = strings.Split(flattened, ConnectionChainSeparator)
	}
	b.WindowStart = NormalizeConnectionInstant(b.WindowStart)
	b.WindowEnd = NormalizeConnectionInstant(b.WindowEnd)
	if b.WindowStart == "" {
		b.WindowStart = b.BucketStart
	}
	if b.WindowEnd == "" || b.WindowEnd < b.WindowStart {
		b.WindowEnd = b.WindowStart
	}
	if b.BucketStart == "" || b.TargetHost == "" {
		return b, false
	}
	if b.TargetPort < 0 || b.TargetPort > 65535 {
		return b, false
	}
	if b.ConnectionsOpened < 0 || b.ConnectionsClosed < 0 {
		return b, false
	}
	if b.UplinkBytes < 0 || b.DownlinkBytes < 0 || b.DurationMsTotal < 0 {
		return b, false
	}
	return b, true
}

// DimensionKey is the aggregation identity of a bucket: everything except the
// measures. The agent uses it directly as its map key; the server hashes it
// together with the node id to derive connection_events.aggregate_key, so that
// two nodes reporting identical dimensions can never collide onto one row.
//
// Call Normalize first — DimensionKey does not re-normalise, so that the agent
// pays for it once per connection rather than once per event.
func (b ConnectionBucket) DimensionKey() string {
	parts := []string{
		b.BucketStart,
		b.AuthName,
		b.SourceIP,
		b.TargetHost,
		strconv.FormatInt(b.TargetPort, 10),
		b.Domain,
		b.Network,
		strconv.FormatInt(b.IPVersion, 10),
		b.Protocol,
		b.Inbound,
		b.InboundType,
		b.Rule,
		b.Outbound,
		b.OutboundType,
		ConnectionChainString(b.Chain),
	}
	return strings.Join(parts, ConnectionDimensionSeparator)
}

// Merge folds another bucket's measures into this one. Dimensions are assumed
// equal — the caller reached this bucket through its DimensionKey.
func (b *ConnectionBucket) Merge(other ConnectionBucket) {
	b.ConnectionsOpened += other.ConnectionsOpened
	b.ConnectionsClosed += other.ConnectionsClosed
	b.UplinkBytes += other.UplinkBytes
	b.DownlinkBytes += other.DownlinkBytes
	b.DurationMsTotal += other.DurationMsTotal
	if other.WindowStart != "" && (b.WindowStart == "" || other.WindowStart < b.WindowStart) {
		b.WindowStart = other.WindowStart
	}
	if other.WindowEnd > b.WindowEnd {
		b.WindowEnd = other.WindowEnd
	}
}
