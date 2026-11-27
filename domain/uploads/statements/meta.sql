--| tier: standard
-- What one unexpired upload's creation kept: its media type, digest, and
-- file name.
SELECT media_type, digest, name FROM upload WHERE id = {{id}} AND expires_at > CURRENT_TIMESTAMP
