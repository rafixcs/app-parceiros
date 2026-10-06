-- Extensions and a guard against databases created with the old schema.
--
-- The schema was rewritten in English before the first production deploy,
-- and the migrations are tracked in a new table (schema_migrations). A local
-- database created before that still has the old tables: recreate it.

-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('public.usuarios') IS NOT NULL THEN
        RAISE EXCEPTION 'this database has the old schema (table usuarios); recreate it, see docs/arquitetura.md';
    END IF;
END
$$;
-- +goose StatementEnd

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- +goose Down
DROP EXTENSION IF EXISTS pg_trgm;
DROP EXTENSION IF EXISTS pgcrypto;
