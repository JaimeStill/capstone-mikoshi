--| tier: standard
--| returning: find
-- Inserts one item over the blob its SHA-256 names, its id minted by the
-- service, its duration unknown, and its creation time the column's
-- default. A second item for one upload is a unique violation.
INSERT INTO item (id, type, name, media_type, sha256, size, route, kind, upload_id)
VALUES ({{id:uuid}}, {{type}}, {{name}}, {{media_type}}, {{sha256}}, {{size:bigint}}, {{route}}, {{kind}}, {{upload_id}})
