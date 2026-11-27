--| tier: standard
-- Deletes one available blob row, in the transaction that claimed it,
-- once its object and the content's source occurrences are deleted.
DELETE FROM blob WHERE sha256 = {{sha256}} AND status = 'available'
