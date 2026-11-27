--| tier: standard
-- Inserts one upload at offset 0 with no hash state. Its id is the
-- primary key, so an id in use is a unique violation.
INSERT INTO upload (id, media_type, digest, name, upload_length, expires_at)
VALUES ({{id}}, {{media_type}}, {{digest}}, {{name}}, {{upload_length:bigint}}, {{expires_at:timestamp with time zone}})
