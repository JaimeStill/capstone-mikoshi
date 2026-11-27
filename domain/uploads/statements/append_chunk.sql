--| tier: standard
-- Stores one chunk of an upload at the offset it starts at, in the
-- transaction that advanced the upload past it.
INSERT INTO upload_chunk (upload_id, chunk_offset, content)
VALUES ({{upload_id}}, {{chunk_offset:bigint}}, {{content}})
