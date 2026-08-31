-- +goose Up
SET ROLE control_plane_owner;

CREATE INDEX attachment_set_items_artifact_revision
    ON control_plane.attachment_set_items (artifact_id, artifact_revision, attachment_set_id);
CREATE INDEX runs_active_artifact_retention
    ON control_plane.runs (id, session_id, created_at, input_attachment_set_id)
    WHERE state IN ('QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING');
CREATE INDEX session_turns_run_attachment
    ON control_plane.session_turns (run_id, attachment_set_id)
    WHERE run_id IS NOT NULL AND attachment_set_id IS NOT NULL;
CREATE INDEX runtime_revisions_root_snapshot
    ON control_plane.runtime_revisions (root_run_id);

GRANT SELECT (
    attachment_set_id, artifact_id, artifact_revision
) ON TABLE control_plane.attachment_set_items TO artifact_retention_runtime;
GRANT SELECT (
    id, session_id, input_attachment_set_id, state, created_at
) ON TABLE control_plane.runs TO artifact_retention_runtime;
GRANT SELECT (
    run_id, session_id, attachment_set_id, created_at
) ON TABLE control_plane.session_turns TO artifact_retention_runtime;
GRANT SELECT (
    root_run_id, safe_snapshot
) ON TABLE control_plane.runtime_revisions TO artifact_retention_runtime;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DROP INDEX control_plane.runtime_revisions_root_snapshot;
DROP INDEX control_plane.session_turns_run_attachment;
DROP INDEX control_plane.runs_active_artifact_retention;
DROP INDEX control_plane.attachment_set_items_artifact_revision;

REVOKE SELECT (
    attachment_set_id, artifact_id, artifact_revision
) ON TABLE control_plane.attachment_set_items FROM artifact_retention_runtime;
REVOKE SELECT (
    id, session_id, input_attachment_set_id, state, created_at
) ON TABLE control_plane.runs FROM artifact_retention_runtime;
REVOKE SELECT (
    run_id, session_id, attachment_set_id, created_at
) ON TABLE control_plane.session_turns FROM artifact_retention_runtime;
REVOKE SELECT (
    root_run_id, safe_snapshot
) ON TABLE control_plane.runtime_revisions FROM artifact_retention_runtime;

RESET ROLE;
