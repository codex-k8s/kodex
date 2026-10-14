-- Новая synthetic credential revision исключает прежний receipt по pin,
-- но не меняет его immutable snapshot, terminal state или время.
WITH credential AS (
    INSERT INTO control_plane.integration_credential_revisions
      (ref,organization_id,connection_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256,created_by)
    SELECT 'icr_readback_new_'||ref,organization_id,id,2,'kodex-system/synthetic#api_key',
      '60000000-0000-4000-8000-000000000002'::uuid,'2',repeat('b',64),created_by
    FROM control_plane.integration_connections WHERE ref=@connection_ref
    RETURNING id,connection_id
)
UPDATE control_plane.integration_connections c
SET credential_revision_id=credential.id,version=version+1
FROM credential WHERE c.id=credential.connection_id;
