-- +goose Up
CREATE TABLE log_event_search_documents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL UNIQUE REFERENCES log_events(id) ON DELETE CASCADE
);

INSERT INTO log_event_search_documents (event_id)
SELECT id FROM log_events;

CREATE VIRTUAL TABLE log_events_search USING fts3(
  node_name,
  user_name,
  auth_name,
  source_ip,
  target_host,
  target_port,
  action,
  raw_message
);

INSERT INTO log_events_search (
  docid,
  node_name,
  user_name,
  auth_name,
  source_ip,
  target_host,
  target_port,
  action,
  raw_message
)
SELECT
  d.id,
  n.name,
  u.name,
  e.auth_name,
  e.source_ip,
  e.target_host,
  CAST(e.target_port AS TEXT),
  e.action,
  e.raw_message
FROM log_event_search_documents d
JOIN log_events e ON e.id = d.event_id
JOIN nodes n ON n.id = e.node_id
JOIN proxy_users u ON u.id = e.proxy_user_id
WHERE e.proxy_user_id IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER log_events_search_after_insert
AFTER INSERT ON log_events
BEGIN
  INSERT INTO log_event_search_documents (event_id) VALUES (NEW.id);
  INSERT INTO log_events_search (
    docid,
    node_name,
    user_name,
    auth_name,
    source_ip,
    target_host,
    target_port,
    action,
    raw_message
  )
  SELECT
    d.id,
    n.name,
    u.name,
    NEW.auth_name,
    NEW.source_ip,
    NEW.target_host,
    CAST(NEW.target_port AS TEXT),
    NEW.action,
    NEW.raw_message
  FROM log_event_search_documents d
  JOIN nodes n ON n.id = NEW.node_id
  JOIN proxy_users u ON u.id = NEW.proxy_user_id
  WHERE d.event_id = NEW.id
    AND NEW.proxy_user_id IS NOT NULL;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER log_events_search_after_update
AFTER UPDATE ON log_events
BEGIN
  DELETE FROM log_events_search
  WHERE docid = (
    SELECT id FROM log_event_search_documents WHERE event_id = OLD.id
  );
  INSERT INTO log_events_search (
    docid,
    node_name,
    user_name,
    auth_name,
    source_ip,
    target_host,
    target_port,
    action,
    raw_message
  )
  SELECT
    d.id,
    n.name,
    u.name,
    NEW.auth_name,
    NEW.source_ip,
    NEW.target_host,
    CAST(NEW.target_port AS TEXT),
    NEW.action,
    NEW.raw_message
  FROM log_event_search_documents d
  JOIN nodes n ON n.id = NEW.node_id
  JOIN proxy_users u ON u.id = NEW.proxy_user_id
  WHERE d.event_id = NEW.id
    AND NEW.proxy_user_id IS NOT NULL;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER log_events_search_after_delete
BEFORE DELETE ON log_events
BEGIN
  DELETE FROM log_events_search
  WHERE docid = (
    SELECT id FROM log_event_search_documents WHERE event_id = OLD.id
  );
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER log_events_search_after_node_rename
AFTER UPDATE OF name ON nodes
WHEN OLD.name <> NEW.name
BEGIN
  UPDATE log_events_search
  SET node_name = NEW.name
  WHERE docid IN (
    SELECT d.id
    FROM log_event_search_documents d
    JOIN log_events e ON e.id = d.event_id
    WHERE e.node_id = NEW.id
      AND e.proxy_user_id IS NOT NULL
  );
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER log_events_search_after_user_rename
AFTER UPDATE OF name ON proxy_users
WHEN OLD.name <> NEW.name
BEGIN
  UPDATE log_events_search
  SET user_name = NEW.name
  WHERE docid IN (
    SELECT d.id
    FROM log_event_search_documents d
    JOIN log_events e ON e.id = d.event_id
    WHERE e.proxy_user_id = NEW.id
  );
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS log_events_search_after_user_rename;
DROP TRIGGER IF EXISTS log_events_search_after_node_rename;
DROP TRIGGER IF EXISTS log_events_search_after_delete;
DROP TRIGGER IF EXISTS log_events_search_after_update;
DROP TRIGGER IF EXISTS log_events_search_after_insert;
DROP TABLE IF EXISTS log_events_search;
DROP TABLE IF EXISTS log_event_search_documents;
