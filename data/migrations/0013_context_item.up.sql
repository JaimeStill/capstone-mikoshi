-- The items placed in explicit contexts, one row per reference, so one
-- item may belong to many contexts. A reference lives only as long as its
-- context: deleting a context, alone or with the subtree above it,
-- removes its references. The foreign key to the item does not cascade,
-- so an item's references are deleted with it explicitly, in the
-- transaction that deletes it. The index on item_id serves that delete.
CREATE TABLE context_item (
  context_id uuid NOT NULL,
  item_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pk_context_item PRIMARY KEY (context_id, item_id),
  CONSTRAINT fk_context_item_context FOREIGN KEY (context_id) REFERENCES context (id) ON DELETE CASCADE,
  CONSTRAINT fk_context_item_item FOREIGN KEY (item_id) REFERENCES item (id)
);

CREATE INDEX ix_context_item_item_id ON context_item (item_id);
