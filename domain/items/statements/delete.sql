--| tier: standard
-- Deletes one item by its id, once its references are deleted; no row
-- changed means no item has the id.
DELETE FROM item WHERE id = {{id:uuid}}
