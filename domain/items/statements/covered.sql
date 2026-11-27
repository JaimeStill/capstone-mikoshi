--| tier: standard
--| key: id
--| field: id uuid not null
--| field: route text not null
--| field: kind text
--| field: duration double precision
-- The items a context covers, as the read returns each: those referenced
-- in the explicit contexts named by contexts, and those named by matches,
-- the items its query contexts' filters selected. The SELECT list is the
-- scan order, and IN lists each item once whichever sets hold it.
SELECT id, type, name, media_type, sha256, size, route, kind, duration, created_at FROM item
WHERE id IN (
    SELECT r.item_id FROM context_item r
    WHERE r.context_id IN (SELECT c.id FROM UNNEST({{contexts:uuid ARRAY}}) AS c (id))
  )
  OR id IN (SELECT m.id FROM UNNEST({{matches:uuid ARRAY}}) AS m (id))
