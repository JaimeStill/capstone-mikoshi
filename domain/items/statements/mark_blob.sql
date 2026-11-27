--| tier: standard
-- Marks one blob row available once its object is written, and moves its
-- update time to now. No row changed means the row was reclaimed while
-- the object was written.
UPDATE blob SET status = 'available', updated_at = CURRENT_TIMESTAMP WHERE sha256 = {{sha256}}
