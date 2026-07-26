-- name: CreateEndpoint :exec
INSERT INTO endpoints (id, proxy_id, host_id, enabled)
VALUES (sqlc.arg(id), sqlc.arg(proxy_id), sqlc.arg(host_id), sqlc.arg(enabled));

-- name: GetEndpointByID :one
SELECT * FROM endpoints WHERE id = sqlc.arg(id);

-- name: GetEndpointByProxyHost :one
SELECT * FROM endpoints
WHERE proxy_id = sqlc.arg(proxy_id) AND host_id = sqlc.arg(host_id);

-- name: ListEndpoints :many
SELECT * FROM endpoints ORDER BY created_at, id;

-- name: ListEndpointsByProxyID :many
SELECT * FROM endpoints WHERE proxy_id = sqlc.arg(proxy_id) ORDER BY created_at, id;

-- name: CountEndpointsByHostID :one
SELECT COUNT(*) FROM endpoints WHERE host_id = sqlc.arg(host_id);

-- name: SetEndpointEnabled :execrows
UPDATE endpoints
SET enabled = sqlc.arg(enabled),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id);

-- name: DeleteEndpoint :execrows
DELETE FROM endpoints WHERE id = sqlc.arg(id);
