--| tier: standard
-- Deletes one context and its whole subtree, by the descendant walk, in
-- one statement, so the parent foreign key, checked at the statement's
-- end, finds no row whose parent the statement removed. UNION, not UNION
-- ALL, discards a context the walk has visited, so the walk terminates on
-- a cycle in the tree.
WITH RECURSIVE down (id) AS (
    SELECT c.id FROM context c WHERE c.id = {{id:uuid}}
  UNION
    SELECT c.id FROM context c JOIN down d ON c.parent_id = d.id
)
DELETE FROM context WHERE id IN (SELECT id FROM down)
