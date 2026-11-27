--| tier: standard
-- Removes one item's reference from one context; no row changed means
-- the reference was not in place.
DELETE FROM context_item WHERE context_id = {{context_id:uuid}} AND item_id = {{item_id:uuid}}
