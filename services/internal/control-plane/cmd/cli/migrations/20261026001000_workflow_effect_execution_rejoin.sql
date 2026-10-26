-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';

-- Новый generation может повторно использовать physical node. Уникальность
-- rejoin связывается с полным execution tuple; прежний prefix остаётся immutable.
-- +goose StatementBegin
DO $migration$
DECLARE v_source text; v_updated text;
BEGIN
    SELECT prosrc INTO STRICT v_source FROM pg_proc
    WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure;
    IF md5(v_source) IS DISTINCT FROM 'f17f47b59c2c0dbece7fdecf8138947d' THEN
        RAISE EXCEPTION 'workflow effect guard source changed';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger trigger_row
        WHERE trigger_row.tgrelid='control_plane.run_nodes'::regclass AND trigger_row.tgname='workflow_effect_records_guard'
            AND trigger_row.tgfoid='control_plane.guard_workflow_effect_records()'::regprocedure
            AND trigger_row.tgtype=31 AND trigger_row.tgenabled='O' AND trigger_row.tgattr::text=''
            AND trigger_row.tgqual IS NULL AND NOT trigger_row.tgisinternal AND NOT trigger_row.tgdeferrable) THEN
        RAISE EXCEPTION 'workflow effect trigger source changed';
    END IF;
    v_updated:=replace(v_source,$old$OR EXISTS (SELECT 1 FROM jsonb_array_elements(v_old_rejoins) item WHERE item->>'resultKey'=v_record->>'resultKey' AND item->>'operation'=v_record->>'operation')$old$,
        $new$OR EXISTS (SELECT 1 FROM jsonb_array_elements(v_old_rejoins) item
            WHERE item->>'resultKey'=v_record->>'resultKey' AND item->>'operation'=v_record->>'operation'
                AND item->>'runtimeRevisionId'=v_record->>'runtimeRevisionId' AND item->>'leaseId'=v_record->>'leaseId'
                AND item->'generation'=v_record->'generation' AND item->'runtimeAttempt'=v_record->'runtimeAttempt'
                AND item->'businessAttempt'=v_record->'businessAttempt')$new$);
    IF v_updated=v_source THEN RAISE EXCEPTION 'workflow effect uniqueness preimage missing'; END IF;
    EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.guard_workflow_effect_records() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS %L',v_updated);
END;
$migration$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_workflow_effect_records() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_effect_records() TO control_plane_runtime;
RESET ROLE;

-- +goose Down
SELECT 1;
