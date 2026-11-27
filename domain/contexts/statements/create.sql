--| tier: standard
--| returning: find
-- Inserts one context under its parent, its id minted by the service and
-- its creation time the column's default.
INSERT INTO context (id, parent_id, name, kind, filter)
VALUES ({{id:uuid}}, {{parent_id:uuid}}, {{name}}, {{kind}}, {{filter}})
