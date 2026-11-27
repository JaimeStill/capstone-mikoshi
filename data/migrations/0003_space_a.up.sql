-- Set A's embedding space, ID 1: intfloat/multilingual-e5-large through
-- the embeddings capability, 1,024 dimensions, cosine, with its partial
-- HNSW index over the cast to its dimensions, restricted to its rows. The
-- index uses pgvector's hnsw method and vector_cosine_ops operator class (a
-- native form of the pgvector extension; another engine expresses a
-- per-space approximate nearest-neighbour index differently or not at
-- all). The index's cast refuses a vector of other dimensions in space 1.
-- See https://github.com/JaimeStill/capstone-data-intelligence/blob/main/context/chunks.md#embedding
INSERT INTO embedding_space (id, capability, served_model, settings, dimensions, metric, query_prefix)
VALUES (1, 'embeddings', 'intfloat/multilingual-e5-large',
  '{"request":"1","input_type":"document","prefix":"passage: "}', 1024, 'cosine', 'query: ');

CREATE INDEX ix_embedding_space_1 ON embedding USING hnsw ((embedding::vector(1024)) vector_cosine_ops) WHERE space = 1;
