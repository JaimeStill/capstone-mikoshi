--| tier: standard
--| returning: progress
-- The conditional commit of an append: moves one unexpired upload's
-- offset from expected to next and stores the hash state, only while its
-- offset is expected. Of two appends racing at one offset, the second
-- waits on the first's row lock and then finds the offset moved, so it
-- changes nothing and the progress read returns the winner's offset.
UPDATE upload SET upload_offset = {{next:bigint}}, hash_state = {{hash_state}}
WHERE id = {{id}} AND upload_offset = {{expected:bigint}} AND expires_at > CURRENT_TIMESTAMP
