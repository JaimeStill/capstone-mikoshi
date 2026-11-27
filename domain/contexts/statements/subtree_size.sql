--| tier: standard
-- The delete's count, the descendant walk counted: the number of contexts
-- in one context's subtree, the context included, so zero for an unknown
-- context and one for a leaf. UNION, not UNION ALL, discards a context the
-- walk has visited, so the walk terminates on a cycle in the tree.
WITH RECURSIVE down (id) AS (
    SELECT c.id FROM context c WHERE c.id = {{id:uuid}}
  UNION
    SELECT c.id FROM context c JOIN down d ON c.parent_id = d.id
)
SELECT COUNT(*) FROM down
