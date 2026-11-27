-- The items: each a named entry over the blob its SHA-256 names, of type
-- file or record. An item keeps its name and media type, its content's
-- SHA-256 and size, docproc's route and kind for the content (kind NULL
-- when no signature names one), and its duration in seconds, NULL until
-- ingest fills it. A file item keeps the id of the upload that created
-- it, unique, so one upload creates one item. The foreign key names the
-- blob without cascading, so a blob an item names is not deleted.
CREATE TABLE item (
  id uuid NOT NULL,
  type text NOT NULL,
  name text NOT NULL,
  media_type text NOT NULL,
  sha256 text NOT NULL,
  size bigint NOT NULL,
  route text NOT NULL,
  kind text,
  duration double precision,
  upload_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pk_item PRIMARY KEY (id),
  CONSTRAINT fk_item_blob FOREIGN KEY (sha256) REFERENCES blob (sha256),
  CONSTRAINT uq_item_upload UNIQUE (upload_id),
  CONSTRAINT cc_item_type CHECK (type IN ('file', 'record')),
  CONSTRAINT cc_item_media_type CHECK (media_type <> ''),
  CONSTRAINT cc_item_sha256 CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT cc_item_size CHECK (size >= 0),
  CONSTRAINT cc_item_route CHECK (route <> ''),
  CONSTRAINT cc_item_kind CHECK (kind <> ''),
  CONSTRAINT cc_item_duration CHECK (duration >= 0)
);

CREATE INDEX ix_item_sha256 ON item (sha256);
