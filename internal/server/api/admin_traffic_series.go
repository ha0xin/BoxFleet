package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/server/db"
)

const (
	adminTrafficSeriesDefaultLimit = 25
	adminTrafficSeriesMaxLimit     = 100
)

type adminTrafficSeriesResponse struct {
	Bucket        string               `json:"bucket"`
	OffsetMinutes int                  `json:"offset_minutes"`
	Start         string               `json:"start"`
	End           string               `json:"end"`
	Group         string               `json:"group"`
	Series        []adminTrafficSeries `json:"series"`
	Truncated     bool                 `json:"truncated"`
}

type adminTrafficSeries struct {
	Key    string               `json:"key"`
	Label  string               `json:"label"`
	Points []adminTrafficPoint  `json:"points"`
	Totals adminTrafficDirected `json:"totals"`
}

type adminTrafficPoint struct {
	BucketStart string `json:"bucket_start"`
	adminTrafficDirected
}

// adminTrafficDirected keeps both directions on one object so a stacked chart
// reads a bucket without joining two series.
type adminTrafficDirected struct {
	UplinkRawBytes        int64 `json:"uplink_raw_bytes"`
	UplinkBillableBytes   int64 `json:"uplink_billable_bytes"`
	DownlinkRawBytes      int64 `json:"downlink_raw_bytes"`
	DownlinkBillableBytes int64 `json:"downlink_billable_bytes"`
}

func adminTrafficSeriesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, ok := parseSeriesParams(w, r)
		if !ok {
			return
		}
		group, ok := queryGroup(
			w, r,
			string(db.TrafficSeriesGroupTotal),
			string(db.TrafficSeriesGroupTotal),
			string(db.TrafficSeriesGroupUser),
			string(db.TrafficSeriesGroupNode),
		)
		if !ok {
			return
		}
		result, err := store.TrafficSeries(r.Context(), db.TrafficSeriesFilter{
			Start:         params.Start,
			End:           params.End,
			Bucket:        params.Bucket,
			OffsetMinutes: params.OffsetMinutes,
			Group:         db.TrafficSeriesGroup(group),
			UserName:      strings.TrimSpace(r.URL.Query().Get("user")),
			NodeName:      strings.TrimSpace(r.URL.Query().Get("node")),
			Limit:         queryBoundedLimit(r, adminTrafficSeriesDefaultLimit, adminTrafficSeriesMaxLimit),
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminTrafficSeriesResponse{
			Bucket:        string(params.Bucket),
			OffsetMinutes: params.OffsetMinutes,
			Start:         params.StartRFC3339(),
			End:           params.EndRFC3339(),
			Group:         group,
			Series:        adminTrafficSeriesList(result.Series),
			Truncated:     result.Truncated,
		})
	}
}

func adminTrafficSeriesList(series []db.TrafficSeries) []adminTrafficSeries {
	out := make([]adminTrafficSeries, 0, len(series))
	for _, item := range series {
		points := make([]adminTrafficPoint, 0, len(item.Points))
		for _, point := range item.Points {
			points = append(points, adminTrafficPoint{
				BucketStart:          point.BucketStart.UTC().Format(time.RFC3339),
				adminTrafficDirected: adminTrafficDirectedFrom(point),
			})
		}
		out = append(out, adminTrafficSeries{
			Key:    item.Key,
			Label:  item.Label,
			Points: points,
			Totals: adminTrafficDirectedFrom(item.Totals),
		})
	}
	return out
}

func adminTrafficDirectedFrom(point db.TrafficPoint) adminTrafficDirected {
	return adminTrafficDirected{
		UplinkRawBytes:        point.UplinkRawBytes,
		UplinkBillableBytes:   point.UplinkBillableBytes,
		DownlinkRawBytes:      point.DownlinkRawBytes,
		DownlinkBillableBytes: point.DownlinkBillableBytes,
	}
}
