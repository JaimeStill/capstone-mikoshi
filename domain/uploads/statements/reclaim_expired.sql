--| tier: standard
-- Deletes at most budget expired uploads, earliest expiry first, and by
-- cascade their chunks: the expired uploads' reclaimer's pass. The
-- subquery reads ix_upload_expires_at in expiry order and stops at the
-- budget.
DELETE FROM upload
WHERE id IN (
    SELECT id FROM upload
    WHERE expires_at <= CURRENT_TIMESTAMP
    ORDER BY expires_at
    FETCH FIRST {{budget:bigint}} ROWS ONLY
)
