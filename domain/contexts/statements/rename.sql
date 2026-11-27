--| tier: standard
--| returning: find
-- Sets one context's name, its parent unchanged.
UPDATE context SET name = {{name}} WHERE id = {{id:uuid}}
