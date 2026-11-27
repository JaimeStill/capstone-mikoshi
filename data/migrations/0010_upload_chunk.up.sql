-- The chunks of each upload in progress, keyed by the upload and the
-- offset each starts at, so an upload's chunks read back in offset order.
-- A chunk holds at least one byte, so each starts past the one before it.
-- A chunk lives only as long as its upload: deleting or reclaiming the
-- upload removes its chunks.
CREATE TABLE upload_chunk (
  upload_id text NOT NULL,
  chunk_offset bigint NOT NULL,
  content bytea NOT NULL,
  CONSTRAINT pk_upload_chunk PRIMARY KEY (upload_id, chunk_offset),
  CONSTRAINT fk_upload_chunk_upload FOREIGN KEY (upload_id) REFERENCES upload (id) ON DELETE CASCADE,
  CONSTRAINT cc_upload_chunk_offset CHECK (chunk_offset >= 0),
  CONSTRAINT cc_upload_chunk_content CHECK (octet_length(content) > 0)
);
