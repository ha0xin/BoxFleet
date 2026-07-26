package db

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
	"github.com/haoxin/boxfleet/internal/servicecatalog"
)

// ServiceUsageGroup selects what the host breakdown rolls up into.
type ServiceUsageGroup string

const (
	ServiceUsageGroupService  ServiceUsageGroup = "service"
	ServiceUsageGroupCategory ServiceUsageGroup = "category"
)

const (
	// One scope must not be able to pull an unbounded host list into memory.
	// Beyond this the breakdown is reported as truncated rather than silently
	// partial.
	networkEventHostScanCap = 200000

	serviceUsageDefaultLimit = 20
	serviceUsageMaxLimit     = 100
	serviceUsageOtherKey     = "other"
	serviceUsageOtherLabel   = "Other"

	hostUsageDefaultLimit = 50
	hostUsageMaxLimit     = 500
)

// HostCount is one destination host in the filtered window. Connections are
// connection counts, not bytes — log_events carries no byte columns, so no
// per-destination volume exists anywhere in the schema.
type HostCount struct {
	Host        string
	Connections int64
	LastSeen    string
}

// ServiceUsageRow is one service (or category) in the breakdown. Hosts is the
// number of distinct destination hosts that classified into this row.
type ServiceUsageRow struct {
	Key         string
	Label       string
	Category    string
	Connections int64
	Hosts       int64
}

type ServiceUsageResult struct {
	Rows             []ServiceUsageRow
	Other            ServiceUsageRow
	TotalConnections int64
	TotalHosts       int64
	Truncated        bool
	CatalogVersion   string
}

// HostUsageRow is one host with its resolved classification attached.
type HostUsageRow struct {
	Host         string
	Service      string
	ServiceLabel string
	Category     string
	Source       string
	Connections  int64
	LastSeen     string
}

type HostUsagePage struct {
	Hosts     []HostUsageRow
	Total     int64
	Limit     int64
	Offset    int64
	Truncated bool
}

// DomainServiceOverride is an operator-supplied classification rule. Suffix is
// the primary key and is stored already lowercased and dot-normalized.
type DomainServiceOverride struct {
	Suffix    string
	Service   string
	Label     string
	Category  string
	CreatedAt string
	UpdatedAt string
}

// NetworkEventHostCounts ranks destination hosts in the filtered window.
//
// lower() is mandatory: RecordLogEvents stores target_host with the casing
// sing-box logged and only the aggregate key is lowercased, so "Example.com"
// and "example.com" are distinct stored rows that must collapse here. The
// reported flag is true when more hosts existed than the limit returned.
func (db *DB) NetworkEventHostCounts(ctx context.Context, filter LogEventFilter, limit int64) ([]HostCount, bool, error) {
	scope, err := db.resolveLogEventScope(ctx, filter)
	if err != nil {
		return nil, false, err
	}
	if limit <= 0 || limit > networkEventHostScanCap {
		limit = networkEventHostScanCap
	}
	searchJoin, where, args := buildLogEventPredicates(scope)
	query := `
SELECT lower(e.target_host) AS host, SUM(e.count) AS connections, MAX(e.window_end) AS last_seen
FROM log_events e` + searchJoin + `
WHERE ` + strings.Join(where, " AND ") + `
GROUP BY host
ORDER BY connections DESC, host ASC
LIMIT ?`
	queryArgs := append(append([]any{}, args...), limit+1)
	rows, err := db.sql.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	hosts := make([]HostCount, 0, 64)
	for rows.Next() {
		var row HostCount
		if err := rows.Scan(&row.Host, &row.Connections, &row.LastSeen); err != nil {
			return nil, false, err
		}
		hosts = append(hosts, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := int64(len(hosts)) > limit
	if truncated {
		hosts = hosts[:limit]
	}
	return hosts, truncated, nil
}

// NetworkEventServiceUsage rolls the host breakdown up into services or
// categories. Classification is read-time, so an override or a dataset bump
// retroactively reclassifies history without touching log_events.
func (db *DB) NetworkEventServiceUsage(ctx context.Context, filter LogEventFilter, group ServiceUsageGroup, limit int64) (ServiceUsageResult, error) {
	if group == "" {
		group = ServiceUsageGroupService
	}
	if group != ServiceUsageGroupService && group != ServiceUsageGroupCategory {
		return ServiceUsageResult{}, fmt.Errorf("unsupported service usage group %q", group)
	}
	if limit <= 0 {
		limit = serviceUsageDefaultLimit
	}
	if limit > serviceUsageMaxLimit {
		limit = serviceUsageMaxLimit
	}
	hosts, truncated, err := db.NetworkEventHostCounts(ctx, filter, networkEventHostScanCap)
	if err != nil {
		return ServiceUsageResult{}, err
	}
	catalog, err := db.serviceCatalog(ctx)
	if err != nil {
		return ServiceUsageResult{}, err
	}

	index := make(map[string]int, 64)
	rows := make([]ServiceUsageRow, 0, 64)
	result := ServiceUsageResult{
		Truncated:      truncated,
		CatalogVersion: servicecatalog.Version(),
		TotalHosts:     int64(len(hosts)),
	}
	for _, host := range hosts {
		classified := catalog.Classify(host.Host)
		key, label := classified.Service, classified.Label
		if group == ServiceUsageGroupCategory {
			key, label = classified.Category, classified.Category
		}
		position, ok := index[key]
		if !ok {
			position = len(rows)
			index[key] = position
			rows = append(rows, ServiceUsageRow{Key: key, Label: label, Category: classified.Category})
		}
		rows[position].Connections += host.Connections
		rows[position].Hosts++
		result.TotalConnections += host.Connections
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Connections != rows[j].Connections {
			return rows[i].Connections > rows[j].Connections
		}
		return rows[i].Key < rows[j].Key
	})

	result.Other = ServiceUsageRow{Key: serviceUsageOtherKey, Label: serviceUsageOtherLabel}
	if int64(len(rows)) > limit {
		for _, row := range rows[limit:] {
			result.Other.Connections += row.Connections
			result.Other.Hosts += row.Hosts
		}
		rows = rows[:limit]
	}
	result.Rows = rows
	return result, nil
}

// NetworkEventHostUsage is the per-host drill-down behind a service row.
// Filtering by service happens after classification, so the paging is applied
// in Go rather than in SQL.
func (db *DB) NetworkEventHostUsage(ctx context.Context, filter LogEventFilter, service string, limit, offset int64) (HostUsagePage, error) {
	if limit <= 0 {
		limit = hostUsageDefaultLimit
	}
	if limit > hostUsageMaxLimit {
		limit = hostUsageMaxLimit
	}
	if offset < 0 {
		offset = 0
	}
	hosts, truncated, err := db.NetworkEventHostCounts(ctx, filter, networkEventHostScanCap)
	if err != nil {
		return HostUsagePage{}, err
	}
	catalog, err := db.serviceCatalog(ctx)
	if err != nil {
		return HostUsagePage{}, err
	}
	wanted := strings.ToLower(strings.TrimSpace(service))
	matched := make([]HostUsageRow, 0, len(hosts))
	for _, host := range hosts {
		classified := catalog.Classify(host.Host)
		if wanted != "" && strings.ToLower(classified.Service) != wanted {
			continue
		}
		matched = append(matched, HostUsageRow{
			Host:         host.Host,
			Service:      classified.Service,
			ServiceLabel: classified.Label,
			Category:     classified.Category,
			Source:       classified.Source,
			Connections:  host.Connections,
			LastSeen:     host.LastSeen,
		})
	}
	page := HostUsagePage{
		Hosts:     []HostUsageRow{},
		Total:     int64(len(matched)),
		Limit:     limit,
		Offset:    offset,
		Truncated: truncated,
	}
	if offset < page.Total {
		end := offset + limit
		if end > page.Total {
			end = page.Total
		}
		page.Hosts = matched[offset:end]
	}
	return page, nil
}

// serviceCatalog layers the operator overrides over the embedded dataset.
// WithOverrides returns the receiver untouched when there is nothing to layer,
// so this stays cheap enough to call per request.
func (db *DB) serviceCatalog(ctx context.Context) (*servicecatalog.Catalog, error) {
	overrides, err := db.ListDomainServiceOverrides(ctx)
	if err != nil {
		return nil, err
	}
	rules := make([]servicecatalog.Override, 0, len(overrides))
	for _, override := range overrides {
		rules = append(rules, servicecatalog.Override{
			Suffix:   override.Suffix,
			Service:  override.Service,
			Label:    override.Label,
			Category: override.Category,
		})
	}
	return servicecatalog.Default().WithOverrides(rules), nil
}

func (db *DB) ListDomainServiceOverrides(ctx context.Context) ([]DomainServiceOverride, error) {
	rows, err := db.q.ListDomainServiceOverrides(ctx)
	if err != nil {
		return nil, err
	}
	overrides := make([]DomainServiceOverride, 0, len(rows))
	for _, row := range rows {
		overrides = append(overrides, domainServiceOverrideFromRow(row))
	}
	return overrides, nil
}

// UpsertDomainServiceOverride writes a rule and reads it back, so the caller
// always sees the stored (normalized) suffix and the server-assigned stamps.
func (db *DB) UpsertDomainServiceOverride(ctx context.Context, override DomainServiceOverride) (DomainServiceOverride, error) {
	suffix, err := validateDomainSuffix(override.Suffix)
	if err != nil {
		return DomainServiceOverride{}, err
	}
	service := strings.TrimSpace(override.Service)
	if service == "" {
		return DomainServiceOverride{}, errors.New("service is required")
	}
	if err := db.q.UpsertDomainServiceOverride(ctx, store.UpsertDomainServiceOverrideParams{
		Suffix:   suffix,
		Service:  service,
		Label:    strings.TrimSpace(override.Label),
		Category: strings.TrimSpace(override.Category),
	}); err != nil {
		return DomainServiceOverride{}, err
	}
	row, err := db.q.GetDomainServiceOverride(ctx, suffix)
	if err != nil {
		return DomainServiceOverride{}, err
	}
	return domainServiceOverrideFromRow(row), nil
}

func (db *DB) DeleteDomainServiceOverride(ctx context.Context, suffix string) error {
	normalized, err := validateDomainSuffix(suffix)
	if err != nil {
		return err
	}
	return db.q.DeleteDomainServiceOverride(ctx, normalized)
}

func domainServiceOverrideFromRow(row store.DomainServiceOverride) DomainServiceOverride {
	return DomainServiceOverride{
		Suffix:    row.Suffix,
		Service:   row.Service,
		Label:     row.Label,
		Category:  row.Category,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// validateDomainSuffix normalizes the way servicecatalog normalizes a host, so
// ".Example.COM." and "example.com" are the same rule however an operator
// spells it.
func validateDomainSuffix(value string) (string, error) {
	suffix := strings.ToLower(strings.Trim(strings.TrimSpace(value), "."))
	if suffix == "" {
		return "", errors.New("suffix is required")
	}
	if strings.ContainsAny(suffix, "/?#@") {
		return "", fmt.Errorf("suffix %q must be a bare domain suffix", value)
	}
	for _, r := range suffix {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", fmt.Errorf("suffix %q must not contain whitespace", value)
		}
	}
	return suffix, nil
}
