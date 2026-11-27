--| tier: standard
-- Deletes one upload, and by cascade its chunks, only when it has
-- expired, so a create can take an expired upload's id.
DELETE FROM upload WHERE id = {{id}} AND expires_at <= CURRENT_TIMESTAMP
