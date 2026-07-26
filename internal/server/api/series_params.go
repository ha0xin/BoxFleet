package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/server/db"
)

// seriesParams is the shared time-window contract for every bucketed telemetry
// endpoint. Clients never bucket, so the window, the granularity and the
// day-bucket offset are validated once here and the facade is handed a range
// it can zero-fill without re-checking.
type seriesParams struct {
	Start         time.Time
	End           time.Time
	Bucket        db.Bucket
	OffsetMinutes int
}

// StartRFC3339 and EndRFC3339 render the window the way the facade filters
// compare it: UTC RFC3339Nano, matching how the columns are stored.
func (p seriesParams) StartRFC3339() string {
	return p.Start.Format(time.RFC3339Nano)
}

func (p seriesParams) EndRFC3339() string {
	return p.End.Format(time.RFC3339Nano)
}

// parseSeriesParams reads start, end, bucket and offset_minutes. On invalid
// input it writes the 422 and reports false; the caller returns immediately.
func parseSeriesParams(w http.ResponseWriter, r *http.Request) (seriesParams, bool) {
	start, ok := queryRequiredTime(w, r, "start")
	if !ok {
		return seriesParams{}, false
	}
	end, ok := queryRequiredTime(w, r, "end")
	if !ok {
		return seriesParams{}, false
	}
	if !end.After(start) {
		writeAdminError(w, errors.New("end must be after start"))
		return seriesParams{}, false
	}
	offsetMinutes, ok := queryOffsetMinutes(w, r)
	if !ok {
		return seriesParams{}, false
	}
	span := end.Sub(start)
	bucket, err := db.ParseBucket(r.URL.Query().Get("bucket"), span)
	if err != nil {
		writeAdminError(w, err)
		return seriesParams{}, false
	}
	// traffic_usage_deltas and log_events are both append-only and unbounded in
	// the traffic case, so the span ceiling is what keeps the scan finite.
	if span > bucket.MaxSpan() {
		writeAdminError(w, fmt.Errorf("%s buckets cover at most %d days per request", bucket, bucket.MaxSpanDays()))
		return seriesParams{}, false
	}
	return seriesParams{
		Start:         start,
		End:           end,
		Bucket:        bucket,
		OffsetMinutes: offsetMinutes,
	}, true
}

func queryRequiredTime(w http.ResponseWriter, r *http.Request, name string) (time.Time, bool) {
	raw, ok := queryOptionalTime(w, r, name)
	if !ok {
		return time.Time{}, false
	}
	if raw == "" {
		writeAdminError(w, errors.New(name+" is required"))
		return time.Time{}, false
	}
	// queryOptionalTime already parsed and normalized to UTC RFC3339Nano.
	parsed, _ := time.Parse(time.RFC3339Nano, raw)
	return parsed, true
}

func queryOffsetMinutes(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("offset_minutes"))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		writeAdminError(w, errors.New("offset_minutes must be an integer"))
		return 0, false
	}
	if err := db.ValidateBucketOffsetMinutes(value); err != nil {
		writeAdminError(w, err)
		return 0, false
	}
	return value, true
}

// queryBoundedLimit is queryLimit with a per-endpoint ceiling; aggregation
// endpoints cap far below the shared 500 of the row-listing endpoints.
func queryBoundedLimit(r *http.Request, fallback, max int64) int64 {
	value := queryLimit(r, fallback)
	if value > max {
		return max
	}
	return value
}

// queryGroup reads the grouping dimension, rejecting anything the endpoint
// does not aggregate by instead of silently falling back.
func queryGroup(w http.ResponseWriter, r *http.Request, fallback string, allowed ...string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("group")))
	if value == "" {
		return fallback, true
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, true
		}
	}
	writeAdminError(w, fmt.Errorf("group must be one of %s", strings.Join(allowed, ", ")))
	return "", false
}
