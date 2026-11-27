--| tier: standard
-- The descendant walk: the ids and kinds of one context's subtree, the
-- context included, by adjacency down from it. UNION, not UNION ALL,
-- discards a context the walk has visited, so the walk terminates on a
-- cycle in the tree.
WITH RECURSIVE down (id, kind) AS (
    SELECT c.id, c.kind FROM context c WHERE c.id = {{id:uuid}}
  UNION
    SELECT c.id, c.kind FROM context c JOIN down d ON c.parent_id = d.id
)
SELECT id, kind FROM down
