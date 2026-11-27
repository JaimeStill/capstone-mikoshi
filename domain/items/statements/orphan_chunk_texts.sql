--| tier: standard
-- The SHA-256 of each chunk text cut from one of the given source texts
-- that no remaining source names: the chunk texts whose embeddings the
-- orphaned blobs' reclaimer considers once it deletes those chunks.
SELECT DISTINCT c.text_sha256 FROM chunk c
WHERE c.source_text_sha256 IN (SELECT t.sha256 FROM UNNEST({{texts:text ARRAY}}) AS t (sha256))
  AND NOT EXISTS (SELECT 1 FROM source s WHERE s.text_sha256 = c.source_text_sha256)
