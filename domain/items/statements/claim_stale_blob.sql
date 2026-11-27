--| tier: native
--| native: postgres — SELECT ... FOR UPDATE SKIP LOCKED, so a pass locks one stale pending row and passes over a row a create holds rather than wait on it. Port: an engine must lock the row it selects and skip a row another transaction holds (SQL Server: UPDLOCK with READPAST; MySQL 8: FOR UPDATE SKIP LOCKED).
--| transaction: required
-- Locks one blob row left pending since more than stale_age ago, on the
-- database's clock, that no item names, the stalest first: the pending
-- blobs' reclaimer's claim. The lock holds for the rest of the
-- transaction, in which the reclaimer deletes the row's object and then
-- the row. A row a create holds is skipped; one a create touched or
-- marked since the read began is checked again under the lock, so a
-- fresh or available row is never claimed.
SELECT sha256 FROM blob
WHERE status = 'pending'
  AND updated_at < CURRENT_TIMESTAMP - {{stale_age:interval}}
  AND NOT EXISTS (SELECT 1 FROM item WHERE item.sha256 = blob.sha256)
ORDER BY updated_at
FETCH FIRST 1 ROWS ONLY
FOR UPDATE SKIP LOCKED
