--| tier: standard
-- The item the upload with the given id created, as the read returns it.
SELECT id, type, name, media_type, sha256, size, route, kind, duration, created_at FROM item WHERE upload_id = {{upload_id}}
