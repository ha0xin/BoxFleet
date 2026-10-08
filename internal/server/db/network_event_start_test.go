package db

import (
	"context"
	"testing"
	"time"
)

func TestSessionStartDefinesListChartAndHostWindow(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, d)
	if _, err := d.SetNodeConnectionTelemetry(ctx, SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	instant := func(h int) string { return at.Add(time.Duration(h) * time.Hour).Format(time.RFC3339Nano) }
	b := ConnectionBucket{ConnectionID: "session", StartedAt: instant(0), AuthName: "vless-39090@alice", TargetHost: "example.com", TargetPort: 443, ConnectionsOpened: 1, UplinkBytes: 100, WindowStart: instant(0), WindowEnd: instant(0)}
	if err := d.RecordConnectionReport(ctx, connectionTestReport(1, instant(0), b)); err != nil {
		t.Fatal(err)
	}
	b.UplinkBytes = 1100
	b.WindowStart, b.WindowEnd = instant(2), instant(2)
	if err := d.RecordConnectionReport(ctx, connectionTestReport(2, instant(2), b)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		start, end int
		want       int64
	}{
		{"start included", 0, 1, 1}, {"update does not move start", 2, 3, 0}, {"end excluded", -1, 0, 0}, {"whole session", 0, 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := LogEventFilter{Start: instant(tc.start), End: instant(tc.end)}
			page, err := d.ListLogEventsPage(ctx, filter)
			if err != nil {
				t.Fatal(err)
			}
			got, err := d.NetworkEventSeries(ctx, NetworkEventSeriesFilter{LogEventFilter: filter, Bucket: BucketHour})
			if err != nil {
				t.Fatal(err)
			}
			var sum int64
			for _, p := range got.Series[0].Points {
				sum += p.Count
			}
			hosts, _, err := d.NetworkEventHostCounts(ctx, filter, 10)
			if err != nil {
				t.Fatal(err)
			}
			var hostCount int64
			for _, host := range hosts {
				hostCount += host.Connections
			}
			if page.Total != tc.want || got.Series[0].Total != tc.want || sum != tc.want || hostCount != tc.want {
				t.Fatalf("want %d: rows=%d total=%d points=%d hosts=%d", tc.want, page.Total, got.Series[0].Total, sum, hostCount)
			}
			if tc.want == 1 && (page.Events[0].UplinkBytes == nil || *page.Events[0].UplinkBytes != 1100) {
				t.Fatal("session must retain cumulative lifetime bytes")
			}
		})
	}
}
