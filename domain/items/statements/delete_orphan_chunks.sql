--| tier: standard
-- Deletes the chunks cut from one of the given source texts that no
-- remaining source names, so a chunk another source's text shares stays.
DELETE FROM chunk
WHERE source_text_sha256 IN (SELECT t.sha256 FROM UNNEST({{texts:text ARRAY}}) AS t (sha256))
  AND NOT EXISTS (SELECT 1 FROM source s WHERE s.text_sha256 = chunk.source_text_sha256)
