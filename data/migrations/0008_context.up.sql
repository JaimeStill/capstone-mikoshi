-- The context tree: each context names its parent, its name unique among
-- its siblings. A context is explicit or query, fixed at creation; a query
-- context carries its filter, a non-empty JSON array of conditions, and an
-- explicit one carries none. The root, at the nil UUID, is the one context
-- without a parent, seeded here.
CREATE TABLE context (
  id uuid PRIMARY KEY,
  parent_id uuid,
  name text NOT NULL,
  kind text NOT NULL,
  filter jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_context_parent FOREIGN KEY (parent_id) REFERENCES context (id),
  CONSTRAINT uq_context_sibling_name UNIQUE (parent_id, name),
  CONSTRAINT cc_context_root CHECK ((parent_id IS NULL) = (id = '00000000-0000-0000-0000-000000000000')),
  CONSTRAINT cc_context_name CHECK (name <> ''),
  CONSTRAINT cc_context_kind CHECK (kind IN ('explicit', 'query')),
  CONSTRAINT cc_context_filter CHECK ((kind = 'query') = (filter IS NOT NULL)),
  CONSTRAINT cc_context_filter_list CHECK (jsonb_typeof(filter) = 'array' AND jsonb_array_length(filter) > 0)
);

INSERT INTO context (id, parent_id, name, kind)
VALUES ('00000000-0000-0000-0000-000000000000', NULL, 'root', 'explicit');
