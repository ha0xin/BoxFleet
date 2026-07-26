package db

import (
	"context"
	"testing"
	"time"
)

func TestParseBucketDerivesGranularityFromSpan(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		span    time.Duration
		want    Bucket
		wantErr bool
	}{
		{name: "empty derives hour for a day", span: 24 * time.Hour, want: BucketHour},
		{name: "empty derives hour at the boundary", span: 48 * time.Hour, want: BucketHour},
		{name: "empty derives day past the boundary", span: 49 * time.Hour, want: BucketDay},
		{name: "explicit hour wins over a long span", value: "hour", span: 30 * 24 * time.Hour, want: BucketHour},
		{name: "explicit day wins over a short span", value: "day", span: time.Hour, want: BucketDay},
		{name: "case and padding are tolerated", value: " Day ", span: time.Hour, want: BucketDay},
		{name: "minute is rejected", value: "minute", span: time.Hour, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBucket(tt.value, tt.span)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseBucket(%q) = %q, want error", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("ParseBucket(%q, %s) = %q, want %q", tt.value, tt.span, got, tt.want)
			}
		})
	}
}

func TestBucketMaxSpanBoundsTheScan(t *testing.T) {
	if got := BucketHour.MaxSpan(); got != 8*24*time.Hour {
		t.Fatalf("hour max span = %s, want 192h", got)
	}
	if got := BucketDay.MaxSpan(); got != 400*24*time.Hour {
		t.Fatalf("day max span = %s, want 9600h", got)
	}
	if got := BucketHour.MaxSpanDays(); got != 8 {
		t.Fatalf("hour max span days = %d, want 8", got)
	}
	if got := BucketDay.MaxSpanDays(); got != 400 {
		t.Fatalf("day max span days = %d, want 400", got)
	}
}

func TestValidateBucketOffsetMinutesRejectsImpossibleZones(t *testing.T) {
	for _, offset := range []int{0, -720, 840, 330, -480} {
		if err := ValidateBucketOffsetMinutes(offset); err != nil {
			t.Fatalf("offset %d rejected: %v", offset, err)
		}
	}
	for _, offset := range []int{-721, 841, 100000} {
		if err := ValidateBucketOffsetMinutes(offset); err == nil {
			t.Fatalf("offset %d accepted", offset)
		}
	}
}

// The zero-fill join is a string match between the key SQL emits and the key
// Go emits. If the two ever disagree every bucket silently reads as empty, so
// pin them against each other over real SQLite.
func TestBucketExprMatchesGoBucketStarts(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	observed := []string{
		"2026-07-26T00:00:00Z",
		"2026-07-26T00:00:00.000000001Z",
		"2026-07-26T09:58:11.221Z",
		"2026-07-26T23:59:59.999999999Z",
		"2026-01-01T00:30:00.5Z",
		"2026-12-31T23:00:00Z",
	}
	offsets := []int{0, -480, 330, 840, -720}

	for _, bucket := range []Bucket{BucketHour, BucketDay} {
		for _, offset := range offsets {
			query := "SELECT " + bucketExpr("?", bucket, offset)
			for _, value := range observed {
				var got string
				if err := store.sql.QueryRowContext(ctx, query, value).Scan(&got); err != nil {
					t.Fatalf("%s bucket at offset %d for %s: %v", bucket, offset, value, err)
				}
				at, err := time.Parse(time.RFC3339Nano, value)
				if err != nil {
					t.Fatal(err)
				}
				want := BucketKey(BucketStart(at, bucket, offset))
				if got != want {
					t.Fatalf("%s bucket at offset %d for %s = %q, want %q", bucket, offset, value, got, want)
				}
				parsed, err := ParseBucketKey(got)
				if err != nil {
					t.Fatalf("ParseBucketKey(%q): %v", got, err)
				}
				if !parsed.Equal(BucketStart(at, bucket, offset)) {
					t.Fatalf("ParseBucketKey(%q) = %s, want %s", got, parsed, BucketStart(at, bucket, offset))
				}
			}
		}
	}
}

func TestBucketStartsCoverBothEndsInclusive(t *testing.T) {
	start := time.Date(2026, 7, 26, 9, 58, 0, 0, time.UTC)
	end := time.Date(2026, 7, 26, 12, 3, 0, 0, time.UTC)
	hours := BucketStarts(start, end, BucketHour, 0)
	want := []string{
		"2026-07-26T09:00:00Z",
		"2026-07-26T10:00:00Z",
		"2026-07-26T11:00:00Z",
		"2026-07-26T12:00:00Z",
	}
	if len(hours) != len(want) {
		t.Fatalf("hour buckets = %d, want %d", len(hours), len(want))
	}
	for i, bucketStart := range hours {
		if got := BucketKey(bucketStart); got != want[i] {
			t.Fatalf("hour bucket %d = %q, want %q", i, got, want[i])
		}
	}

	// Local midnight at UTC+8 is 16:00Z the previous day, so a window that
	// starts after 16:00Z already belongs to the next local day.
	days := BucketStarts(
		time.Date(2026, 7, 25, 17, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC),
		BucketDay,
		480,
	)
	wantDays := []string{
		"2026-07-25T16:00:00Z",
		"2026-07-26T16:00:00Z",
	}
	if len(days) != len(wantDays) {
		t.Fatalf("day buckets = %d, want %d: %v", len(days), len(wantDays), days)
	}
	for i, bucketStart := range days {
		if got := BucketKey(bucketStart); got != wantDays[i] {
			t.Fatalf("day bucket %d = %q, want %q", i, got, wantDays[i])
		}
	}

	if got := BucketStarts(end, start, BucketHour, 0); got != nil {
		t.Fatalf("reversed window returned %v, want nil", got)
	}
}

func TestZeroFillSeriesKeepsPresentBucketsAndOrders(t *testing.T) {
	start := time.Date(2026, 7, 26, 0, 30, 0, 0, time.UTC)
	end := time.Date(2026, 7, 26, 3, 0, 0, 0, time.UTC)
	present := map[string]int64{
		"2026-07-26T01:00:00Z": 7,
		"2026-07-26T03:00:00Z": 11,
		// A key outside the window must never leak into the result.
		"2026-07-27T00:00:00Z": 99,
	}
	got := ZeroFillSeries(start, end, BucketHour, 0, present, func(time.Time) int64 { return 0 })
	want := []int64{0, 7, 0, 11}
	if len(got) != len(want) {
		t.Fatalf("points = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("point %d = %d, want %d", i, got[i], want[i])
		}
	}

	seen := make([]string, 0, 4)
	ZeroFillSeries(start, end, BucketHour, 0, map[string]int64{}, func(bucketStart time.Time) int64 {
		seen = append(seen, BucketKey(bucketStart))
		return 0
	})
	wantSeen := []string{
		"2026-07-26T00:00:00Z",
		"2026-07-26T01:00:00Z",
		"2026-07-26T02:00:00Z",
		"2026-07-26T03:00:00Z",
	}
	for i, key := range wantSeen {
		if seen[i] != key {
			t.Fatalf("zero-filled bucket %d = %q, want %q", i, seen[i], key)
		}
	}
}
