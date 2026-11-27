--| tier: standard
-- The content of one upload's chunk that starts at an offset.
SELECT content FROM upload_chunk WHERE upload_id = {{upload_id}} AND chunk_offset = {{chunk_offset:bigint}}
