--| tier: standard
--| returning: progress
-- Records one unexpired upload's length when none is recorded. A length
-- already recorded stays, and the progress read returns it.
UPDATE upload SET upload_length = {{upload_length:bigint}}
WHERE id = {{id}} AND upload_length IS NULL AND expires_at > CURRENT_TIMESTAMP
