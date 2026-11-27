--| tier: standard
-- Deletes the source occurrences of one input, named by its content's
-- SHA-256, and their sources by the cascade from the occurrence.
DELETE FROM source_occurrence WHERE input_sha256 = {{sha256}}
