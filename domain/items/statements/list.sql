--| tier: standard
--| key: id
--| field: id uuid not null
--| field: route text not null
--| field: kind text
--| field: duration double precision
-- Every item, as the read returns each. The SELECT list is the scan
-- order. route, kind, and duration are the fields a filter may name.
SELECT id, type, name, media_type, sha256, size, route, kind, duration, created_at FROM item
