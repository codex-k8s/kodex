-- name: artifacts_impact :one
WITH target_artifact AS (
    SELECT artifact.id, artifact.version, artifact.lifecycle_state
    FROM control_plane.artifacts artifact
    WHERE artifact.organization_id = @organization_id::uuid
      AND artifact.ref = @artifact_ref
), usage_run_ids AS (
    SELECT run.id AS run_id
    FROM target_artifact artifact
    JOIN control_plane.attachment_set_items item ON item.artifact_id = artifact.id
    JOIN control_plane.runs run ON run.input_attachment_set_id = item.attachment_set_id
    WHERE run.state IN ('QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING')
    UNION
    SELECT run.id AS run_id
    FROM target_artifact artifact
    JOIN control_plane.attachment_set_items item ON item.artifact_id = artifact.id
    JOIN control_plane.session_turns turn ON turn.attachment_set_id = item.attachment_set_id
    JOIN control_plane.runs run ON run.id = turn.run_id
    WHERE run.state IN ('QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING')
), active_runs AS (
    SELECT run.ref, run.title, run.state, COALESCE(project.ref, '') AS project_ref,
           run.created_at
    FROM usage_run_ids usage
    JOIN control_plane.runs run ON run.id = usage.run_id
    LEFT JOIN control_plane.projects project ON project.id = run.project_id
)
SELECT artifact.id::text,
       artifact.version,
       artifact.lifecycle_state,
       (SELECT count(*) FROM control_plane.artifact_bindings binding
        WHERE binding.artifact_id = artifact.id),
       (SELECT count(*) FROM control_plane.attachment_set_items item
        WHERE item.artifact_id = artifact.id),
       (SELECT count(*) FROM active_runs),
       COALESCE((
           SELECT jsonb_agg(jsonb_build_object(
               'runRef', bounded.ref,
               'title', bounded.title,
               'state', bounded.state,
               'projectRef', bounded.project_ref
           ) ORDER BY bounded.created_at DESC, bounded.ref DESC)
           FROM (
               SELECT ref, title, state, project_ref, created_at
               FROM active_runs
               ORDER BY created_at DESC, ref DESC
               LIMIT 21
           ) bounded
       ), '[]'::jsonb)
FROM target_artifact artifact;
