-- The embedding spaces: one row per space at a fixed ID, which the space's
-- own migration registers with its partial index. A space is identified by
-- its capability, served model, document-convention settings, and
-- dimensions; the metric and the query prefix are registered attributes.
-- See https://github.com/JaimeStill/capstone-data-intelligence/blob/main/context/chunks.md#embedding-space
CREATE TABLE embedding_space (
  id integer PRIMARY KEY,
  capability text NOT NULL,
  served_model text NOT NULL,
  settings jsonb NOT NULL,
  dimensions integer NOT NULL,
  metric text NOT NULL,
  query_prefix text NOT NULL,
  CONSTRAINT uq_embedding_space_identity UNIQUE (capability, served_model, settings, dimensions),
  CONSTRAINT cc_embedding_space_id CHECK (id > 0),
  CONSTRAINT cc_embedding_space_capability CHECK (capability <> ''),
  CONSTRAINT cc_embedding_space_served_model CHECK (served_model <> ''),
  CONSTRAINT cc_embedding_space_dimensions CHECK (dimensions > 0),
  CONSTRAINT cc_embedding_space_metric CHECK (metric IN ('l2', 'cosine', 'inner product'))
);
