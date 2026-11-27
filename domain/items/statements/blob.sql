--| tier: standard
-- One blob row by its SHA-256: its size and status.
SELECT sha256, size, status FROM blob WHERE sha256 = {{sha256}}
