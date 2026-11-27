-- The embeddings, keyed by the chunk text's SHA-256 (lowercase hex) and the
-- space, so chunks with the same text share one vector per space. Every
-- space shares the one untyped pgvector vector column (a native form of
-- the pgvector extension; another engine stores the vector as an array or
-- in a separate vector store); each space's migration adds its partial
-- index over the cast to its dimensions. A row holds exactly one of a
-- vector or the error the space's endpoint returned.
-- See https://github.com/JaimeStill/capstone-data-intelligence/blob/main/context/chunks.md#embedding
CREATE TABLE embedding (
  chunk_text_sha256 text NOT NULL,
  space integer NOT NULL,
  embedding vector,
  error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pk_embedding PRIMARY KEY (chunk_text_sha256, space),
  CONSTRAINT fk_embedding_space FOREIGN KEY (space) REFERENCES embedding_space (id),
  CONSTRAINT cc_embedding_chunk_text_sha256 CHECK (chunk_text_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT cc_embedding_vector_or_error CHECK ((embedding IS NULL) <> (error IS NULL)),
  CONSTRAINT cc_embedding_error CHECK (error <> '')
);
