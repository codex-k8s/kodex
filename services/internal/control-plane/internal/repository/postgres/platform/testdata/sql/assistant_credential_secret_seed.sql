WITH secret AS (
  INSERT INTO control_plane.runtime_secrets
    (ref, organization_id, project_id, scope_kind, namespace, name, value_type,
     state, current_revision, created_by)
  VALUES
    (@secret_ref, @organization_id::uuid, NULLIF(@project_id, '')::uuid,
     @scope_kind, 'kodex-runtime', @name, 'STRING', 'ACTIVE', 1, @actor_id::uuid)
  RETURNING id, ref
), revision AS (
  INSERT INTO control_plane.runtime_secret_revisions
    (ref, secret_id, revision, namespace, secret_name, secret_key, secret_uid,
     secret_resource_version, content_sha256)
  SELECT @revision_ref, secret.id, 1, 'kodex-runtime', @secret_name,
         'value', @secret_uid, '111', @content_sha256
  FROM secret
  RETURNING secret_id, revision, namespace, secret_name, secret_key, secret_uid,
            secret_resource_version, content_sha256
)
SELECT secret.ref, revision.revision, revision.namespace, revision.secret_name,
       revision.secret_key, revision.secret_uid, revision.secret_resource_version,
       revision.content_sha256
FROM secret JOIN revision ON revision.secret_id = secret.id;
