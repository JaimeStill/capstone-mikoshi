--| tier: standard
-- Deletes every reference to one item, in the transaction that deletes
-- the item, through the index on item_id.
DELETE FROM context_item WHERE item_id = {{id:uuid}}
