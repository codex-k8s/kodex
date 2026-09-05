-- name: runtime_secret_revision_retained :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.runtime_secrets secret
    JOIN control_plane.runtime_secret_revisions revision ON revision.secret_id=secret.id
    WHERE secret.organization_id=$1::uuid AND secret.id=$2::uuid AND secret.state='ACTIVE'
      AND revision.revision=$3::bigint AND revision.state='ACTIVE'
      AND (secret.current_revision=revision.revision OR EXISTS (
          SELECT 1 FROM control_plane.runtime_environment_versions environment_revision
          WHERE environment_revision.organization_id=secret.organization_id
            AND environment_revision.secret_descriptors @> jsonb_build_array(jsonb_build_object('secret_ref',secret.ref,'revision',revision.revision))
            AND (EXISTS (SELECT 1 FROM control_plane.runtime_environment_sets environment WHERE environment.current_version_id=environment_revision.id)
              OR EXISTS (SELECT 1 FROM control_plane.agent_runtime_environment_bindings binding WHERE binding.environment_version_id=environment_revision.id)
              OR EXISTS (SELECT 1 FROM control_plane.runtime_revisions runtime JOIN control_plane.runs run ON run.id=runtime.run_id
                  WHERE runtime.runtime_environment_version_id=environment_revision.id AND run.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')))
      ))
);
