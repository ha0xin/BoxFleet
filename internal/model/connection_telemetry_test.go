package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNormalizeConnectionHost(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"EXAMPLE.com", "example.com"},
		{"  Example.COM  ", "example.com"},
		{"example.com.", "example.com"},
		{"[2001:DB8::1]", "2001:db8::1"},
		{"2001:db8::1", "2001:db8::1"},
		{".", "."},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizeConnectionHost(tt.in); got != tt.want {
			t.Errorf("NormalizeConnectionHost(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitConnectionAddress(t *testing.T) {
	tests := []struct {
		in       string
		wantHost string
		wantPort int64
		wantOK   bool
	}{
		{"Example.COM:443", "example.com", 443, true},
		{"[2001:db8::1]:8080", "2001:db8::1", 8080, true},
		{"198.51.100.7:53", "198.51.100.7", 53, true},
		{"example.com", "", 0, false},
		{"example.com:", "", 0, false},
		{":443", "", 0, false},
		{"example.com:70000", "", 0, false},
		{"example.com:https", "", 0, false},
	}
	for _, tt := range tests {
		host, port, ok := SplitConnectionAddress(tt.in)
		if host != tt.wantHost || port != tt.wantPort || ok != tt.wantOK {
			t.Errorf("SplitConnectionAddress(%q) = (%q, %d, %t), want (%q, %d, %t)",
				tt.in, host, port, ok, tt.wantHost, tt.wantPort, tt.wantOK)
		}
	}
}

// The gotcha this design exists to absorb: buildConnectionProto has no fallback
// to Destination.Fqdn and BoxFleet renders no sniff action, so the host arrives
// in `destination`. Domain must win when it is populated and be invisible when
// it is not.
func TestConnectionBucketPrefersDomainOverDestinationHost(t *testing.T) {
	withoutDomain, ok := ConnectionBucket{
		BucketStart: "2026-07-26T10:03:11Z",
		TargetHost:  "203.0.113.9",
		TargetPort:  443,
	}.Normalize()
	if !ok {
		t.Fatal("bucket without domain rejected")
	}
	if withoutDomain.TargetHost != "203.0.113.9" {
		t.Fatalf("target host = %q, want the destination host", withoutDomain.TargetHost)
	}

	withDomain, ok := ConnectionBucket{
		BucketStart: "2026-07-26T10:03:11Z",
		TargetHost:  "203.0.113.9",
		Domain:      "Cdn.EXAMPLE.com",
		TargetPort:  443,
	}.Normalize()
	if !ok {
		t.Fatal("bucket with domain rejected")
	}
	if withDomain.TargetHost != "cdn.example.com" {
		t.Fatalf("target host = %q, want the lowercased sniffed domain", withDomain.TargetHost)
	}
	if withDomain.DimensionKey() == withoutDomain.DimensionKey() {
		t.Fatal("sniffed and unsniffed buckets share a dimension key")
	}
}

func TestConnectionBucketNormalizeIsIdempotent(t *testing.T) {
	bucket := ConnectionBucket{
		BucketStart:       "2026-07-26T10:03:11.482Z",
		AuthName:          " alice ",
		SourceIP:          "[2001:DB8::5]",
		TargetHost:        "CDN.Example.com.",
		TargetPort:        443,
		Network:           "TCP",
		IPVersion:         6,
		Protocol:          "TLS",
		Inbound:           " vless-in ",
		Chain:             []string{"vless-in", "", " direct "},
		ConnectionsOpened: 3,
		UplinkBytes:       10,
		WindowStart:       "2026-07-26T10:03:11.482913Z",
		WindowEnd:         "2026-07-26T10:04:00Z",
	}
	first, ok := bucket.Normalize()
	if !ok {
		t.Fatal("normalize rejected a valid bucket")
	}
	second, ok := first.Normalize()
	if !ok {
		t.Fatal("normalize rejected its own output")
	}
	if first.DimensionKey() != second.DimensionKey() {
		t.Fatalf("dimension key not stable:\nfirst  = %q\nsecond = %q", first.DimensionKey(), second.DimensionKey())
	}
	if first.BucketStart != "2026-07-26T10:00:00.000Z" {
		t.Fatalf("bucket start = %q, want the 5-minute grid point", first.BucketStart)
	}
	if first.AuthName != "alice" || first.SourceIP != "2001:db8::5" || first.TargetHost != "cdn.example.com" {
		t.Fatalf("dimensions not canonicalised: %+v", first)
	}
	if first.Network != "tcp" || first.Protocol != "tls" || first.Inbound != "vless-in" {
		t.Fatalf("descriptive fields not canonicalised: %+v", first)
	}
	if got := ConnectionChainString(first.Chain); got != "vless-in>direct" {
		t.Fatalf("chain = %q, want vless-in>direct", got)
	}
}

// window_start and window_end are folded with SQL MIN()/MAX() over TEXT, so the
// stored representation has to be fixed width or the wrong extreme survives a
// merge. RFC3339Nano trims trailing zeros and would break exactly this pair.
func TestConnectionInstantsCompareLexicographically(t *testing.T) {
	const (
		rawEarlier = "2026-07-26T10:03:11Z"
		rawLater   = "2026-07-26T10:03:11.482Z"
	)
	// The trap being closed: unpadded RFC3339Nano compares '.' (0x2E) against
	// 'Z' (0x5A), so the later instant sorts first and SQL MAX() picks it.
	if !(rawLater < rawEarlier) {
		t.Fatal("test premise broken: raw RFC3339Nano already compares correctly")
	}

	earlier := NormalizeConnectionInstant(rawEarlier)
	later := NormalizeConnectionInstant(rawLater)
	if earlier == "" || later == "" {
		t.Fatalf("normalize returned empty: %q %q", earlier, later)
	}
	if !(earlier < later) {
		t.Fatalf("%q should sort before %q", earlier, later)
	}
	if len(earlier) != len(later) {
		t.Fatalf("instant layout is not fixed width: %q vs %q", earlier, later)
	}
}

func TestTruncateConnectionBucketGrid(t *testing.T) {
	if ConnectionBucketInterval != 5*time.Minute {
		t.Fatalf("bucket interval = %s; the volume estimate on connection_events assumes 5m", ConnectionBucketInterval)
	}
	tests := map[string]string{
		"2026-07-26T10:00:00Z":      "2026-07-26T10:00:00.000Z",
		"2026-07-26T10:04:59.999Z":  "2026-07-26T10:00:00.000Z",
		"2026-07-26T10:05:00Z":      "2026-07-26T10:05:00.000Z",
		"2026-07-26T12:07:30+02:00": "2026-07-26T10:05:00.000Z",
		"not-a-time":                "",
	}
	for in, want := range tests {
		if got := TruncateConnectionBucket(in); got != want {
			t.Errorf("TruncateConnectionBucket(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConnectionBucketNormalizeRejectsUnusableRows(t *testing.T) {
	valid := ConnectionBucket{
		BucketStart: "2026-07-26T10:00:00Z",
		TargetHost:  "example.com",
		TargetPort:  443,
	}
	if _, ok := valid.Normalize(); !ok {
		t.Fatal("baseline bucket rejected")
	}

	rejects := map[string]ConnectionBucket{
		"no bucket start": {TargetHost: "example.com", TargetPort: 443},
		"unparseable bucket start": {
			BucketStart: "yesterday", TargetHost: "example.com", TargetPort: 443,
		},
		"no host": {BucketStart: "2026-07-26T10:00:00Z", TargetPort: 443},
		"port out of range": {
			BucketStart: "2026-07-26T10:00:00Z", TargetHost: "example.com", TargetPort: 70000,
		},
		"negative bytes": {
			BucketStart: "2026-07-26T10:00:00Z", TargetHost: "example.com",
			TargetPort: 443, DownlinkBytes: -1,
		},
		"negative connections": {
			BucketStart: "2026-07-26T10:00:00Z", TargetHost: "example.com",
			TargetPort: 443, ConnectionsOpened: -1,
		},
	}
	for name, bucket := range rejects {
		if _, ok := bucket.Normalize(); ok {
			t.Errorf("%s: normalize accepted an unusable bucket", name)
		}
	}
}

func TestConnectionBucketDimensionKeySeparatesEveryDimension(t *testing.T) {
	base := ConnectionBucket{
		BucketStart:  "2026-07-26T10:00:00Z",
		AuthName:     "alice",
		SourceIP:     "198.51.100.4",
		TargetHost:   "example.com",
		TargetPort:   443,
		Network:      "tcp",
		IPVersion:    4,
		Protocol:     "tls",
		Inbound:      "vless-in",
		InboundType:  "vless",
		Rule:         "default",
		Outbound:     "direct",
		OutboundType: "direct",
		Chain:        []string{"vless-in", "direct"},
	}
	variants := map[string]func(*ConnectionBucket){
		"bucket start":  func(b *ConnectionBucket) { b.BucketStart = "2026-07-26T10:05:00Z" },
		"auth name":     func(b *ConnectionBucket) { b.AuthName = "bob" },
		"source ip":     func(b *ConnectionBucket) { b.SourceIP = "198.51.100.5" },
		"target host":   func(b *ConnectionBucket) { b.TargetHost = "other.example.com" },
		"target port":   func(b *ConnectionBucket) { b.TargetPort = 8443 },
		"domain":        func(b *ConnectionBucket) { b.Domain = "sniffed.example.com" },
		"network":       func(b *ConnectionBucket) { b.Network = "udp" },
		"ip version":    func(b *ConnectionBucket) { b.IPVersion = 6 },
		"protocol":      func(b *ConnectionBucket) { b.Protocol = "http" },
		"inbound":       func(b *ConnectionBucket) { b.Inbound = "ss-in" },
		"inbound type":  func(b *ConnectionBucket) { b.InboundType = "shadowsocks" },
		"rule":          func(b *ConnectionBucket) { b.Rule = "block" },
		"outbound":      func(b *ConnectionBucket) { b.Outbound = "proxy" },
		"outbound type": func(b *ConnectionBucket) { b.OutboundType = "socks" },
		"chain":         func(b *ConnectionBucket) { b.Chain = []string{"vless-in", "proxy"} },
	}

	normalizedBase, ok := base.Normalize()
	if !ok {
		t.Fatal("base bucket rejected")
	}
	baseKey := normalizedBase.DimensionKey()
	for name, mutate := range variants {
		variant := base
		mutate(&variant)
		normalized, ok := variant.Normalize()
		if !ok {
			t.Fatalf("%s: variant rejected", name)
		}
		if normalized.DimensionKey() == baseKey {
			t.Errorf("%s: dimension key did not change", name)
		}
	}

	// Measures must not participate, or a bucket would never merge with itself.
	measured := base
	measured.ConnectionsOpened = 9
	measured.ConnectionsClosed = 7
	measured.UplinkBytes = 1 << 20
	measured.DownlinkBytes = 1 << 22
	measured.DurationMsTotal = 91234
	measured.WindowStart = "2026-07-26T10:01:00Z"
	measured.WindowEnd = "2026-07-26T10:04:00Z"
	normalizedMeasured, ok := measured.Normalize()
	if !ok {
		t.Fatal("measured variant rejected")
	}
	if normalizedMeasured.DimensionKey() != baseKey {
		t.Fatal("measures leaked into the dimension key")
	}
	if strings.Count(baseKey, ConnectionDimensionSeparator) != len(variants)-1 {
		t.Fatalf("dimension key has %d separators for %d dimensions",
			strings.Count(baseKey, ConnectionDimensionSeparator), len(variants))
	}
}

func TestConnectionBucketMergeFoldsMeasuresAndWidensWindow(t *testing.T) {
	first, _ := ConnectionBucket{
		BucketStart: "2026-07-26T10:00:00Z", TargetHost: "example.com", TargetPort: 443,
		ConnectionsOpened: 2, ConnectionsClosed: 1,
		UplinkBytes: 100, DownlinkBytes: 900, DurationMsTotal: 4000,
		WindowStart: "2026-07-26T10:01:00Z", WindowEnd: "2026-07-26T10:02:00Z",
	}.Normalize()
	second, _ := ConnectionBucket{
		BucketStart: "2026-07-26T10:00:00Z", TargetHost: "example.com", TargetPort: 443,
		ConnectionsOpened: 3, ConnectionsClosed: 4,
		UplinkBytes: 20, DownlinkBytes: 80, DurationMsTotal: 1000,
		WindowStart: "2026-07-26T10:00:30Z", WindowEnd: "2026-07-26T10:04:30Z",
	}.Normalize()

	merged := first
	merged.Merge(second)
	if merged.ConnectionsOpened != 5 || merged.ConnectionsClosed != 5 {
		t.Fatalf("connection counters = (%d, %d), want (5, 5)", merged.ConnectionsOpened, merged.ConnectionsClosed)
	}
	if merged.UplinkBytes != 120 || merged.DownlinkBytes != 980 || merged.DurationMsTotal != 5000 {
		t.Fatalf("measures = (%d, %d, %d)", merged.UplinkBytes, merged.DownlinkBytes, merged.DurationMsTotal)
	}
	if merged.WindowStart != "2026-07-26T10:00:30.000Z" {
		t.Fatalf("window start = %q, want the earlier of the two", merged.WindowStart)
	}
	if merged.WindowEnd != "2026-07-26T10:04:30.000Z" {
		t.Fatalf("window end = %q, want the later of the two", merged.WindowEnd)
	}
	if merged.DimensionKey() != first.DimensionKey() {
		t.Fatal("merge changed the dimension key")
	}
}

func TestConnectionAttributionRatio(t *testing.T) {
	tests := []struct {
		name     string
		coverage ConnectionCoverage
		want     float64
	}{
		{"idle window reports full coverage", ConnectionCoverage{}, 1},
		{
			"half the bytes attributed",
			ConnectionCoverage{BytesObserved: 1000, BytesAttributed: 500},
			0.5,
		},
		{
			"single-user shadowsocks only",
			ConnectionCoverage{BytesObserved: 1000, BytesAttributed: 0},
			0,
		},
		{
			"attributed cannot exceed observed",
			ConnectionCoverage{BytesObserved: 1000, BytesAttributed: 4000},
			1,
		},
	}
	for _, tt := range tests {
		if got := tt.coverage.ConnectionAttributionRatio(); got != tt.want {
			t.Errorf("%s: ratio = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// The report is the agent/server wire format; a field renamed on one side and
// not the other is a silent data loss, so the encoding is pinned here.
func TestConnectionReportJSONRoundTrip(t *testing.T) {
	report := ConnectionReport{
		NodeName:    "edge-1",
		Sequence:    42,
		AgentBootID: "boot-abc",
		WindowStart: "2026-07-26T10:00:00.000Z",
		WindowEnd:   "2026-07-26T10:05:00.000Z",
		ReportedAt:  "2026-07-26T10:05:01.000Z",
		Coverage: ConnectionCoverage{
			ConnectionsObserved:     120,
			ConnectionsAttributed:   100,
			ConnectionsUnattributed: 18,
			ConnectionsOrphaned:     2,
			StreamResets:            1,
			DroppedBuckets:          0,
			BytesObserved:           5000,
			BytesAttributed:         4800,
		},
		Buckets: []ConnectionBucket{{
			BucketStart:       "2026-07-26T10:00:00.000Z",
			AuthName:          "alice",
			SourceIP:          "198.51.100.4",
			TargetHost:        "example.com",
			TargetPort:        443,
			Network:           "tcp",
			IPVersion:         4,
			Protocol:          "tls",
			Inbound:           "vless-in",
			InboundType:       "vless",
			Outbound:          "direct",
			OutboundType:      "direct",
			Chain:             []string{"vless-in", "direct"},
			ConnectionsOpened: 5,
			ConnectionsClosed: 4,
			UplinkBytes:       1200,
			DownlinkBytes:     3600,
			DurationMsTotal:   88000,
			WindowStart:       "2026-07-26T10:00:10.000Z",
			WindowEnd:         "2026-07-26T10:04:50.000Z",
		}},
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ConnectionReport
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Sequence != report.Sequence || decoded.AgentBootID != report.AgentBootID {
		t.Fatalf("idempotency key lost in transit: %+v", decoded)
	}
	if decoded.Coverage != report.Coverage {
		t.Fatalf("coverage = %+v, want %+v", decoded.Coverage, report.Coverage)
	}
	if len(decoded.Buckets) != 1 || decoded.Buckets[0].DimensionKey() != report.Buckets[0].DimensionKey() {
		t.Fatalf("bucket did not survive the round trip: %+v", decoded.Buckets)
	}

	for _, field := range []string{
		`"agent_boot_id"`, `"sequence"`, `"coverage"`, `"connections_observed"`,
		`"bytes_attributed"`, `"bucket_start"`, `"auth_name"`, `"target_host"`,
		`"uplink_bytes"`, `"downlink_bytes"`, `"duration_ms_total"`,
		`"connections_opened"`, `"connections_closed"`,
	} {
		if !strings.Contains(string(encoded), field) {
			t.Errorf("encoded report is missing %s", field)
		}
	}
	// Fields 14/15 of the proto are never populated server-side; nothing may
	// name them on the wire either.
	if strings.Contains(string(encoded), `"uplink"`) || strings.Contains(string(encoded), `"downlink"`) {
		t.Error("wire format exposes the never-populated uplink/downlink proto fields")
	}
}
