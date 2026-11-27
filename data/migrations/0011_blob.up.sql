-- The content-keyed blobs items name, one row per SHA-256 in lowercase
-- hex, its object at sha256/<hex> in the bucket. A row is written pending
-- before its object is written and marked available after, so an
-- available row has its object. updated_at moves at each write to the row
-- and each item created over it; the reclaimers measure a stale pending
-- row, and content no item names, from it. Items name a blob by its
-- digest, and no delete cascades from an item to its blob.
CREATE TABLE blob (
  sha256 text NOT NULL,
  size bigint NOT NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pk_blob PRIMARY KEY (sha256),
  CONSTRAINT cc_blob_sha256 CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT cc_blob_size CHECK (size >= 0),
  CONSTRAINT cc_blob_status CHECK (status IN ('pending', 'available'))
);

CREATE INDEX ix_blob_status_updated_at ON blob (status, updated_at);
