package db

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

// Ingest tests. The wire payload comes from a node, which holds a bearer token
// and nothing else, so these lean on the hostile cases: replays that would
// double byte totals, values that would poison a running sum, and fields that
// would break an invariant the schema depends on.

const connectionTestBucket = "2026-07-26T12:03:41Z"

func TestRecordConnectionReportIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	report := connectionTestReport(1, connectionTestBucket, ConnectionBucket{
		AuthName:          "vless-39090@alice",
		SourceIP:          "198.51.100.9",
		TargetHost:        "example.com",
		TargetPort:        443,
		ConnectionsOpened: 3,
		ConnectionsClosed: 2,
		UplinkBytes:       1000,
		DownlinkBytes:     9000,
	})
	for attempt := 0; attempt < 3; attempt++ {
		if err := db.RecordConnectionReport(ctx, report); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	rows := listConnectionEvents(t, ctx, db)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	// Buckets are summed on conflict, so a replay that got past the sequence
	// guard would show up here as tripled bytes rather than as an error.
	if rows[0].UplinkBytes != 1000 || rows[0].DownlinkBytes != 9000 {
		t.Fatalf("replay inflated bytes: up=%d down=%d", rows[0].UplinkBytes, rows[0].DownlinkBytes)
	}
	if rows[0].ConnectionsOpened != 3 || rows[0].ConnectionsClosed != 2 {
		t.Fatalf("replay inflated counts: opened=%d closed=%d", rows[0].ConnectionsOpened, rows[0].ConnectionsClosed)
	}
	if count := countConnectionReports(t, ctx, db); count != 1 {
		t.Fatalf("connection_reports = %d, want 1", count)
	}
}

// A new agent boot restarts the sequence at zero, so idempotency is keyed on
// the pair and a fresh boot id must not be mistaken for a replay.
func TestRecordConnectionReportSeparatesAgentBoots(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	first := connectionTestReport(1, connectionTestBucket, ConnectionBucket{
		AuthName: "vless-39090@alice", SourceIP: "198.51.100.9",
		TargetHost: "example.com", TargetPort: 443, UplinkBytes: 500,
	})
	second := first
	second.AgentBootID = "boot-2"
	if err := db.RecordConnectionReport(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordConnectionReport(ctx, second); err != nil {
		t.Fatal(err)
	}
	rows := listConnectionEvents(t, ctx, db)
	if len(rows) != 1 || rows[0].UplinkBytes != 1000 {
		t.Fatalf("rows = %d, uplink = %d, want one row of 1000", len(rows), rows[0].UplinkBytes)
	}
}

// Attribution mirrors what the wire can carry: VLESS and multi-user
// Shadowsocks populate `user`, single-user Shadowsocks never does. Rows in the
// second class are kept against a NULL user — dropping them, as RecordLogEvents
// does, would silently understate every bytes-per-host total.
func TestRecordConnectionReportKeepsUnattributedRows(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	err := db.RecordConnectionReport(ctx, connectionTestReport(1, connectionTestBucket,
		ConnectionBucket{
			AuthName: "vless-39090@alice", SourceIP: "198.51.100.9",
			TargetHost: "example.com", TargetPort: 443, UplinkBytes: 100, DownlinkBytes: 200,
		},
		ConnectionBucket{
			AuthName: "", SourceIP: "198.51.100.10",
			TargetHost: "cdn.example.net", TargetPort: 443, UplinkBytes: 7, DownlinkBytes: 11,
		},
		ConnectionBucket{
			AuthName: "ss-8443@nobody", SourceIP: "198.51.100.11",
			TargetHost: "other.example.org", TargetPort: 80, UplinkBytes: 1, DownlinkBytes: 2,
		},
	))
	if err != nil {
		t.Fatal(err)
	}

	byHost := make(map[string]store.ListConnectionEventsPageRow)
	for _, row := range listConnectionEvents(t, ctx, db) {
		byHost[row.TargetHost] = row
	}
	if len(byHost) != 3 {
		t.Fatalf("hosts = %d, want 3", len(byHost))
	}
	if !byHost["example.com"].ProxyUserID.Valid {
		t.Fatal("known credential was not attributed")
	}
	if byHost["cdn.example.net"].ProxyUserID.Valid {
		t.Fatal("empty auth_name was attributed to a user")
	}
	// An auth name with no matching credential is the same shape as
	// unattributed: keep the bytes, lose the user.
	if byHost["other.example.org"].ProxyUserID.Valid {
		t.Fatal("unknown credential was attributed to a user")
	}
	total, err := db.q.SumConnectionBytesByHost(ctx, store.SumConnectionBytesByHostParams{
		StartTime: "2026-07-26T00:00:00.000Z",
		EndTime:   "2026-07-27T00:00:00.000Z",
		// Optional filters are interface{} because of the `arg = '' OR ...`
		// idiom; nil would make every comparison NULL and return nothing.
		NodeID:      "",
		ProxyUserID: "",
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(total) != 3 {
		t.Fatalf("hosts in byte totals = %d, want 3", len(total))
	}
}

// The dimension tuple is the aggregation identity, so a second report covering
// the same tuple must merge rather than fork a row — that is what stops one
// long-lived session from becoming several fragments.
func TestRecordConnectionReportMergesAcrossReports(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	bucket := ConnectionBucket{
		AuthName: "vless-39090@alice", SourceIP: "198.51.100.9",
		TargetHost: "example.com", TargetPort: 443,
		ConnectionsOpened: 1, UplinkBytes: 100, DownlinkBytes: 200, DurationMsTotal: 0,
		WindowStart: "2026-07-26T12:03:00Z", WindowEnd: "2026-07-26T12:04:00Z",
	}
	if err := db.RecordConnectionReport(ctx, connectionTestReport(1, connectionTestBucket, bucket)); err != nil {
		t.Fatal(err)
	}
	second := bucket
	second.ConnectionsOpened = 0
	second.ConnectionsClosed = 1
	second.UplinkBytes = 50
	second.DownlinkBytes = 60
	second.DurationMsTotal = 61000
	second.WindowStart = "2026-07-26T12:05:00Z"
	second.WindowEnd = "2026-07-26T12:06:00Z"
	if err := db.RecordConnectionReport(ctx, connectionTestReport(2, connectionTestBucket, second)); err != nil {
		t.Fatal(err)
	}

	rows := listConnectionEvents(t, ctx, db)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 merged row", len(rows))
	}
	row := rows[0]
	if row.UplinkBytes != 150 || row.DownlinkBytes != 260 {
		t.Fatalf("bytes not merged: up=%d down=%d", row.UplinkBytes, row.DownlinkBytes)
	}
	if row.ConnectionsOpened != 1 || row.ConnectionsClosed != 1 || row.DurationMsTotal != 61000 {
		t.Fatalf("counters not merged: %+v", row)
	}
	// The extremes have to widen, not be replaced, or a merged row would claim
	// a window narrower than the traffic it holds.
	if row.WindowStart != "2026-07-26T12:03:00.000Z" || row.WindowEnd != "2026-07-26T12:06:00.000Z" {
		t.Fatalf("window not widened: %s .. %s", row.WindowStart, row.WindowEnd)
	}
}

// Every measure on the wire is added into a total that is never recomputed, so
// an implausible value has to be clamped on the way in rather than corrected
// later. Rows that cannot be placed on the time axis or the host axis at all
// are dropped instead of stored half-formed.
func TestRecordConnectionReportClampsHostileValues(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	longHost := strings.Repeat("a", 400) + ".example.com"
	err := db.RecordConnectionReport(ctx, connectionTestReport(1, connectionTestBucket,
		ConnectionBucket{
			// Negative measures and an over-long, upper-cased host with a
			// trailing root dot.
			AuthName: "VLESS-39090@ALICE", SourceIP: " 198.51.100.9 ",
			TargetHost: strings.ToUpper(longHost) + ".", TargetPort: 443,
			ConnectionsOpened: -5, ConnectionsClosed: -1,
			UplinkBytes: -1, DownlinkBytes: -1, DurationMsTotal: -1,
		},
		ConnectionBucket{
			// Byte totals no five-minute window on any link could produce.
			AuthName: "vless-39090@alice", SourceIP: "198.51.100.9",
			TargetHost: "big.example.com", TargetPort: 443,
			UplinkBytes: 1 << 62, DownlinkBytes: 1 << 62, DurationMsTotal: 1 << 62,
			ConnectionsOpened: 1 << 40,
		},
		ConnectionBucket{
			// Port out of range: no axis to place it on, so it is dropped.
			AuthName: "vless-39090@alice", TargetHost: "badport.example.com", TargetPort: 70000,
		},
		ConnectionBucket{
			// Unparseable bucket timestamp, overridden below.
			AuthName: "vless-39090@alice", TargetHost: "badtime.example.com", TargetPort: 443,
		},
		ConnectionBucket{
			// No host at all.
			AuthName: "vless-39090@alice", TargetHost: "   ", TargetPort: 443,
		},
	))
	if err != nil {
		t.Fatal(err)
	}

	byHost := make(map[string]store.ListConnectionEventsPageRow)
	for _, row := range listConnectionEvents(t, ctx, db) {
		byHost[row.TargetHost] = row
	}
	if len(byHost) != 2 {
		t.Fatalf("stored hosts = %d, want 2 (three buckets are unplaceable): %v", len(byHost), byHost)
	}
	for host, row := range byHost {
		if host != strings.ToLower(host) {
			t.Fatalf("host %q was stored un-normalised", host)
		}
		if len(host) > maxConnectionHostLen {
			t.Fatalf("host %q is %d characters, want at most %d", host, len(host), maxConnectionHostLen)
		}
		if row.AuthName != "vless-39090@alice" {
			t.Fatalf("auth name %q was not trimmed to its stored form", row.AuthName)
		}
		if row.UplinkBytes < 0 || row.DownlinkBytes < 0 || row.DurationMsTotal < 0 {
			t.Fatalf("negative measure survived: %+v", row)
		}
		if row.UplinkBytes > maxConnectionBucketBytes || row.DownlinkBytes > maxConnectionBucketBytes {
			t.Fatalf("byte ceiling not applied: %+v", row)
		}
		if row.ConnectionsOpened > maxConnectionBucketConnections {
			t.Fatalf("connection ceiling not applied: %+v", row)
		}
		if row.DurationMsTotal > maxConnectionBucketDurationMs {
			t.Fatalf("duration ceiling not applied: %+v", row)
		}
	}
}

// A node does not get to place its own rows on the time axis: whatever it
// sends is re-truncated to the bucket grid server-side.
func TestRecordConnectionReportRetruncatesBucketStart(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	if err := db.RecordConnectionReport(ctx, connectionTestReport(1, "2026-07-26T12:03:41.812Z", ConnectionBucket{
		AuthName: "vless-39090@alice", TargetHost: "example.com", TargetPort: 443, UplinkBytes: 1,
	})); err != nil {
		t.Fatal(err)
	}
	rows := listConnectionEvents(t, ctx, db)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].BucketStart != "2026-07-26T12:00:00.000Z" {
		t.Fatalf("bucket_start = %q, want the five-minute grid point", rows[0].BucketStart)
	}
}

func TestRecordConnectionReportRejectsMalformedEnvelope(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	bucket := ConnectionBucket{AuthName: "vless-39090@alice", TargetHost: "example.com", TargetPort: 443}
	missingBoot := connectionTestReport(1, connectionTestBucket, bucket)
	missingBoot.AgentBootID = "  "
	if err := db.RecordConnectionReport(ctx, missingBoot); err == nil {
		t.Error("report with no agent_boot_id was accepted")
	}

	longBoot := connectionTestReport(1, connectionTestBucket, bucket)
	longBoot.AgentBootID = strings.Repeat("b", maxConnectionBootIDLen+1)
	if err := db.RecordConnectionReport(ctx, longBoot); err == nil {
		t.Error("report with an oversized agent_boot_id was accepted")
	}

	negative := connectionTestReport(-1, connectionTestBucket, bucket)
	if err := db.RecordConnectionReport(ctx, negative); err == nil {
		t.Error("report with a negative sequence was accepted")
	}

	unknownNode := connectionTestReport(1, connectionTestBucket, bucket)
	unknownNode.NodeName = "does-not-exist"
	if err := db.RecordConnectionReport(ctx, unknownNode); err == nil {
		t.Error("report for an unknown node was accepted")
	}
}

// A batch larger than the cap is truncated rather than rejected: the node has
// already lost the surplus either way, and refusing the whole window would
// discard the buckets that did fit.
func TestRecordConnectionReportCapsBatchSize(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	report := connectionTestReport(1, connectionTestBucket)
	for i := 0; i < maxConnectionBucketsPerReport+50; i++ {
		report.Buckets = append(report.Buckets, ConnectionBucket{
			BucketStart: connectionTestBucket,
			AuthName:    "vless-39090@alice",
			TargetHost:  "host" + string(rune('a'+i%26)) + "-" + strconv.Itoa(i) + ".example.com",
			TargetPort:  443,
			UplinkBytes: 1,
		})
	}
	if err := db.RecordConnectionReport(ctx, report); err != nil {
		t.Fatal(err)
	}
	if rows := listConnectionEvents(t, ctx, db); len(rows) != maxConnectionBucketsPerReport {
		t.Fatalf("rows = %d, want the batch cap of %d", len(rows), maxConnectionBucketsPerReport)
	}
}

// Retention rides on ingest because this server has no scheduler. A bucket
// older than the window is deleted by the very report that carries it, which
// also proves the cutoff and the stored timestamps compare correctly as text.
func TestRecordConnectionReportAppliesRetentionInline(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	if err := db.SetConnectionEventRetentionDays(ctx, 2); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	fresh := now.Add(-30 * time.Minute).Format(time.RFC3339Nano)
	stale := now.AddDate(0, 0, -10).Format(time.RFC3339Nano)

	report := connectionTestReport(1, fresh, ConnectionBucket{
		BucketStart: fresh, AuthName: "vless-39090@alice",
		TargetHost: "fresh.example.com", TargetPort: 443, UplinkBytes: 5,
	}, ConnectionBucket{
		BucketStart: stale, AuthName: "vless-39090@alice",
		TargetHost: "stale.example.com", TargetPort: 443, UplinkBytes: 5,
	})
	report.WindowStart = fresh
	report.WindowEnd = fresh
	report.ReportedAt = fresh
	if err := db.RecordConnectionReport(ctx, report); err != nil {
		t.Fatal(err)
	}

	rows := listConnectionEvents(t, ctx, db)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want only the fresh bucket", len(rows))
	}
	if rows[0].TargetHost != "fresh.example.com" {
		t.Fatalf("surviving host = %q", rows[0].TargetHost)
	}

	// The report row itself outlives its own ingest and is swept by a later
	// one, since its window is inside the retention period.
	if count := countConnectionReports(t, ctx, db); count != 1 {
		t.Fatalf("connection_reports = %d, want 1", count)
	}
	stalePost := connectionTestReport(2, fresh)
	stalePost.WindowStart = stale
	stalePost.WindowEnd = stale
	stalePost.ReportedAt = stale
	if err := db.RecordConnectionReport(ctx, stalePost); err != nil {
		t.Fatal(err)
	}
	if count := countConnectionReports(t, ctx, db); count != 1 {
		t.Fatalf("connection_reports = %d after a stale window, want 1", count)
	}
}

func TestConnectionEventRetentionDaysFallsBackOnBadSetting(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	days, err := db.ConnectionEventRetentionDays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if days != DefaultConnectionEventRetentionDays {
		t.Fatalf("default retention = %d, want %d", days, DefaultConnectionEventRetentionDays)
	}
	for _, bad := range []int64{0, -1, MaxConnectionEventRetentionDays + 1} {
		if err := db.SetConnectionEventRetentionDays(ctx, bad); err == nil {
			t.Errorf("retention %d was accepted", bad)
		}
	}
	// A value written past the facade must not stall ingest.
	if err := db.setSettingInt(ctx, SettingConnectionEventRetentionDays, 0); err != nil {
		t.Fatal(err)
	}
	days, err = db.ConnectionEventRetentionDays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if days != DefaultConnectionEventRetentionDays {
		t.Fatalf("retention = %d after a bad stored value, want the default", days)
	}
}

// Secret lifecycle. The secret is stored in the clear because the renderer has
// to emit it into the node config — see the note on NodeConnectionTelemetry —
// so the discipline that remains is: never empty, never short, never
// off-loopback, and replaceable.
func TestNodeConnectionTelemetrySecretLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	if _, ok, err := db.NodeConnectionTelemetryConfig(ctx, "azus"); err != nil || ok {
		t.Fatalf("fresh node reported telemetry: ok=%v err=%v", ok, err)
	}

	enabled, err := db.SetNodeConnectionTelemetry(ctx, SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Enabled {
		t.Fatal("opt-in did not enable")
	}
	if err := ValidateConnectionTelemetrySecret(enabled.Secret); err != nil {
		t.Fatalf("minted secret is unusable: %v", err)
	}
	if enabled.ListenAddress != DefaultConnectionTelemetryListenAddress || enabled.ListenPort != DefaultConnectionTelemetryListenPort {
		t.Fatalf("unexpected default endpoint: %+v", enabled)
	}

	// Disabling keeps the secret so re-enabling is not a config change on the
	// node; rotation is the way to replace it.
	disabled, err := db.SetNodeConnectionTelemetry(ctx, SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled {
		t.Fatal("opt-out did not disable")
	}
	if disabled.Secret != enabled.Secret {
		t.Fatal("disabling rotated the secret")
	}

	rotated, err := db.RotateNodeConnectionTelemetrySecret(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Secret == enabled.Secret {
		t.Fatal("rotation reused the secret")
	}
	if err := ValidateConnectionTelemetrySecret(rotated.Secret); err != nil {
		t.Fatalf("rotated secret is unusable: %v", err)
	}
	if rotated.RotatedAt == "" {
		t.Fatal("rotation did not stamp rotated_at")
	}
	if rotated.Enabled != disabled.Enabled {
		t.Fatal("rotation changed the enabled flag")
	}

	if err := db.DeleteNodeConnectionTelemetry(ctx, "azus"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.NodeConnectionTelemetryConfig(ctx, "azus"); err != nil || ok {
		t.Fatalf("delete left a row: ok=%v err=%v", ok, err)
	}
	if _, err := db.RotateNodeConnectionTelemetrySecret(ctx, "azus"); err == nil {
		t.Error("rotating a node with no configuration was accepted")
	}
}

func TestSetNodeConnectionTelemetryRejectsPublicBind(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	for _, address := range []string{"0.0.0.0", "203.0.113.10", "::", "localhost", "not-an-ip"} {
		if _, err := db.SetNodeConnectionTelemetry(ctx, SetNodeConnectionTelemetryParams{
			NodeName:      "azus",
			Enabled:       true,
			ListenAddress: address,
		}); err == nil {
			t.Errorf("listen address %q was accepted", address)
		}
	}
	if _, err := db.SetNodeConnectionTelemetry(ctx, SetNodeConnectionTelemetryParams{
		NodeName: "azus", Enabled: true, ListenAddress: "::1", ListenPort: 19091,
	}); err != nil {
		t.Fatalf("IPv6 loopback was refused: %v", err)
	}
}

func TestConnectionTelemetryValidators(t *testing.T) {
	if err := ValidateConnectionTelemetryListen("127.0.0.1", 0); err == nil {
		t.Error("port 0 was accepted")
	}
	if err := ValidateConnectionTelemetryListen("127.0.0.1", 70000); err == nil {
		t.Error("port 70000 was accepted")
	}
	if err := ValidateConnectionTelemetryListen("", DefaultConnectionTelemetryListenPort); err == nil {
		t.Error("empty listen address was accepted")
	}
	// The one that matters most: sing-box's authenticate() returns nil — not an
	// error — when the secret is empty, so an empty secret is an open control
	// plane, not a closed one.
	if err := ValidateConnectionTelemetrySecret(""); err == nil {
		t.Error("empty secret was accepted")
	}
	if err := ValidateConnectionTelemetrySecret(strings.Repeat("a", MinConnectionTelemetrySecretLength-1)); err == nil {
		t.Error("short secret was accepted")
	}
	if err := ValidateConnectionTelemetrySecret(strings.Repeat(" ", 64)); err == nil {
		t.Error("whitespace secret was accepted")
	}
}

func TestListEnabledConnectionTelemetryNodes(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, db)

	nodes, err := db.ListEnabledConnectionTelemetryNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Fatalf("enabled nodes = %d before any opt-in", len(nodes))
	}
	if _, err := db.SetNodeConnectionTelemetry(ctx, SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	nodes, err = db.ListEnabledConnectionTelemetryNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].NodeName != "azus" {
		t.Fatalf("enabled nodes = %+v", nodes)
	}
	if nodes[0].ListenAddress != DefaultConnectionTelemetryListenAddress {
		t.Fatalf("unexpected listen address %q", nodes[0].ListenAddress)
	}
}

// The aggregate key carries the node id, so two nodes reporting an identical
// dimension tuple must land on separate rows rather than merging into one.
func TestConnectionEventAggregateKeyIsPerNode(t *testing.T) {
	bucket, ok := ConnectionBucket{
		BucketStart: connectionTestBucket, AuthName: "shared",
		TargetHost: "example.com", TargetPort: 443,
	}.Normalize()
	if !ok {
		t.Fatal("fixture bucket did not normalise")
	}
	first := connectionEventAggregateKey("node-a", bucket)
	second := connectionEventAggregateKey("node-b", bucket)
	if first == second {
		t.Fatal("aggregate key ignores the node id")
	}
	if len(first) != 64 {
		t.Fatalf("aggregate key is %d characters, want a 64-character sha256 hex", len(first))
	}
	if again := connectionEventAggregateKey("node-a", bucket); again != first {
		t.Fatal("aggregate key is not stable")
	}
}

func connectionTestReport(sequence int64, bucketStart string, buckets ...ConnectionBucket) ConnectionReport {
	for i := range buckets {
		if buckets[i].BucketStart == "" {
			buckets[i].BucketStart = bucketStart
		}
	}
	return ConnectionReport{
		NodeName:    "azus",
		Sequence:    sequence,
		AgentBootID: "boot-1",
		WindowStart: bucketStart,
		WindowEnd:   bucketStart,
		ReportedAt:  bucketStart,
		Coverage: ConnectionCoverage{
			ConnectionsObserved:   int64(len(buckets)),
			ConnectionsAttributed: int64(len(buckets)),
			BytesObserved:         1,
			BytesAttributed:       1,
		},
		Buckets: buckets,
	}
}

func listConnectionEvents(t *testing.T, ctx context.Context, db *DB) []store.ListConnectionEventsPageRow {
	t.Helper()
	rows, err := db.q.ListConnectionEventsPage(ctx, store.ListConnectionEventsPageParams{
		NodeID:      "",
		ProxyUserID: "",
		StartTime:   "",
		EndTime:     "",
		Offset:      0,
		Limit:       int64(maxConnectionBucketsPerReport + 100),
	})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func countConnectionReports(t *testing.T, ctx context.Context, db *DB) int64 {
	t.Helper()
	var count int64
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_reports`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// seedConnectionIngestFixture builds the smallest real graph ingest needs: a
// node with a VLESS proxy and one issued credential, so auth-name attribution
// resolves through proxy_accesses the same way it does in production.
func seedConnectionIngestFixture(t *testing.T, ctx context.Context, db *DB) {
	t.Helper()
	if _, err := db.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "azus",
		Name:         "vless-39090",
		Protocol:     ProtocolVLESSReality,
		Listen:       "0.0.0.0",
		ListenPort:   39090,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: `{"server_name":"www.amazon.com","reality_private_key":"private","reality_public_key":"public","short_id":"01234567","handshake_server":"www.amazon.com","handshake_port":443}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BindUserToNode(ctx, "alice", "azus"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IssueVLESSRealityCredential(ctx, IssueCredentialParams{
		UserName:  "alice",
		NodeName:  "azus",
		ProxyName: "vless-39090",
	}); err != nil {
		t.Fatal(err)
	}
}
