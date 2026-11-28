--| tier: standard
--| returning: find
-- Sets one context's name, its parent unchanged. A context that already
-- has the name matches no row, so the command reports it unchanged.
UPDATE context SET name = {{name}} WHERE id = {{id:uuid}} AND name <> {{name}}
