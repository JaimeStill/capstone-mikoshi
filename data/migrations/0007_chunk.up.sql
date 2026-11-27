-- The chunks, keyed by their source text's SHA-256 (lowercase hex), their
-- chunker, and their index in that text. Each holds its text, its span as
-- byte offsets [span_start, span_end) into the source text, and its text's
-- SHA-256, indexed, through which it reaches its embeddings. A chunk is
-- stored whether or not its embedding succeeded. Each carries a stored
-- generated to_tsvector('simple', text) with a GIN index, so it stays
-- searchable by text.
-- See https://github.com/JaimeStill/capstone-data-intelligence/blob/main/context/chunks.md#chunk
CREATE TABLE chunk (
  source_text_sha256 text NOT NULL,
  chunker integer NOT NULL,
  chunk_index integer NOT NULL,
  text text NOT NULL,
  span_start integer NOT NULL,
  span_end integer NOT NULL,
  text_sha256 text NOT NULL,
  text_search tsvector GENERATED ALWAYS AS (to_tsvector('simple', text)) STORED,
  CONSTRAINT pk_chunk PRIMARY KEY (source_text_sha256, chunker, chunk_index),
  CONSTRAINT fk_chunk_chunker FOREIGN KEY (chunker) REFERENCES chunker (id),
  CONSTRAINT cc_chunk_source_text_sha256 CHECK (source_text_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT cc_chunk_index CHECK (chunk_index >= 0),
  CONSTRAINT cc_chunk_span CHECK (span_start >= 0 AND span_end >= span_start),
  CONSTRAINT cc_chunk_text_sha256 CHECK (text_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX ix_chunk_text_sha256 ON chunk (text_sha256);

CREATE INDEX ix_chunk_text_search ON chunk USING gin (text_search);
