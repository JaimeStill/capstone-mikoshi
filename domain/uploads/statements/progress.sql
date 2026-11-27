--| tier: standard
-- One unexpired upload's progress: its offset, its length or NULL while
-- unknown, its hash state, and its expiry.
SELECT upload_offset, upload_length, hash_state, expires_at FROM upload WHERE id = {{id}} AND expires_at > CURRENT_TIMESTAMP
