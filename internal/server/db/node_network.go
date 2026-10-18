package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/model"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

var ErrNetworkRevision = errors.New("network snapshot revision conflict")

type ObservedIP struct {
	Address    string `json:"address"`
	Family     int    `json:"family"`
	Source     string `json:"source"`
	ObservedAt string `json:"observed_at"`
	Stale      bool   `json:"stale"`
}

type NodeNetworkSummary struct {
	Revision     int64        `json:"revision"`
	AddressCount int64        `json:"address_count"`
	CollectedAt  string       `json:"collected_at"`
	ReceivedAt   string       `json:"received_at"`
	AttemptedAt  string       `json:"attempted_at"`
	Error        string       `json:"error"`
	Stale        bool         `json:"stale"`
	ObservedIPs  []ObservedIP `json:"observed_ips"`
}

type NodeNetwork struct {
	NodeNetworkSummary
	Addresses []model.NodeAddress `json:"addresses"`
}

func networkStale(value string, age time.Duration) bool {
	t, err := time.Parse(time.RFC3339Nano, value)
	return err != nil || time.Since(t) > age
}

// NodeNetworkSummaries performs two bounded batched reads, never one read per
// address or node. Local inventory data is fetched only by the detail endpoint.
func (db *DB) NodeNetworkSummaries(ctx context.Context, ids []string) (map[string]NodeNetworkSummary, error) {
	out := make(map[string]NodeNetworkSummary, len(ids))
	for _, id := range ids {
		out[id] = NodeNetworkSummary{Stale: true, ObservedIPs: []ObservedIP{}}
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := db.q.ListNodeNetworkSummaries(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.NodeID] = NodeNetworkSummary{Revision: r.Revision, AddressCount: r.AddressCount, CollectedAt: r.CollectedAt, ReceivedAt: r.ReceivedAt, AttemptedAt: r.AttemptedAt, Error: r.Error, Stale: networkStale(r.ReceivedAt, 10*time.Minute), ObservedIPs: []ObservedIP{}}
	}
	ips, err := db.q.ListNodeObservedIPs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		s := out[ip.NodeID]
		s.ObservedIPs = append(s.ObservedIPs, ObservedIP{Address: ip.Address, Family: int(ip.Family), Source: ip.Source, ObservedAt: ip.ObservedAt, Stale: networkStale(ip.ObservedAt, 3*time.Minute)})
		out[ip.NodeID] = s
	}
	return out, nil
}

func (db *DB) NodeNetwork(ctx context.Context, nodeName string) (NodeNetwork, error) {
	n, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return NodeNetwork{}, err
	}
	// Keep summary and successful address snapshot from one database transaction.
	var result NodeNetwork
	err = db.atomic(ctx, func(tx *DB) error {
		s, err := tx.NodeNetworkSummaries(ctx, []string{n.ID})
		if err != nil {
			return err
		}
		result = NodeNetwork{NodeNetworkSummary: s[n.ID], Addresses: []model.NodeAddress{}}
		r, err := tx.q.GetNodeNetworkSnapshot(ctx, n.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return json.Unmarshal([]byte(r.AddressesJson), &result.Addresses)
	})
	return result, err
}

func normalizeNetworkSnapshot(report model.NetworkSnapshot) (model.NetworkSnapshot, error) {
	if report.Revision <= 0 || report.Revision == int64(^uint64(0)>>1) {
		return report, fmt.Errorf("invalid network revision")
	}
	if _, err := time.Parse(time.RFC3339Nano, report.CollectedAt); err != nil {
		return report, fmt.Errorf("invalid collection time")
	}
	if len(report.Error) > 1024 || len(report.Addresses) > model.MaxNodeAddresses {
		return report, fmt.Errorf("network snapshot exceeds limits")
	}
	if report.Complete && report.Error != "" {
		return report, fmt.Errorf("complete snapshot cannot contain a collection error")
	}
	if !report.Complete {
		if strings.TrimSpace(report.Error) == "" {
			return report, fmt.Errorf("incomplete snapshot requires an error")
		}
		report.Addresses = []model.NodeAddress{}
		return report, nil
	}
	seen := map[string]bool{}
	clean := make([]model.NodeAddress, 0, len(report.Addresses))
	for _, a := range report.Addresses {
		ip, err := netip.ParseAddr(a.Address)
		if err != nil || ip.Zone() != "" || ip.Is4In6() || a.PrefixLength < 0 || a.PrefixLength > ip.BitLen() || len(a.Interface) == 0 || len(a.Interface) > 128 || a.InterfaceIndex <= 0 || len(a.Scope) > 32 {
			return report, fmt.Errorf("invalid local address")
		}
		a.Address = ip.String()
		a.Family = 6
		if ip.Is4() {
			a.Family = 4
		}
		key := fmt.Sprintf("%d/%s/%s/%d", a.InterfaceIndex, a.Interface, a.Address, a.PrefixLength)
		if seen[key] {
			return report, fmt.Errorf("duplicate local address")
		}
		seen[key] = true
		clean = append(clean, a)
	}
	sort.Slice(clean, func(i, j int) bool {
		a, b := clean[i], clean[j]
		if a.InterfaceIndex != b.InterfaceIndex {
			return a.InterfaceIndex < b.InterfaceIndex
		}
		if a.Address != b.Address {
			return a.Address < b.Address
		}
		return a.PrefixLength < b.PrefixLength
	})
	report.Addresses = clean
	return report, nil
}

func (db *DB) RecordNetworkSnapshot(ctx context.Context, nodeName string, report model.NetworkSnapshot) error {
	report, err := normalizeNetworkSnapshot(report)
	if err != nil {
		return err
	}
	n, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(report)
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	addresses, _ := json.Marshal(report.Addresses)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return db.withTx(ctx, func(q *store.Queries) error {
		old, err := q.GetNodeNetworkSnapshot(ctx, n.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if report.Revision == old.Revision && hash == old.ContentHash {
				return nil
			}
			if report.Revision <= old.Revision {
				return ErrNetworkRevision
			}
		}
		collected, received := report.CollectedAt, now
		if !report.Complete {
			collected = ""
			received = ""
		}
		return q.UpsertNodeNetworkSnapshot(ctx, store.UpsertNodeNetworkSnapshotParams{NodeID: n.ID, Revision: report.Revision, ContentHash: hash, AddressesJson: string(addresses), CollectedAt: collected, ReceivedAt: received, AttemptedAt: now, Error: report.Error})
	})
}

func (db *DB) RecordObservedIP(ctx context.Context, nodeName string, ip ObservedIP) error {
	addr, err := netip.ParseAddr(ip.Address)
	if err != nil || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.Zone() != "" {
		return fmt.Errorf("invalid observed IP")
	}
	addr = addr.Unmap()
	// Pseudo IPv4 is not a real public observation; CGNAT is also not public.
	if netip.MustParsePrefix("240.0.0.0/4").Contains(addr) || netip.MustParsePrefix("100.64.0.0/10").Contains(addr) {
		return fmt.Errorf("non-public observed IP")
	}
	if ip.Source != "direct" && ip.Source != "cloudflare" {
		return fmt.Errorf("invalid observation source")
	}
	n, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return err
	}
	family := int64(6)
	if addr.Is4() {
		family = 4
	}
	return db.withTx(ctx, func(q *store.Queries) error {
		if err := q.UpsertNodeObservedIP(ctx, store.UpsertNodeObservedIPParams{NodeID: n.ID, Family: family, Address: addr.String(), Source: ip.Source, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
			return err
		}
		return q.PruneNodeObservedIPs(ctx, store.PruneNodeObservedIPsParams{NodeID: n.ID, Cutoff: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano)})
	})
}

// Preserve heartbeat and the server-owned observation in the same transaction.
func (db *DB) RecordHeartbeatWithObservedIP(ctx context.Context, heartbeat Heartbeat, ip *ObservedIP) error {
	return db.atomic(ctx, func(tx *DB) error {
		if err := tx.RecordHeartbeat(ctx, heartbeat); err != nil {
			return err
		}
		if ip != nil {
			return tx.RecordObservedIP(ctx, heartbeat.NodeName, *ip)
		}
		return nil
	})
}
