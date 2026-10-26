-- name: ssh_compose__secret_reserve :one
UPDATE control_plane.runtime_secrets secret
SET purpose='SSH_COMPOSE',display_hint_prefix='',display_hint_suffix='',
    version=CASE WHEN purpose='SSH_COMPOSE' THEN version ELSE version+1 END,
    updated_at=CASE WHEN purpose='SSH_COMPOSE' THEN updated_at ELSE clock_timestamp() END
WHERE secret.organization_id=@organization_id::uuid AND secret.project_id=@project_id::uuid
  AND secret.ref=@secret_ref AND secret.state='ACTIVE'
  AND (secret.purpose='SSH_COMPOSE' OR (secret.display_hint_prefix='' AND secret.display_hint_suffix=''
    AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_secret_operations previous
      WHERE previous.secret_id=secret.id AND previous.terminal_secret_snapshot->'DisplayHint' IS NOT NULL
        AND previous.terminal_secret_snapshot->'DisplayHint'<>'null'::jsonb)))
  AND NOT EXISTS (
    SELECT 1 FROM control_plane.runtime_environment_versions environment,
      jsonb_array_elements(environment.secret_descriptors) descriptor
    WHERE environment.organization_id=secret.organization_id
      AND (COALESCE(descriptor->>'secret_ref',descriptor->>'secretRef')=secret.ref OR COALESCE(descriptor->>'secret_name',descriptor->>'secretName') IN (
        SELECT revision.secret_name FROM control_plane.runtime_secret_revisions revision WHERE revision.secret_id=secret.id))
  )
  AND NOT EXISTS (
    SELECT 1 FROM control_plane.runtime_revisions revision,
      jsonb_array_elements(COALESCE(revision.safe_snapshot->'secretProjections','[]'::jsonb)) projection
    WHERE revision.organization_id=secret.organization_id AND COALESCE(projection->>'secret_name',projection->>'secretName') IN (
      SELECT version.secret_name FROM control_plane.runtime_secret_revisions version WHERE version.secret_id=secret.id)
  )
  AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_secret_operations operation
    WHERE operation.secret_id=secret.id AND operation.kind='REVEAL')
RETURNING secret.id::text;
