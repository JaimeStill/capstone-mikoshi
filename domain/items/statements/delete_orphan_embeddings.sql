--| tier: standard
-- Deletes the embeddings, in every space, of the given chunk texts that
-- no remaining chunk names, so a vector another chunk shares stays.
DELETE FROM embedding
WHERE chunk_text_sha256 IN (SELECT t.sha256 FROM UNNEST({{texts:text ARRAY}}) AS t (sha256))
  AND NOT EXISTS (SELECT 1 FROM chunk c WHERE c.text_sha256 = embedding.chunk_text_sha256)
