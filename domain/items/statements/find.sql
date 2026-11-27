--| tier: standard
-- One item by its id, as the read returns it.
SELECT id, type, name, media_type, sha256, size, route, kind, duration, created_at FROM item WHERE id = {{id:uuid}}
