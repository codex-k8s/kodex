-- name: runtime_materialization_expected_authority :one
SELECT root_run.initiated_by::text, COALESCE(revision.project_id::text, ''), revision.id::text
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id = lease.runtime_revision_id
JOIN control_plane.runs root_run ON root_run.id = revision.root_run_id
WHERE lease.ref = $1;
