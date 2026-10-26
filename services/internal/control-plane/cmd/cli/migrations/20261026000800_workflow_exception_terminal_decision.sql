-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';

-- Исправляется только классификация terminal episode. Применённая006 неизменна;
-- системный FAILED/expiry не выдаётся за человеческий REJECT.
-- +goose StatementBegin
DO $migration$
DECLARE v_source text; v_updated text;
BEGIN
    SELECT prosrc INTO STRICT v_source FROM pg_proc
    WHERE oid='control_plane.guard_workflow_exception_episodes()'::regprocedure;
    IF md5(v_source) IS DISTINCT FROM 'ae8842f72328d4b91f6d9fa19227c7c4' THEN
        RAISE EXCEPTION 'workflow exception terminal guard source changed';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger trigger_row
        WHERE trigger_row.tgrelid='control_plane.runs'::regclass AND trigger_row.tgname='workflow_exception_episodes_guard'
            AND trigger_row.tgfoid='control_plane.guard_workflow_exception_episodes()'::regprocedure
            AND trigger_row.tgtype=23 AND trigger_row.tgenabled='O' AND trigger_row.tgattr::text=''
            AND trigger_row.tgqual IS NULL AND NOT trigger_row.tgisinternal AND NOT trigger_row.tgdeferrable) THEN
        RAISE EXCEPTION 'workflow exception episode trigger source changed';
    END IF;
    v_updated:=replace(v_source,$old$CASE WHEN NEW.state='FAILED' THEN 'REJECTED' ELSE 'CANCELLED' END$old$,$new$CASE WHEN EXISTS (
                    SELECT 1 FROM control_plane.owner_gates decision_gate
                    WHERE decision_gate.id::text=episode->>'gateId' AND decision_gate.ref=episode->>'gateRef'
                        AND decision_gate.organization_id=NEW.organization_id AND decision_gate.root_run_id=NEW.id
                        AND decision_gate.project_id=NEW.project_id
                        AND decision_gate.state='REJECTED' AND decision_gate.decision='REJECT'
                ) THEN 'REJECTED' ELSE 'CANCELLED' END$new$);
    IF v_updated=v_source THEN RAISE EXCEPTION 'workflow exception terminal guard preimage missing'; END IF;
    v_updated:=replace(v_updated,$before$BEGIN
    IF TG_OP='INSERT' THEN$before$,$after$BEGIN
    IF TG_OP='DELETE' THEN
        IF OLD.workflow_exception_history<>'[]'::jsonb OR OLD.workflow_exception IS NOT NULL THEN
            IF NOT control_plane.project_purge_authorized() OR NOT EXISTS (
                SELECT 1 FROM control_plane.project_purge_targets target
                JOIN control_plane.project_purge_context context USING (transaction_id)
                WHERE target.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
                    AND target.relation_id='control_plane.runs'::regclass AND target.row_key=jsonb_build_array(OLD.id)
            ) THEN
                RAISE EXCEPTION 'workflow exception history requires authorized project purge'
                    USING ERRCODE='23514', CONSTRAINT='workflow_exception_delete_authority';
            END IF;
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP='INSERT' THEN$after$);
    EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.guard_workflow_exception_episodes() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS %L',v_updated);
END $migration$;
-- +goose StatementEnd
DROP TRIGGER workflow_exception_episodes_guard ON control_plane.runs;
CREATE TRIGGER workflow_exception_episodes_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.runs
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workflow_exception_episodes();
REVOKE ALL ON FUNCTION control_plane.guard_workflow_exception_episodes() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_exception_episodes() TO control_plane_runtime;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'workflow exception terminal guard migration is forward-only'; END $$;
-- +goose StatementEnd
