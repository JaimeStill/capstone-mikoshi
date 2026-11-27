--| tier: standard
-- Moves the update time of the blob the item with the given id names to
-- now, locking the row for the rest of the transaction that deletes the
-- item, so content no item names is stale only once its last item's
-- deletion is older than the stale age. No row changed means no item has
-- the id.
UPDATE blob SET updated_at = CURRENT_TIMESTAMP
WHERE sha256 = (SELECT i.sha256 FROM item i WHERE i.id = {{id:uuid}})
