--| tier: standard
-- Deletes one unexpired upload, and by cascade its chunks.
DELETE FROM upload WHERE id = {{id}} AND expires_at > CURRENT_TIMESTAMP
