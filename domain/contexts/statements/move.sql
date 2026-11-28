--| tier: standard
--| returning: find
-- Sets one context's parent and name. Its descendants follow it by
-- adjacency, their own parents unchanged. A context that already has the
-- parent and the name matches no row, so the command reports it unchanged.
UPDATE context SET parent_id = {{parent_id:uuid}}, name = {{name}} WHERE id = {{id:uuid}} AND (parent_id <> {{parent_id:uuid}} OR name <> {{name}})
