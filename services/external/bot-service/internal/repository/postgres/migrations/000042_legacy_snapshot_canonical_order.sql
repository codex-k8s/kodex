-- +goose Up
-- Канонический порядок snapshot не зависит от locale исходной PostgreSQL.
-- Payload остаётся чувствительным и доступным только migration principal через
-- ранее выданный EXECUTE grant на функцию с неизменной сигнатурой.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION matter_codex_legacy_snapshot_rows()
RETURNS TABLE(table_name text, row_payload text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
	source_table record;
BEGIN
	FOR source_table IN
		SELECT inventory.table_name AS name
		FROM public.matter_codex_legacy_source_tables() AS inventory
		ORDER BY inventory.table_name COLLATE "C"
	LOOP
		table_name := source_table.name;
		row_payload := NULL;
		RETURN NEXT;
		RETURN QUERY EXECUTE format(
			'SELECT %L::text, to_jsonb(source_row)::text FROM public.%I AS source_row ORDER BY to_jsonb(source_row)::text COLLATE "C"',
			source_table.name,
			source_table.name
		);
	END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
	RAISE EXCEPTION 'migration 000042 is forward-only: canonical snapshot ordering cannot be reverted safely';
END
$$;
-- +goose StatementEnd
