--| tier: standard
--| returning: find
-- Sets one context's parent and name. Its descendants follow it by
-- adjacency, their own parents unchanged.
UPDATE context SET parent_id = {{parent_id:uuid}}, name = {{name}} WHERE id = {{id:uuid}}
