--| tier: standard
-- The SHA-256 of each text the source occurrences of one input yield,
-- the input named by its content's SHA-256: the texts whose chunks the
-- orphaned blobs' reclaimer considers once it deletes the occurrences.
SELECT DISTINCT text_sha256 FROM source WHERE input_sha256 = {{sha256}}
