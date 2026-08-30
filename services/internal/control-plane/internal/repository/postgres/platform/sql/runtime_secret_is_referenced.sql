-- name: runtime_secret_is_referenced :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.runtime_environment_sets environment
    JOIN control_plane.runtime_environment_versions version
      ON version.id = environment.current_version_id
    WHERE environment.organization_id = @organization_id::uuid
      AND version.secret_descriptors @> jsonb_build_array(jsonb_build_object('secret_ref', @secret_ref::text))
    UNION ALL
    SELECT 1
    FROM control_plane.runtime_revisions revision
    JOIN control_plane.runs run ON run.id = revision.run_id
    JOIN control_plane.runtime_environment_versions version
      ON version.id = revision.runtime_environment_version_id
    WHERE revision.organization_id = @organization_id::uuid
      AND run.state IN ('QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING')
      AND version.secret_descriptors @> jsonb_build_array(jsonb_build_object('secret_ref', @secret_ref::text))
);
