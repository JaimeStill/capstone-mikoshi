DROP INDEX IF EXISTS ix_embedding_space_1;
DELETE FROM embedding WHERE space = 1;
DELETE FROM embedding_space WHERE id = 1;
