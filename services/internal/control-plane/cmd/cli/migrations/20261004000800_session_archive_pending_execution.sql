-- +goose Up
SET ROLE control_plane_owner;

-- Новый Run имеет queued node раньше исполняемого session turn. Оба вида
-- server-owned работы одинаково запрещают snapshot/delete и требуют restore.
-- +goose StatementBegin
CREATE FUNCTION control_plane.session_archive_pending_execution(p_organization uuid, p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path = pg_catalog, control_plane AS $$
SELECT EXISTS (
    SELECT 1 FROM control_plane.session_turns turn
    WHERE turn.organization_id=p_organization AND turn.session_id=p_session
      AND turn.state IN ('QUEUED','RUNNING')
) OR EXISTS (
    SELECT 1 FROM control_plane.runs run
    JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
    JOIN control_plane.run_nodes node ON node.run_id=run.id AND node.organization_id=run.organization_id
    WHERE run.organization_id=p_organization AND run.session_id=p_session
      AND run.state IN ('QUEUED','RUNNING') AND root.state IN ('QUEUED','RUNNING')
      AND node.state IN ('QUEUED','RUNNING')
);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.session_archive_pending_execution(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.session_archive_pending_execution(uuid,uuid) TO control_plane_runtime;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'session archive pending execution is forward-only';
END $$;
-- +goose StatementEnd
