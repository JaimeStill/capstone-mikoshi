--| tier: standard
-- Deletes one pending blob row, in the transaction that claimed it, once
-- its object is deleted.
DELETE FROM blob WHERE sha256 = {{sha256}} AND status = 'pending'
