package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/haoxin/boxfleet/internal/id"
	"github.com/haoxin/boxfleet/internal/model"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
	"github.com/mattn/go-sqlite3"
)

type TrafficReport = model.TrafficReport
type TrafficDelta = model.TrafficDelta

type TrafficSummary struct {
	UserName      string
	Direction     string
	RawBytes      int64
	BillableBytes int64
}

func (db *DB) RecordTrafficReport(ctx context.Context, report TrafficReport) error {
	node, err := db.GetNode(ctx, report.NodeName)
	if err != nil {
		return err
	}
	reportedAt := report.ReportedAt
	if reportedAt == "" {
		reportedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	reportID, err := id.New("tr")
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(q *store.Queries) error {
		if err := q.CreateTrafficReport(ctx, store.CreateTrafficReportParams{
			ID:          reportID,
			NodeID:      node.ID,
			Sequence:    report.Sequence,
			AgentBootID: report.AgentBootID,
			ReportedAt:  reportedAt,
		}); err != nil {
			if isSQLiteUniqueConstraint(err) {
				if _, existingErr := q.GetTrafficReportBySequence(ctx, store.GetTrafficReportBySequenceParams{
					NodeID:      node.ID,
					AgentBootID: report.AgentBootID,
					Sequence:    report.Sequence,
				}); existingErr == nil {
					return nil
				}
			}
			return err
		}
		for _, delta := range report.Deltas {
			if delta.RawBytesDelta <= 0 {
				continue
			}
			if delta.Direction != "uplink" && delta.Direction != "downlink" {
				return fmt.Errorf("unsupported traffic direction %q", delta.Direction)
			}
			credential, err := q.GetTrafficCredentialByNodeAuthName(ctx, store.GetTrafficCredentialByNodeAuthNameParams{
				NodeName: node.Name,
				AuthName: delta.AuthName,
			})
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
			observedAt := delta.ObservedAt
			if observedAt == "" {
				observedAt = reportedAt
			}
			deltaID, err := id.New("td")
			if err != nil {
				return err
			}
			billable := int64(math.Ceil(float64(delta.RawBytesDelta) * credential.EffectiveMultiplier))
			if err := q.CreateTrafficUsageDelta(ctx, store.CreateTrafficUsageDeltaParams{
				ID:                  deltaID,
				ReportID:            reportID,
				NodeID:              node.ID,
				ProxyUserID:         credential.ProxyUserID,
				ProxyID:             sql.NullString{String: credential.ProxyID, Valid: true},
				AuthName:            delta.AuthName,
				Direction:           delta.Direction,
				RawBytesDelta:       delta.RawBytesDelta,
				EffectiveMultiplier: credential.EffectiveMultiplier,
				BillableBytesDelta:  billable,
				CounterValue:        delta.CounterValue,
				CounterEpoch:        delta.CounterEpoch,
				ObservedAt:          observedAt,
			}); err != nil {
				return err
			}
			if err := q.UpsertTrafficUsageTotal(ctx, store.UpsertTrafficUsageTotalParams{
				ProxyUserID:        credential.ProxyUserID,
				Direction:          delta.Direction,
				RawBytesDelta:      delta.RawBytesDelta,
				BillableBytesDelta: billable,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func isSQLiteUniqueConstraint(err error) bool {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique || sqliteErr.Code == sqlite3.ErrConstraint
	}
	return false
}

func (db *DB) SumTrafficByUser(ctx context.Context, userName string) ([]TrafficSummary, error) {
	rows, err := db.q.SumTrafficByUser(ctx, normalizeName(userName))
	if err != nil {
		return nil, err
	}
	out := make([]TrafficSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, TrafficSummary{
			UserName:      row.UserName,
			Direction:     row.Direction,
			RawBytes:      row.RawBytes,
			BillableBytes: row.BillableBytes,
		})
	}
	return out, nil
}

func (db *DB) SumTrafficByAllUsers(ctx context.Context) ([]TrafficSummary, error) {
	rows, err := db.q.SumTrafficByAllUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TrafficSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, TrafficSummary{
			UserName:      row.UserName,
			Direction:     row.Direction,
			RawBytes:      row.RawBytes,
			BillableBytes: row.BillableBytes,
		})
	}
	return out, nil
}
