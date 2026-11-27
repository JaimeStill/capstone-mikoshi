--| tier: standard
-- Inserts one blob row as pending, before its object is written. Its
-- SHA-256 is the primary key, so a row written concurrently is a unique
-- violation.
INSERT INTO blob (sha256, size, status) VALUES ({{sha256}}, {{size:bigint}}, 'pending')
