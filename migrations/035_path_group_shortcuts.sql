-- +goose Up
-- Preserve the current selection, then stop following group membership changes.
INSERT INTO direct_path_grants (proxy_user_id, path_id, enabled)
SELECT DISTINCT a.proxy_user_id, m.path_id, 1
FROM user_path_group_grants a
JOIN path_groups g ON g.id=a.group_id AND g.enabled=1 AND g.deleted_at IS NULL
JOIN path_group_members m ON m.group_id=g.id
JOIN proxy_users u ON u.id=a.proxy_user_id AND u.deleted_at IS NULL
WHERE 1
ON CONFLICT (proxy_user_id,path_id) DO UPDATE SET enabled=1;
DELETE FROM user_path_group_grants;

-- +goose Down
-- Selection is preserved as individual grants; historical group origins cannot
-- be reconstructed. No schema change is required to run the previous binary.
SELECT 1;
