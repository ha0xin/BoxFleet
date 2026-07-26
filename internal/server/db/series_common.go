package db

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Bucket is the granularity of a server-side time series. The server owns
// bucketing and zero-fill end to end: the SQL grouping expression, the Go
// bucket walk, and the key both produce all live in this file so a bucket can
// never mean one thing in SQL and another in Go.
type Bucket string

const (
	BucketHour Bucket = "hour"
	BucketDay  Bucket = "day"
)

const (
	// Hour buckets align to UTC in all but the :30/:45 zones and the client
	// formats the labels locally, so an 8-day ceiling keeps the scan bounded
	// without an offset. Day boundaries visibly disagree with local time, so
	// day buckets shift by an explicit offset instead.
	bucketHourMaxSpan = 8 * 24 * time.Hour
	bucketDayMaxSpan  = 400 * 24 * time.Hour
	// Spans up to two days read naturally as hours; anything longer defaults
	// to days rather than handing back hundreds of bars.
	bucketHourDerivationSpan = 48 * time.Hour
	// Real UTC offsets run from -12:00 to +14:00.
	bucketOffsetMinutesMin = -12 * 60
	bucketOffsetMinutesMax = 14 * 60
	// Guard against a facade caller that skipped the API-level span clamp.
	// The largest legitimate series is 400 day buckets.
	bucketStartsHardCap = 1024
)

// bucketKeyLayout is the exact shape both bucketExpr and BucketKey emit.
// Zero-fill matches rows to buckets by this string, so the two must not drift.
const bucketKeyLayout = "2006-01-02T15:04:05Z"

// ParseBucket validates an operator-supplied granularity. An empty value
// derives one from the requested span.
func ParseBucket(value string, span time.Duration) (Bucket, error) {
	switch Bucket(strings.ToLower(strings.TrimSpace(value))) {
	case "":
		if span <= bucketHourDerivationSpan {
			return BucketHour, nil
		}
		return BucketDay, nil
	case BucketHour:
		return BucketHour, nil
	case BucketDay:
		return BucketDay, nil
	default:
		return "", errors.New("bucket must be hour or day")
	}
}

// MaxSpan is the widest window this granularity may cover in one request.
func (b Bucket) MaxSpan() time.Duration {
	if b == BucketDay {
		return bucketDayMaxSpan
	}
	return bucketHourMaxSpan
}

// MaxSpanDays renders MaxSpan for error messages.
func (b Bucket) MaxSpanDays() int {
	return int(b.MaxSpan() / (24 * time.Hour))
}

// ValidateBucketOffsetMinutes bounds the day-bucket offset to real UTC offsets.
func ValidateBucketOffsetMinutes(offset int) error {
	if offset < bucketOffsetMinutesMin || offset > bucketOffsetMinutesMax {
		return fmt.Errorf("offset_minutes must be between %d and %d", bucketOffsetMinutesMin, bucketOffsetMinutesMax)
	}
	return nil
}

// bucketExpr renders the SQL expression that maps a stored RFC3339Nano column
// to its bucket key. Both forms slice the column to a fixed width first:
// stored values carry up to nine fractional digits and SQLite's date parser is
// only reliably specified to three, so nothing is ever handed a value it might
// reject. column is always a literal from this package, never operator input.
func bucketExpr(column string, bucket Bucket, offsetMinutes int) string {
	if bucket != BucketDay {
		return "substr(" + column + ", 1, 13) || ':00:00Z'"
	}
	// Shift into local time, take the local date, then shift back so the key is
	// the UTC instant of local midnight.
	forward := signedMinutes(offsetMinutes)
	backward := signedMinutes(-offsetMinutes)
	return "strftime('%Y-%m-%dT%H:%M:%SZ', datetime(date(datetime(substr(" +
		column + ", 1, 19), '" + forward + " minutes')), '" + backward + " minutes'))"
}

func signedMinutes(minutes int) string {
	if minutes >= 0 {
		return "+" + strconv.Itoa(minutes)
	}
	return strconv.Itoa(minutes)
}

// BucketStart truncates an instant to the start of the bucket containing it.
func BucketStart(at time.Time, bucket Bucket, offsetMinutes int) time.Time {
	at = at.UTC()
	if bucket != BucketDay {
		return at.Truncate(time.Hour)
	}
	offset := time.Duration(offsetMinutes) * time.Minute
	local := at.Add(offset)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	return midnight.Add(-offset)
}

// BucketKey renders a bucket start the way bucketExpr renders it in SQL.
func BucketKey(bucketStart time.Time) string {
	return bucketStart.UTC().Format(bucketKeyLayout)
}

// ParseBucketKey reads a key produced by bucketExpr back into an instant.
func ParseBucketKey(value string) (time.Time, error) {
	return time.Parse(bucketKeyLayout, value)
}

// BucketStarts lists every bucket start in [start, end], oldest first. Both
// ends are inclusive because the event filters are: a row whose window
// straddles end still belongs to end's bucket.
func BucketStarts(start, end time.Time, bucket Bucket, offsetMinutes int) []time.Time {
	if end.Before(start) {
		return nil
	}
	step := time.Hour
	if bucket == BucketDay {
		step = 24 * time.Hour
	}
	last := BucketStart(end, bucket, offsetMinutes)
	starts := make([]time.Time, 0, 32)
	for cursor := BucketStart(start, bucket, offsetMinutes); !cursor.After(last); cursor = cursor.Add(step) {
		starts = append(starts, cursor)
		if len(starts) >= bucketStartsHardCap {
			break
		}
	}
	return starts
}

// ZeroFillSeries expands a sparse GROUP BY result into one entry per bucket in
// [start, end], oldest first. present is keyed by the bucket key SQL emitted;
// zero builds the entry for a bucket with no rows.
func ZeroFillSeries[T any](
	start, end time.Time,
	bucket Bucket,
	offsetMinutes int,
	present map[string]T,
	zero func(bucketStart time.Time) T,
) []T {
	starts := BucketStarts(start, end, bucket, offsetMinutes)
	points := make([]T, 0, len(starts))
	for _, bucketStart := range starts {
		if value, ok := present[BucketKey(bucketStart)]; ok {
			points = append(points, value)
			continue
		}
		points = append(points, zero(bucketStart))
	}
	return points
}
