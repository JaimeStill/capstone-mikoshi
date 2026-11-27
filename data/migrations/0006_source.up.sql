-- The sources: the texts a source occurrence yields, each a child of its
-- occurrence, keyed by the occurrence, the source's kind, and its text's
-- SHA-256 (lowercase hex), with the identity of the stage that made it
-- (name, version, and configuration hash) when one did. Sources are
-- indexed on the text's SHA-256, through which chunks join to them.
-- See https://github.com/JaimeStill/capstone-data-intelligence/blob/main/context/chunks.md#source-occurrence
CREATE TABLE source (
  input_sha256 text NOT NULL,
  part_id text NOT NULL,
  kind text NOT NULL,
  text_sha256 text NOT NULL,
  stage_name text,
  stage_version text,
  stage_config text,
  CONSTRAINT pk_source PRIMARY KEY (input_sha256, part_id, kind, text_sha256),
  CONSTRAINT fk_source_occurrence FOREIGN KEY (input_sha256, part_id)
    REFERENCES source_occurrence (input_sha256, part_id) ON DELETE CASCADE,
  CONSTRAINT cc_source_kind CHECK (kind IN ('output', 'metadata', 'vision', 'transcript')),
  CONSTRAINT cc_source_text_sha256 CHECK (text_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT cc_source_stage CHECK (
    (stage_name IS NULL) = (stage_version IS NULL)
    AND (stage_name IS NULL) = (stage_config IS NULL)
  ),
  CONSTRAINT cc_source_stage_name CHECK (stage_name <> '')
);

CREATE INDEX ix_source_text_sha256 ON source (text_sha256);
