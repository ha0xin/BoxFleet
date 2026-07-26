package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type ProxyUser = store.ProxyUser

type ProxyUserWithProxyCount struct {
	ProxyUser
	ProxyCount int64
}

// UserFilter mirrors the controls on the Users page. Status is matched against
// the derived status below, not against the stored column, so the operator's
// filter and the badge on the row always mean the same thing.
type UserFilter struct {
	Search    string
	Status    string
	Deleted   string
	Sort      string
	Direction string
	Limit     int64
	Offset    int64
}

// ProxyUserPageRow carries everything one row of the paged inventory renders.
// Quota state depends on traffic, so the traffic rollup is read by the same
// query that pages: fetching it separately would mean a second full-table
// request per page, which is what server-side pagination exists to avoid.
type ProxyUserPageRow struct {
	ProxyUser
	ProxyCount            int64
	EffectiveStatus       string
	UplinkRawBytes        int64
	UplinkBillableBytes   int64
	DownlinkRawBytes      int64
	DownlinkBillableBytes int64
}

type UserPage struct {
	Users  []ProxyUserPageRow
	Total  int64
	Limit  int64
	Offset int64
}

// userBillableBytesSQL totals billable traffic for one user. traffic_usage_totals
// is keyed by (proxy_user_id, direction), so this is a two-row primary-key lookup
// per user rather than a scan of the delta history.
const userBillableBytesSQL = `(SELECT COALESCE(SUM(t.billable_bytes), 0) FROM traffic_usage_totals t WHERE t.proxy_user_id = u.id)`

const userProxyCountSQL = `(SELECT COUNT(*) FROM proxy_accesses a WHERE a.proxy_user_id = u.id AND a.deleted_at IS NULL)`

// userEffectiveStatusSQL derives the status the Users page badges, in the same
// precedence: deleted, disabled, over quota, expired, then active. Quota
// exhaustion and expiry are states the stored column only learns about later, so
// deriving them here keeps a filtered page from disagreeing with its own rows.
// expire_at is stored as second-precision RFC3339 UTC (normalizeExpireAt), which
// this strftime format matches exactly, so the comparison stays a text compare.
const userEffectiveStatusSQL = `CASE
    WHEN u.deleted_at IS NOT NULL THEN 'deleted'
    WHEN u.status = 'disabled' THEN 'disabled'
    WHEN u.status = 'quota_exceeded'
      OR (u.global_quota_bytes > 0 AND ` + userBillableBytesSQL + ` >= u.global_quota_bytes) THEN 'quota_exceeded'
    WHEN u.status = 'expired'
      OR (u.expire_at IS NOT NULL AND u.expire_at <> ''
        AND u.expire_at <= strftime('%Y-%m-%dT%H:%M:%SZ', 'now')) THEN 'expired'
    ELSE 'active'
  END`

type CreateProxyUserParams struct {
	Name             string
	DisplayName      string
	GlobalQuotaBytes int64
	ExpireAt         string
}

func (db *DB) CreateProxyUser(ctx context.Context, params CreateProxyUserParams) (ProxyUser, error) {
	name := normalizeName(params.Name)
	if name == "" {
		return ProxyUser{}, errors.New("user name is required")
	}
	if err := validateNameForAuth(name, "user"); err != nil {
		return ProxyUser{}, err
	}
	expireAt, err := normalizeExpireAt(params.ExpireAt)
	if err != nil {
		return ProxyUser{}, err
	}
	userID, err := id.New("usr")
	if err != nil {
		return ProxyUser{}, err
	}
	err = db.q.CreateProxyUser(ctx, store.CreateProxyUserParams{
		ID:               userID,
		Name:             name,
		DisplayName:      params.DisplayName,
		GlobalQuotaBytes: params.GlobalQuotaBytes,
		ExpireAt:         expireAt,
	})
	if err != nil {
		return ProxyUser{}, err
	}
	return db.GetProxyUser(ctx, name)
}

func (db *DB) ListProxyUsers(ctx context.Context) ([]ProxyUser, error) {
	return db.q.ListProxyUsers(ctx)
}

func (db *DB) ListProxyUsersWithProxyCounts(ctx context.Context) ([]ProxyUserWithProxyCount, error) {
	rows, err := db.q.ListProxyUsersWithProxyCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProxyUserWithProxyCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, ProxyUserWithProxyCount{
			ProxyUser: ProxyUser{
				ID:               row.ID,
				Name:             row.Name,
				DisplayName:      row.DisplayName,
				Status:           row.Status,
				GlobalQuotaBytes: row.GlobalQuotaBytes,
				ExpireAt:         row.ExpireAt,
				DeletedAt:        row.DeletedAt,
				CreatedAt:        row.CreatedAt,
				UpdatedAt:        row.UpdatedAt,
			},
			ProxyCount: row.ProxyCount,
		})
	}
	return out, nil
}

func (db *DB) ListDeletedProxyUsersWithProxyCounts(ctx context.Context) ([]ProxyUserWithProxyCount, error) {
	rows, err := db.q.ListDeletedProxyUsersWithProxyCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProxyUserWithProxyCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, ProxyUserWithProxyCount{
			ProxyUser: ProxyUser{
				ID:               row.ID,
				Name:             row.Name,
				DisplayName:      row.DisplayName,
				Status:           row.Status,
				GlobalQuotaBytes: row.GlobalQuotaBytes,
				ExpireAt:         row.ExpireAt,
				DeletedAt:        row.DeletedAt,
				CreatedAt:        row.CreatedAt,
				UpdatedAt:        row.UpdatedAt,
			},
			ProxyCount: row.ProxyCount,
		})
	}
	return out, nil
}

func (db *DB) ListUsersPage(ctx context.Context, filter UserFilter) (UserPage, error) {
	limit := pageLimit(filter.Limit, 50)
	offset := pageOffset(filter.Offset)
	where, args := userPageWhere(filter)
	whereSQL := strings.Join(where, " AND ")
	var total int64
	countQuery := `
SELECT COUNT(*)
FROM proxy_users u
WHERE ` + whereSQL
	if err := db.sql.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return UserPage{}, err
	}
	sortSQL := userPageSort(filter.Sort, filter.Direction)
	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, limit, offset)
	listQuery := `
SELECT
  u.id,
  u.name,
  u.display_name,
  u.status,
  u.global_quota_bytes,
  u.expire_at,
  u.deleted_at,
  u.created_at,
  u.updated_at,
  ` + userProxyCountSQL + ` AS proxy_count,
  ` + userEffectiveStatusSQL + ` AS effective_status,
  COALESCE((SELECT t.raw_bytes FROM traffic_usage_totals t WHERE t.proxy_user_id = u.id AND t.direction = 'uplink'), 0) AS uplink_raw_bytes,
  COALESCE((SELECT t.billable_bytes FROM traffic_usage_totals t WHERE t.proxy_user_id = u.id AND t.direction = 'uplink'), 0) AS uplink_billable_bytes,
  COALESCE((SELECT t.raw_bytes FROM traffic_usage_totals t WHERE t.proxy_user_id = u.id AND t.direction = 'downlink'), 0) AS downlink_raw_bytes,
  COALESCE((SELECT t.billable_bytes FROM traffic_usage_totals t WHERE t.proxy_user_id = u.id AND t.direction = 'downlink'), 0) AS downlink_billable_bytes
FROM proxy_users u
WHERE ` + whereSQL + `
ORDER BY ` + sortSQL + `
LIMIT ?
OFFSET ?`
	rows, err := db.sql.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return UserPage{}, err
	}
	defer rows.Close()
	users := make([]ProxyUserPageRow, 0)
	for rows.Next() {
		var row ProxyUserPageRow
		if err := rows.Scan(
			&row.ID,
			&row.Name,
			&row.DisplayName,
			&row.Status,
			&row.GlobalQuotaBytes,
			&row.ExpireAt,
			&row.DeletedAt,
			&row.CreatedAt,
			&row.UpdatedAt,
			&row.ProxyCount,
			&row.EffectiveStatus,
			&row.UplinkRawBytes,
			&row.UplinkBillableBytes,
			&row.DownlinkRawBytes,
			&row.DownlinkBillableBytes,
		); err != nil {
			return UserPage{}, err
		}
		users = append(users, row)
	}
	if err := rows.Err(); err != nil {
		return UserPage{}, err
	}
	return UserPage{
		Users:  users,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func userPageWhere(filter UserFilter) ([]string, []any) {
	where := []string{"u.deleted_at IS NULL"}
	args := make([]any, 0, 5)
	if strings.EqualFold(strings.TrimSpace(filter.Deleted), "only") {
		where[0] = "u.deleted_at IS NOT NULL"
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		where = append(where, userEffectiveStatusSQL+" = ?")
		args = append(args, status)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		where = append(where, `(LOWER(u.name) LIKE ? OR LOWER(u.display_name) LIKE ? OR LOWER(u.status) LIKE ? OR `+userEffectiveStatusSQL+` LIKE ?)`)
		args = append(args, like, like, like, like)
	}
	return where, args
}

func userPageSort(sort, direction string) string {
	dir := "ASC"
	if strings.EqualFold(direction, "desc") {
		dir = "DESC"
	}
	sortColumn := "u.name"
	switch strings.TrimSpace(sort) {
	case "display_name":
		sortColumn = "u.display_name"
	case "status":
		sortColumn = userEffectiveStatusSQL
	case "traffic":
		sortColumn = userBillableBytesSQL
	case "quota":
		sortColumn = "u.global_quota_bytes"
	case "proxy_count":
		sortColumn = userProxyCountSQL
	case "expire_at":
		// A missing expiry sorts as "never", which must not collapse into the
		// same slot as an expiry at the epoch.
		sortColumn = "COALESCE(NULLIF(u.expire_at, ''), '9999-12-31T23:59:59Z')"
	case "created_at":
		sortColumn = "u.created_at"
	case "updated_at":
		sortColumn = "u.updated_at"
	}
	return sortColumn + " " + dir + ", u.name ASC"
}

func (db *DB) GetProxyUser(ctx context.Context, name string) (ProxyUser, error) {
	user, err := db.q.GetProxyUserByName(ctx, normalizeName(name))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProxyUser{}, fmt.Errorf("proxy user %q not found", name)
		}
		return ProxyUser{}, err
	}
	return user, nil
}

func (db *DB) SetProxyUserStatus(ctx context.Context, name, status string) error {
	if status != "active" && status != "disabled" {
		return fmt.Errorf("unsupported user status %q", status)
	}
	affected, err := db.q.SetProxyUserStatus(ctx, store.SetProxyUserStatusParams{
		Status: status,
		Name:   normalizeName(name),
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "proxy user", name)
}

func (db *DB) DisableProxyUser(ctx context.Context, name string) (ProxyUser, error) {
	if err := db.SetProxyUserStatus(ctx, name, "disabled"); err != nil {
		return ProxyUser{}, err
	}
	return db.GetProxyUser(ctx, name)
}

func (db *DB) SoftDeleteProxyUser(ctx context.Context, name string) (ProxyUser, error) {
	affected, err := db.q.SoftDeleteProxyUser(ctx, normalizeName(name))
	if err != nil {
		return ProxyUser{}, err
	}
	if err := requireAffected(affected, "proxy user", name); err != nil {
		return ProxyUser{}, err
	}
	return db.getProxyUserIncludingDeleted(ctx, name)
}

func (db *DB) RestoreProxyUser(ctx context.Context, name string) (ProxyUser, error) {
	affected, err := db.q.RestoreProxyUser(ctx, normalizeName(name))
	if err != nil {
		return ProxyUser{}, err
	}
	if err := requireAffected(affected, "deleted proxy user", name); err != nil {
		return ProxyUser{}, err
	}
	return db.GetProxyUser(ctx, name)
}

func (db *DB) getProxyUserIncludingDeleted(ctx context.Context, name string) (ProxyUser, error) {
	user, err := db.q.GetProxyUserByNameIncludingDeleted(ctx, normalizeName(name))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProxyUser{}, fmt.Errorf("proxy user %q not found", name)
		}
		return ProxyUser{}, err
	}
	return user, nil
}

type UpdateProxyUserParams struct {
	// Nil fields are left unchanged.
	DisplayName      *string
	Status           *string
	GlobalQuotaBytes *int64
	ExpireAt         *string
}

// UpdateProxyUser applies a partial user patch atomically: all fields are
// validated first, then written in a single transaction so a later invalid
// field never leaves an earlier one half-committed.
func (db *DB) UpdateProxyUser(ctx context.Context, name string, params UpdateProxyUserParams) (ProxyUser, error) {
	normalized := normalizeName(name)
	if normalized == "" {
		return ProxyUser{}, errors.New("user name is required")
	}
	if params.Status != nil && *params.Status != "active" && *params.Status != "disabled" {
		return ProxyUser{}, fmt.Errorf("unsupported user status %q", *params.Status)
	}
	expireAt := sql.NullString{}
	if params.ExpireAt != nil {
		parsed, err := normalizeExpireAt(*params.ExpireAt)
		if err != nil {
			return ProxyUser{}, err
		}
		expireAt = parsed
	}
	err := db.withTx(ctx, func(q *store.Queries) error {
		if params.DisplayName != nil {
			affected, err := q.SetProxyUserDisplayName(ctx, store.SetProxyUserDisplayNameParams{DisplayName: *params.DisplayName, Name: normalized})
			if err != nil {
				return err
			}
			if err := requireAffected(affected, "proxy user", name); err != nil {
				return err
			}
		}
		if params.GlobalQuotaBytes != nil {
			affected, err := q.SetProxyUserQuota(ctx, store.SetProxyUserQuotaParams{GlobalQuotaBytes: *params.GlobalQuotaBytes, Name: normalized})
			if err != nil {
				return err
			}
			if err := requireAffected(affected, "proxy user", name); err != nil {
				return err
			}
		}
		if params.ExpireAt != nil {
			affected, err := q.SetProxyUserExpire(ctx, store.SetProxyUserExpireParams{ExpireAt: expireAt, Name: normalized})
			if err != nil {
				return err
			}
			if err := requireAffected(affected, "proxy user", name); err != nil {
				return err
			}
		}
		if params.Status != nil {
			affected, err := q.SetProxyUserStatus(ctx, store.SetProxyUserStatusParams{Status: *params.Status, Name: normalized})
			if err != nil {
				return err
			}
			if err := requireAffected(affected, "proxy user", name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ProxyUser{}, err
	}
	return db.GetProxyUser(ctx, name)
}

func (db *DB) SetProxyUserQuota(ctx context.Context, name string, quotaBytes int64) error {
	affected, err := db.q.SetProxyUserQuota(ctx, store.SetProxyUserQuotaParams{
		GlobalQuotaBytes: quotaBytes,
		Name:             normalizeName(name),
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "proxy user", name)
}

func (db *DB) SetProxyUserExpire(ctx context.Context, name, expireAt string) error {
	expires, err := normalizeExpireAt(expireAt)
	if err != nil {
		return err
	}
	affected, err := db.q.SetProxyUserExpire(ctx, store.SetProxyUserExpireParams{
		ExpireAt: expires,
		Name:     normalizeName(name),
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "proxy user", name)
}

// normalizeExpireAt stores expiries as UTC RFC3339. Empty means "no expiry";
// anything else must parse, because expire_at is compared and sorted as text.
func normalizeExpireAt(value string) (sql.NullString, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("expire_at must be an RFC3339 timestamp: %q", value)
	}
	return sql.NullString{String: parsed.UTC().Format(time.RFC3339), Valid: true}, nil
}
