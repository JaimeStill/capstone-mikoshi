--| tier: standard
-- The cycle check, the ancestor walk: a count above zero when node is on
-- the chain from candidate up to the root, candidate included, so a
-- candidate that is node itself counts. Its cost is the depth of
-- candidate. UNION, not UNION ALL, discards a context the walk has
-- visited, so the walk terminates on a cycle in the tree.
WITH RECURSIVE up (id, parent_id) AS (
    SELECT c.id, c.parent_id FROM context c WHERE c.id = {{candidate:uuid}}
  UNION
    SELECT c.id, c.parent_id FROM context c JOIN up u ON c.id = u.parent_id
)
SELECT COUNT(*) FROM up WHERE id = {{node:uuid}}
