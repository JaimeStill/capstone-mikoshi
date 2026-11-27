-- The source occurrences: one row per docproc part of an input, keyed by
-- the input content's SHA-256 (lowercase hex) and the part ID, which names
-- where the part occurs in its input, "/" for the input itself. Each holds
-- the part's SHA-256, content type, parent part ID, depth, and where it
-- was found; the input itself, at depth 0, has neither a parent nor a
-- where.
-- See https://github.com/JaimeStill/capstone-data-intelligence/blob/main/context/chunks.md#source-occurrence
CREATE TABLE source_occurrence (
  input_sha256 text NOT NULL,
  part_id text NOT NULL,
  part_sha256 text NOT NULL,
  content_type text NOT NULL,
  parent text,
  depth integer NOT NULL,
  where_found text,
  CONSTRAINT pk_source_occurrence PRIMARY KEY (input_sha256, part_id),
  CONSTRAINT cc_source_occurrence_input_sha256 CHECK (input_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT cc_source_occurrence_part_id CHECK (part_id <> ''),
  CONSTRAINT cc_source_occurrence_part_sha256 CHECK (part_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT cc_source_occurrence_content_type CHECK (content_type <> ''),
  CONSTRAINT cc_source_occurrence_parent CHECK (parent <> ''),
  CONSTRAINT cc_source_occurrence_depth CHECK (depth >= 0),
  CONSTRAINT cc_source_occurrence_parent_depth CHECK ((parent IS NULL) = (depth = 0)),
  CONSTRAINT cc_source_occurrence_where_found CHECK (where_found <> '')
);
