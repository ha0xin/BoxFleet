-- name: ListPathGroups :many
SELECT * FROM path_groups WHERE deleted_at IS NULL ORDER BY name, id;

-- name: RecordPathGroupChange :exec
INSERT INTO path_group_changes (id,group_id,change_json,impact_json) VALUES (sqlc.arg(id),sqlc.arg(group_id),sqlc.arg(change_json),sqlc.arg(impact_json));

-- name: ListPathGroupChanges :many
SELECT * FROM path_group_changes WHERE group_id=sqlc.arg(group_id) ORDER BY created_at DESC,id DESC LIMIT 100;

-- name: GetPathGroup :one
SELECT * FROM path_groups WHERE id=sqlc.arg(id) AND deleted_at IS NULL;

-- name: CreatePathGroup :exec
INSERT INTO path_groups (id,name,description,enabled) VALUES (sqlc.arg(id),sqlc.arg(name),sqlc.arg(description),sqlc.arg(enabled));

-- name: UpdatePathGroup :execrows
UPDATE path_groups SET name=sqlc.arg(name),description=sqlc.arg(description),enabled=sqlc.arg(enabled),
revision=revision+1, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE id=sqlc.arg(id) AND revision=sqlc.arg(revision) AND deleted_at IS NULL;

-- name: ArchivePathGroup :execrows
UPDATE path_groups SET enabled=0,deleted_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),revision=revision+1
WHERE id=sqlc.arg(id) AND deleted_at IS NULL;

-- name: ListPathGroupMembers :many
SELECT * FROM path_group_members ORDER BY group_id,sort_order,path_id;

-- name: DeletePathGroupMembers :exec
DELETE FROM path_group_members WHERE group_id=sqlc.arg(group_id);

-- name: AddPathGroupMember :exec
INSERT INTO path_group_members (group_id,path_id,sort_order) VALUES (sqlc.arg(group_id),sqlc.arg(path_id),sqlc.arg(sort_order));

-- name: ListPathGroupGrants :many
SELECT * FROM user_path_group_grants ORDER BY proxy_user_id,group_id;

-- name: GrantUserPathGroup :exec
INSERT INTO user_path_group_grants (proxy_user_id,group_id) VALUES (sqlc.arg(proxy_user_id),sqlc.arg(group_id)) ON CONFLICT DO NOTHING;

-- name: RevokeUserPathGroup :exec
DELETE FROM user_path_group_grants WHERE proxy_user_id=sqlc.arg(proxy_user_id) AND group_id=sqlc.arg(group_id);

-- name: SetDirectPathGrant :exec
INSERT INTO direct_path_grants (proxy_user_id,path_id,enabled) VALUES (sqlc.arg(proxy_user_id),sqlc.arg(path_id),1)
ON CONFLICT (proxy_user_id,path_id) DO UPDATE SET enabled=1;

-- name: HasManagedDirectGrantForProxyUser :one
SELECT EXISTS(SELECT 1 FROM direct_path_grants d
JOIN paths p ON p.id=d.path_id JOIN endpoints e ON e.id=p.endpoint_id
WHERE d.proxy_user_id=sqlc.arg(proxy_user_id) AND e.proxy_id=sqlc.arg(proxy_id)
AND d.enabled=1 AND p.managed=1);

-- name: DeleteDirectPathGrant :exec
DELETE FROM direct_path_grants WHERE proxy_user_id=sqlc.arg(proxy_user_id) AND path_id=sqlc.arg(path_id);

-- name: ListUserDirectPathGrants :many
SELECT * FROM direct_path_grants WHERE proxy_user_id=sqlc.arg(proxy_user_id) ORDER BY path_id;

-- name: ListUserDesiredPathIDs :many
SELECT d.path_id FROM direct_path_grants d WHERE d.proxy_user_id=sqlc.arg(proxy_user_id) AND d.enabled=1
UNION
SELECT m.path_id FROM path_group_members m
JOIN path_groups g ON g.id=m.group_id
JOIN user_path_group_grants a ON a.group_id=g.id
WHERE a.proxy_user_id=sqlc.arg(proxy_user_id) AND g.enabled=1 AND g.deleted_at IS NULL
ORDER BY path_id;

-- name: ListUserPathAccessesIncludingInactive :many
SELECT * FROM path_accesses WHERE proxy_user_id=sqlc.arg(proxy_user_id) ORDER BY path_id;

-- name: RemoveRetiredPathGroupSources :exec
DELETE FROM path_group_members WHERE path_id=sqlc.arg(path_id);

-- name: RemoveRetiredPathDirectSources :exec
DELETE FROM direct_path_grants WHERE path_id=sqlc.arg(path_id);

-- name: DeleteUserPathGroupGrants :exec
DELETE FROM user_path_group_grants WHERE proxy_user_id=sqlc.arg(proxy_user_id);

-- name: DeleteUserDirectPathGrants :exec
DELETE FROM direct_path_grants WHERE proxy_user_id=sqlc.arg(proxy_user_id);

-- name: ArchiveUserPathAccesses :exec
UPDATE path_accesses SET enabled=0,deleted_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE proxy_user_id=sqlc.arg(proxy_user_id) AND deleted_at IS NULL;

-- name: ListGroupUserCredentialsIncludingRevoked :many
SELECT * FROM proxy_access_details WHERE proxy_user_id=sqlc.arg(proxy_user_id) ORDER BY id;

-- name: ArchiveGroupUserCredentials :exec
UPDATE proxy_accesses SET enabled=0,deleted_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE proxy_user_id=sqlc.arg(proxy_user_id) AND deleted_at IS NULL;

-- name: PathGroupPreviewState :one
SELECT CAST(json_array(
 (SELECT json_group_array(json_array(id,name,description,enabled,revision,deleted_at)) FROM (SELECT * FROM path_groups ORDER BY id,name)),
 (SELECT json_group_array(json_array(group_id,path_id,sort_order)) FROM (SELECT * FROM path_group_members ORDER BY group_id,path_id)),
 (SELECT json_group_array(json_array(proxy_user_id,group_id)) FROM (SELECT * FROM user_path_group_grants ORDER BY proxy_user_id,group_id)),
 (SELECT json_group_array(json_array(proxy_user_id,path_id,enabled)) FROM (SELECT * FROM direct_path_grants ORDER BY proxy_user_id,path_id)),
 (SELECT json_group_array(json_array(id,path_id,proxy_user_id,enabled,deleted_at)) FROM (SELECT * FROM path_accesses ORDER BY id,path_id)),
 (SELECT json_group_array(json_array(id,name,display_name,endpoint_id,dialer_path_id,enabled,visibility)) FROM (SELECT * FROM paths ORDER BY id,name)),
 (SELECT json_group_array(json_array(id,proxy_id,host_id,enabled)) FROM (SELECT * FROM endpoints ORDER BY id,proxy_id)),
 (SELECT json_group_array(json_array(id,node_id,name,protocol,enabled,deleted_at,settings_json)) FROM (SELECT * FROM proxies ORDER BY id,node_id)),
 (SELECT json_group_array(json_array(id,name,status,deleted_at)) FROM (SELECT * FROM nodes ORDER BY id,name)),
 (SELECT json_group_array(json_array(id,name,status,expire_at,deleted_at)) FROM (SELECT * FROM proxy_users ORDER BY id,name)),
 (SELECT json_group_array(json_array(id,proxy_user_id,node_id,enabled,disabled_reason)) FROM (SELECT * FROM user_node_bindings ORDER BY id,proxy_user_id)),
 (SELECT json_group_array(json_array(id,proxy_id,proxy_user_id,enabled,deleted_at,credential_json)) FROM (SELECT * FROM proxy_accesses ORDER BY id,proxy_id))
) AS TEXT) AS state;

-- name: ListPathGroupSelectionCounts :many
SELECT g.id AS group_id, COUNT(u.id) AS selected_user_count
FROM path_groups g
LEFT JOIN proxy_users u ON u.deleted_at IS NULL
AND EXISTS (
 SELECT 1 FROM path_group_members m JOIN paths p ON p.id=m.path_id
 WHERE m.group_id=g.id AND p.enabled=1 AND p.visibility='selectable'
)
AND NOT EXISTS (
 SELECT 1 FROM path_group_members m JOIN paths p ON p.id=m.path_id
 WHERE m.group_id=g.id AND p.enabled=1 AND p.visibility='selectable'
 AND NOT EXISTS (
  SELECT 1 FROM path_accesses a WHERE a.path_id=m.path_id
  AND a.proxy_user_id=u.id AND a.enabled=1 AND a.deleted_at IS NULL
 )
)
WHERE g.deleted_at IS NULL
GROUP BY g.id;
