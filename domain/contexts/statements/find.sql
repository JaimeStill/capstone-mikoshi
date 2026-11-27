--| tier: standard
-- One context by its id, as the read returns it.
SELECT id, parent_id, name, kind, filter, created_at FROM context WHERE id = {{id:uuid}}
