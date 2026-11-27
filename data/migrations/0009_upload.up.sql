-- The resumable uploads in progress, each keyed by the id its creation
-- mints. Each keeps what its creation carried (the file's media type, its
-- SHA-256 in lowercase hex, and its file name), its length once known,
-- the offset its committed chunks reach, the running SHA-256 state those
-- chunks leave (NULL before the first), and the time it expires, indexed
-- so the expired uploads are found and reclaimed. An upload past its
-- expiry is missing to every read.
CREATE TABLE upload (
  id text PRIMARY KEY,
  media_type text NOT NULL,
  digest text NOT NULL,
  name text NOT NULL,
  upload_length bigint,
  upload_offset bigint NOT NULL DEFAULT 0,
  hash_state bytea,
  expires_at timestamptz NOT NULL,
  CONSTRAINT cc_upload_id CHECK (id <> ''),
  CONSTRAINT cc_upload_digest CHECK (digest ~ '^[0-9a-f]{64}$'),
  CONSTRAINT cc_upload_length CHECK (upload_length >= 0),
  CONSTRAINT cc_upload_offset CHECK (upload_offset >= 0)
);

CREATE INDEX ix_upload_expires_at ON upload (expires_at);
