-- The chunkers: one row per chunker identity, its model and the settings
-- its splitter reports, under a surrogate ID that chunks reference.
-- Ingest registers a chunker it hasn't seen by inserting its identity.
-- See https://github.com/JaimeStill/capstone-data-intelligence/blob/main/context/chunks.md#space-and-chunker
CREATE TABLE chunker (
  id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  model text NOT NULL,
  settings jsonb NOT NULL,
  CONSTRAINT uq_chunker_identity UNIQUE (model, settings),
  CONSTRAINT cc_chunker_model CHECK (model <> '')
);
