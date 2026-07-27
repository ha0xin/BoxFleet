package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/haoxin/boxfleet/internal/id"
	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/secret"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type ConnectionReport = model.ConnectionReport
type ConnectionBucket = model.ConnectionBucket
type ConnectionCoverage = model.ConnectionCoverage

const (
	// SettingConnectionEventRetentionDays is deliberately shorter by default
	// than network_event_retention_days: connection_events carries a wider row
	// at a finer dimension tuple, so it accumulates faster than log_events for
	// the same traffic. The bound matches the network-event key so an operator
	// cannot express one retention the schema accepts and the other rejects.
	SettingConnectionEventRetentionDays = "connection_event_retention_days"
	DefaultConnectionEventRetentionDays = int64(14)
	MaxConnectionEventRetentionDays     = MaxNetworkEventRetentionDays
)

const (
	// DefaultConnectionTelemetryListenAddress and
	// DefaultConnectionTelemetryListenPort mirror the column defaults in
	// migrations/026_connection_telemetry.sql. The address is loopback and the
	// facade refuses to store anything else: sing-box's `service.api` endpoint
	// is a full control plane (StopService, ReloadService,
	// CloseAllConnections, TriggerDebugCrash) sharing one listener with the
	// telemetry stream.
	DefaultConnectionTelemetryListenAddress = "127.0.0.1"
	DefaultConnectionTelemetryListenPort    = int64(9091)

	// MinConnectionTelemetrySecretLength is the schema's CHECK restated in Go.
	// sing-box's daemon authenticate() returns nil for an empty secret, so a
	// missing secret does not fail closed upstream — it disables auth. Every
	// layer that can write or emit one enforces a floor.
	MinConnectionTelemetrySecretLength = 32

	// connectionTelemetrySecretBytes yields a 64-character hex secret, well
	// clear of the floor and generated the same way as Reality material.
	connectionTelemetrySecretBytes = 32
)

const (
	// maxConnectionBucketsPerReport bounds one ingest batch. The agent
	// aggregates before shipping, so a conforming node sends tens to low
	// hundreds of buckets per window; 2000 is the ceiling the request body
	// limit in the API layer is sized against.
	maxConnectionBucketsPerReport = 2000

	// Per-bucket measure ceilings. A bucket covers ConnectionBucketInterval
	// (five minutes) on one node, so these are plausibility bounds rather than
	// arbitrary numbers: a 100 Gbit/s link moves roughly 3.75 TB in five
	// minutes, and 10M connections in five minutes is orders of magnitude
	// above anything a node this size sees. Clamping matters because
	// UpsertConnectionEvent *adds* into a running total that is never
	// recomputed — one hostile int64 would poison a row permanently.
	maxConnectionBucketBytes       = int64(4) << 40
	maxConnectionBucketConnections = int64(10_000_000)
	maxConnectionBucketDurationMs  = int64(30*24*3600) * 1000

	// String ceilings. Every one of these arrives from a node, which is a
	// lower-trust domain; the request body limit bounds the batch but not any
	// single field.
	maxConnectionAuthNameLen = 128
	maxConnectionHostLen     = 253
	maxConnectionLabelLen    = 64
	maxConnectionChainLen    = 512
	maxConnectionBootIDLen   = 128
)

// NodeConnectionTelemetry is one node's opt-in to the sing-box 1.14 daemon
// gRPC stream. A missing row is the disabled state, which is why the fleet
// default is off structurally: the production fleet runs 1.13, where the
// `service.api` config block does not parse at all.
type NodeConnectionTelemetry struct {
	NodeName      string
	Enabled       bool
	ListenAddress string
	ListenPort    int64
	// Secret is stored and returned in the clear. It has to be: the renderer
	// emits it into the node config, so unlike a node token — which the server
	// only ever verifies, and therefore stores as a bcrypt hash — this value
	// must be recoverable. That is the same treatment the Reality private key
	// and the Shadowsocks server password already get in proxies.settings_json,
	// and the rendered config in config_versions.config_json contains all
	// three. Never put it in an admin API response.
	Secret    string
	RotatedAt string
	CreatedAt string
	UpdatedAt string
}

// ConnectionTelemetryNode is the collector-facing view of an enabled node: no
// secret, because the only caller is a fleet-wide listing.
type ConnectionTelemetryNode struct {
	NodeID        string
	NodeName      string
	ListenAddress string
	ListenPort    int64
}

// SetNodeConnectionTelemetryParams drives the per-node opt-in. A zero
// ListenAddress or ListenPort takes the loopback default rather than being
// rejected, so enabling telemetry is a one-field call.
type SetNodeConnectionTelemetryParams struct {
	NodeName      string
	Enabled       bool
	ListenAddress string
	ListenPort    int64
}

// ConnectionEventRetentionDays returns the configured retention, falling back
// to the default when the stored value is out of range rather than failing an
// ingest on a bad setting.
func (db *DB) ConnectionEventRetentionDays(ctx context.Context) (int64, error) {
	value, err := db.settingInt(ctx, SettingConnectionEventRetentionDays, DefaultConnectionEventRetentionDays)
	if err != nil {
		return 0, err
	}
	if err := validateConnectionEventRetentionDays(value); err != nil {
		return DefaultConnectionEventRetentionDays, nil
	}
	return value, nil
}

func (db *DB) SetConnectionEventRetentionDays(ctx context.Context, days int64) error {
	if err := validateConnectionEventRetentionDays(days); err != nil {
		return err
	}
	return db.setSettingInt(ctx, SettingConnectionEventRetentionDays, days)
}

func validateConnectionEventRetentionDays(days int64) error {
	if days < 1 || days > MaxConnectionEventRetentionDays {
		return fmt.Errorf("connection event retention days must be between 1 and %d", MaxConnectionEventRetentionDays)
	}
	return nil
}

// ValidateConnectionTelemetryListen rejects any listen address that is not a
// loopback IP literal. "localhost" is rejected on purpose: sing-box's `listen`
// option is a netip.Addr, so a hostname does not parse there, and accepting it
// here would store a value the renderer could only emit as an invalid config.
func ValidateConnectionTelemetryListen(address string, port int64) error {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return errors.New("connection telemetry listen address is required")
	}
	addr, err := netip.ParseAddr(trimmed)
	if err != nil {
		return fmt.Errorf("connection telemetry listen address %q is not an IP literal", address)
	}
	if !addr.IsLoopback() {
		return fmt.Errorf("connection telemetry listen address %q is not loopback", address)
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("connection telemetry listen port %d is out of range", port)
	}
	return nil
}

// ValidateConnectionTelemetrySecret enforces the floor that keeps sing-box's
// authenticate() from short-circuiting to "no auth".
func ValidateConnectionTelemetrySecret(value string) error {
	if len(strings.TrimSpace(value)) < MinConnectionTelemetrySecretLength {
		return fmt.Errorf("connection telemetry secret must be at least %d characters", MinConnectionTelemetrySecretLength)
	}
	return nil
}

// NodeConnectionTelemetryConfig returns a node's opt-in row. The bool is false
// when no row exists, which is the disabled state — callers must treat a
// missing row and enabled=0 identically.
func (db *DB) NodeConnectionTelemetryConfig(ctx context.Context, nodeName string) (NodeConnectionTelemetry, bool, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return NodeConnectionTelemetry{}, false, err
	}
	row, err := db.q.GetNodeConnectionTelemetry(ctx, node.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeConnectionTelemetry{}, false, nil
		}
		return NodeConnectionTelemetry{}, false, err
	}
	return mapNodeConnectionTelemetry(node.Name, row), true, nil
}

// SetNodeConnectionTelemetry creates or updates a node's opt-in, minting a
// secret on first use and preserving the existing one afterwards. Disabling
// keeps the secret so that re-enabling does not force a config change on the
// node; RotateNodeConnectionTelemetrySecret is the way to replace it.
func (db *DB) SetNodeConnectionTelemetry(ctx context.Context, params SetNodeConnectionTelemetryParams) (NodeConnectionTelemetry, error) {
	node, err := db.GetNode(ctx, params.NodeName)
	if err != nil {
		return NodeConnectionTelemetry{}, err
	}
	address := strings.TrimSpace(params.ListenAddress)
	if address == "" {
		address = DefaultConnectionTelemetryListenAddress
	}
	port := params.ListenPort
	if port == 0 {
		port = DefaultConnectionTelemetryListenPort
	}
	if err := ValidateConnectionTelemetryListen(address, port); err != nil {
		return NodeConnectionTelemetry{}, err
	}
	existing, err := db.q.GetNodeConnectionTelemetry(ctx, node.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return NodeConnectionTelemetry{}, err
	}
	value := existing.Secret
	rotatedAt := existing.RotatedAt
	if err := ValidateConnectionTelemetrySecret(value); err != nil {
		value, err = newConnectionTelemetrySecret()
		if err != nil {
			return NodeConnectionTelemetry{}, err
		}
		rotatedAt = sql.NullString{}
	}
	enabled := int64(0)
	if params.Enabled {
		enabled = 1
	}
	if err := db.q.UpsertNodeConnectionTelemetry(ctx, store.UpsertNodeConnectionTelemetryParams{
		NodeID:        node.ID,
		Enabled:       enabled,
		ListenAddress: address,
		ListenPort:    port,
		Secret:        value,
		RotatedAt:     rotatedAt,
	}); err != nil {
		return NodeConnectionTelemetry{}, err
	}
	config, _, err := db.NodeConnectionTelemetryConfig(ctx, node.Name)
	return config, err
}

// RotateNodeConnectionTelemetrySecret replaces the secret in place, leaving the
// enabled flag and listen endpoint untouched. The node picks the new value up
// on its next config apply, so the collector is briefly unauthenticated against
// the old secret — telemetry gaps are expected and recorded as stream resets in
// the report's coverage counters.
func (db *DB) RotateNodeConnectionTelemetrySecret(ctx context.Context, nodeName string) (NodeConnectionTelemetry, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return NodeConnectionTelemetry{}, err
	}
	existing, err := db.q.GetNodeConnectionTelemetry(ctx, node.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeConnectionTelemetry{}, fmt.Errorf("node %q has no connection telemetry configuration", node.Name)
		}
		return NodeConnectionTelemetry{}, err
	}
	value, err := newConnectionTelemetrySecret()
	if err != nil {
		return NodeConnectionTelemetry{}, err
	}
	if err := db.q.UpsertNodeConnectionTelemetry(ctx, store.UpsertNodeConnectionTelemetryParams{
		NodeID:        node.ID,
		Enabled:       existing.Enabled,
		ListenAddress: existing.ListenAddress,
		ListenPort:    existing.ListenPort,
		Secret:        value,
		RotatedAt:     sql.NullString{String: time.Now().UTC().Format(model.ConnectionInstantLayout), Valid: true},
	}); err != nil {
		return NodeConnectionTelemetry{}, err
	}
	config, _, err := db.NodeConnectionTelemetryConfig(ctx, node.Name)
	return config, err
}

// DeleteNodeConnectionTelemetry removes the opt-in row entirely, returning the
// node to the structural default of disabled.
func (db *DB) DeleteNodeConnectionTelemetry(ctx context.Context, nodeName string) error {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return err
	}
	return db.q.DeleteNodeConnectionTelemetry(ctx, node.ID)
}

// ListEnabledConnectionTelemetryNodes lists the nodes currently opted in.
func (db *DB) ListEnabledConnectionTelemetryNodes(ctx context.Context) ([]ConnectionTelemetryNode, error) {
	rows, err := db.q.ListEnabledConnectionTelemetryNodes(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]ConnectionTelemetryNode, 0, len(rows))
	for _, row := range rows {
		nodes = append(nodes, ConnectionTelemetryNode{
			NodeID:        row.NodeID,
			NodeName:      row.NodeName,
			ListenAddress: row.ListenAddress,
			ListenPort:    row.ListenPort,
		})
	}
	return nodes, nil
}

// RecordConnectionReport ingests one collection window from a node.
//
// Idempotency is (node, agent_boot_id, sequence) and works exactly as it does
// for RecordTrafficReport: a retried POST collides on the unique constraint and
// the whole batch is skipped. That is not cosmetic here — buckets are summed
// into existing rows, so a partially applied replay would inflate byte totals
// with no way to detect it afterwards.
//
// Everything on the wire is treated as hostile. A node holds a bearer token,
// not the server's trust: measures are clamped to per-bucket plausibility
// ceilings, strings are truncated, timestamps are re-derived, and the bucket a
// row lands in is re-truncated server-side rather than taken from the node.
func (db *DB) RecordConnectionReport(ctx context.Context, report ConnectionReport) error {
	node, err := db.GetNode(ctx, report.NodeName)
	if err != nil {
		return err
	}
	bootID := strings.TrimSpace(report.AgentBootID)
	if bootID == "" {
		return errors.New("connection report is missing agent_boot_id")
	}
	if len(bootID) > maxConnectionBootIDLen {
		return fmt.Errorf("connection report agent_boot_id exceeds %d characters", maxConnectionBootIDLen)
	}
	if report.Sequence < 0 {
		return fmt.Errorf("connection report sequence %d is negative", report.Sequence)
	}
	now := time.Now().UTC().Format(model.ConnectionInstantLayout)
	reportedAt := model.NormalizeConnectionInstant(report.ReportedAt)
	if reportedAt == "" {
		reportedAt = now
	}
	windowStart := model.NormalizeConnectionInstant(report.WindowStart)
	if windowStart == "" {
		windowStart = reportedAt
	}
	windowEnd := model.NormalizeConnectionInstant(report.WindowEnd)
	if windowEnd == "" || windowEnd < windowStart {
		windowEnd = windowStart
	}
	reportID, err := id.New("cr")
	if err != nil {
		return err
	}
	buckets := report.Buckets
	if len(buckets) > maxConnectionBucketsPerReport {
		buckets = buckets[:maxConnectionBucketsPerReport]
	}
	retentionDays, err := db.ConnectionEventRetentionDays(ctx)
	if err != nil {
		return err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -int(retentionDays)).Format(model.ConnectionInstantLayout)

	return db.withTx(ctx, func(q *store.Queries) error {
		coverage := report.Coverage
		if err := q.CreateConnectionReport(ctx, store.CreateConnectionReportParams{
			ID:                      reportID,
			NodeID:                  node.ID,
			Sequence:                report.Sequence,
			AgentBootID:             bootID,
			WindowStart:             windowStart,
			WindowEnd:               windowEnd,
			ConnectionsObserved:     clampConnectionCount(coverage.ConnectionsObserved),
			ConnectionsAttributed:   clampConnectionCount(coverage.ConnectionsAttributed),
			ConnectionsUnattributed: clampConnectionCount(coverage.ConnectionsUnattributed),
			ConnectionsOrphaned:     clampConnectionCount(coverage.ConnectionsOrphaned),
			StreamResets:            clampConnectionCount(coverage.StreamResets),
			DroppedBuckets:          clampConnectionCount(coverage.DroppedBuckets),
			BytesObserved:           clampConnectionBytes(coverage.BytesObserved),
			BytesAttributed:         clampConnectionBytes(coverage.BytesAttributed),
			ReportedAt:              reportedAt,
		}); err != nil {
			if isSQLiteUniqueConstraint(err) {
				if _, existingErr := q.GetConnectionReportBySequence(ctx, store.GetConnectionReportBySequenceParams{
					NodeID:      node.ID,
					AgentBootID: bootID,
					Sequence:    report.Sequence,
				}); existingErr == nil {
					return nil
				}
			}
			return err
		}
		// One lookup per distinct credential name rather than per bucket: a
		// busy window repeats the same handful of names across hundreds of
		// buckets, and the map is bounded by the batch cap above.
		userIDs := make(map[string]sql.NullString, len(buckets))
		for _, raw := range buckets {
			bucket, ok := sanitizeConnectionBucket(raw)
			if !ok {
				continue
			}
			proxyUserID, cached := userIDs[bucket.AuthName]
			if !cached {
				proxyUserID = sql.NullString{}
				// An empty auth_name is single-user Shadowsocks, which never
				// attributes. The row is still stored against a NULL user:
				// dropping it (as RecordLogEvents does) would silently
				// understate every bytes-per-host total.
				if bucket.AuthName != "" {
					userID, lookupErr := q.GetProxyUserIDByNodeAuthName(ctx, store.GetProxyUserIDByNodeAuthNameParams{
						NodeName: node.Name,
						AuthName: bucket.AuthName,
					})
					if lookupErr == nil {
						proxyUserID = sql.NullString{String: userID, Valid: true}
					} else if !errors.Is(lookupErr, sql.ErrNoRows) {
						return lookupErr
					}
				}
				userIDs[bucket.AuthName] = proxyUserID
			}
			eventID, err := id.New("ce")
			if err != nil {
				return err
			}
			if err := q.UpsertConnectionEvent(ctx, store.UpsertConnectionEventParams{
				ID:                eventID,
				NodeID:            node.ID,
				ProxyUserID:       proxyUserID,
				AuthName:          bucket.AuthName,
				SourceIp:          bucket.SourceIP,
				TargetHost:        bucket.TargetHost,
				TargetPort:        bucket.TargetPort,
				Domain:            bucket.Domain,
				Network:           bucket.Network,
				IpVersion:         bucket.IPVersion,
				Protocol:          bucket.Protocol,
				Inbound:           bucket.Inbound,
				InboundType:       bucket.InboundType,
				Rule:              bucket.Rule,
				Outbound:          bucket.Outbound,
				OutboundType:      bucket.OutboundType,
				Chain:             model.ConnectionChainString(bucket.Chain),
				ConnectionsOpened: bucket.ConnectionsOpened,
				ConnectionsClosed: bucket.ConnectionsClosed,
				UplinkBytes:       bucket.UplinkBytes,
				DownlinkBytes:     bucket.DownlinkBytes,
				DurationMsTotal:   bucket.DurationMsTotal,
				AggregateKey:      connectionEventAggregateKey(node.ID, bucket),
				BucketStart:       bucket.BucketStart,
				WindowStart:       bucket.WindowStart,
				WindowEnd:         bucket.WindowEnd,
			}); err != nil {
				return err
			}
		}
		// Retention rides on ingest, as it does for log events: there is no
		// scheduler in this server, and both deletes are leading-column range
		// scans on an index rather than table scans.
		if err := q.DeleteConnectionEventsBefore(ctx, cutoff); err != nil {
			return err
		}
		return q.DeleteConnectionReportsBefore(ctx, cutoff)
	})
}

// sanitizeConnectionBucket normalises a wire bucket and bounds every field it
// carries. model.ConnectionBucket.Normalize owns canonical form — lowercasing,
// bucket truncation, the domain-wins rule — and this adds the bounds that only
// matter on the receiving side of an untrusted connection.
func sanitizeConnectionBucket(bucket ConnectionBucket) (ConnectionBucket, bool) {
	bucket, ok := bucket.Normalize()
	if !ok {
		return bucket, false
	}
	bucket.AuthName = truncateConnectionField(bucket.AuthName, maxConnectionAuthNameLen)
	bucket.SourceIP = truncateConnectionField(bucket.SourceIP, maxConnectionHostLen)
	bucket.TargetHost = truncateConnectionField(bucket.TargetHost, maxConnectionHostLen)
	bucket.Domain = truncateConnectionField(bucket.Domain, maxConnectionHostLen)
	bucket.Network = truncateConnectionField(bucket.Network, maxConnectionLabelLen)
	bucket.Protocol = truncateConnectionField(bucket.Protocol, maxConnectionLabelLen)
	bucket.Inbound = truncateConnectionField(bucket.Inbound, maxConnectionLabelLen)
	bucket.InboundType = truncateConnectionField(bucket.InboundType, maxConnectionLabelLen)
	bucket.Rule = truncateConnectionField(bucket.Rule, maxConnectionLabelLen)
	bucket.Outbound = truncateConnectionField(bucket.Outbound, maxConnectionLabelLen)
	bucket.OutboundType = truncateConnectionField(bucket.OutboundType, maxConnectionLabelLen)
	if chain := model.ConnectionChainString(bucket.Chain); len(chain) > maxConnectionChainLen {
		bucket.Chain = strings.Split(truncateConnectionField(chain, maxConnectionChainLen), model.ConnectionChainSeparator)
	}
	bucket.ConnectionsOpened = clampConnectionCount(bucket.ConnectionsOpened)
	bucket.ConnectionsClosed = clampConnectionCount(bucket.ConnectionsClosed)
	bucket.UplinkBytes = clampConnectionBytes(bucket.UplinkBytes)
	bucket.DownlinkBytes = clampConnectionBytes(bucket.DownlinkBytes)
	bucket.DurationMsTotal = clampConnectionDuration(bucket.DurationMsTotal)
	// Truncation can empty a host that Normalize accepted only if the host was
	// whitespace, which Normalize already rejects; re-check anyway so the
	// NOT NULL/lowercase invariants on the row cannot be reached by a shorter
	// path later.
	if bucket.TargetHost == "" {
		return bucket, false
	}
	return bucket, true
}

// truncateConnectionField cuts on a rune boundary so a multi-byte value cannot
// be stored half-encoded.
func truncateConnectionField(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := value[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func clampConnectionCount(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > maxConnectionBucketConnections {
		return maxConnectionBucketConnections
	}
	return value
}

func clampConnectionBytes(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > maxConnectionBucketBytes {
		return maxConnectionBucketBytes
	}
	return value
}

func clampConnectionDuration(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > maxConnectionBucketDurationMs {
		return maxConnectionBucketDurationMs
	}
	return value
}

// connectionEventAggregateKey hashes the node id together with the bucket's
// dimension tuple. The node id is inside the hash so two nodes reporting
// identical dimensions cannot collide onto one row, and the result is a fixed
// 64 characters regardless of how long the dimensions are.
func connectionEventAggregateKey(nodeID string, bucket ConnectionBucket) string {
	sum := sha256.Sum256([]byte(nodeID + model.ConnectionDimensionSeparator + bucket.DimensionKey()))
	return hex.EncodeToString(sum[:])
}

func newConnectionTelemetrySecret() (string, error) {
	value, err := secret.HexBytes(connectionTelemetrySecretBytes)
	if err != nil {
		return "", err
	}
	if err := ValidateConnectionTelemetrySecret(value); err != nil {
		return "", err
	}
	return value, nil
}

func mapNodeConnectionTelemetry(nodeName string, row store.NodeConnectionTelemetry) NodeConnectionTelemetry {
	return NodeConnectionTelemetry{
		NodeName:      nodeName,
		Enabled:       row.Enabled == 1,
		ListenAddress: row.ListenAddress,
		ListenPort:    row.ListenPort,
		Secret:        row.Secret,
		RotatedAt:     row.RotatedAt.String,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}
