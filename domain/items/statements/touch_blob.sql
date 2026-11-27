--| tier: standard
--| returning: blob
-- Moves one blob row's update time to now, locking the row for the rest
-- of the transaction, and the blob read returns its status. A blob being
-- written is no longer stale, and a reclaimer that locks the row waits.
UPDATE blob SET updated_at = CURRENT_TIMESTAMP WHERE sha256 = {{sha256}}
