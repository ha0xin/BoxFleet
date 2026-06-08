-- +goose Up
ALTER TABLE node_tokens ADD COLUMN token_digest TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_node_tokens_token_digest
  ON node_tokens(token_digest)
  WHERE token_digest IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_node_tokens_token_digest;

CREATE TABLE node_tokens_old (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  last_used_at TEXT,
  revoked_at TEXT
);

INSERT INTO node_tokens_old (
  id,
  node_id,
  token_hash,
  created_at,
  last_used_at,
  revoked_at
)
SELECT
  id,
  node_id,
  token_hash,
  created_at,
  last_used_at,
  revoked_at
FROM node_tokens;

DROP TABLE node_tokens;
ALTER TABLE node_tokens_old RENAME TO node_tokens;
CREATE INDEX IF NOT EXISTS idx_node_tokens_node_id ON node_tokens(node_id);
