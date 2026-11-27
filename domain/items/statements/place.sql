--| tier: standard
-- Places one item in one context by reference. A reference already in
-- place is a unique violation, an unknown context or item a foreign key
-- violation.
INSERT INTO context_item (context_id, item_id) VALUES ({{context_id:uuid}}, {{item_id:uuid}})
