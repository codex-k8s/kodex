-- Отдельная disposable metadata, без network/provider/Secret чтения.
WITH credential AS (
    INSERT INTO control_plane.integration_credential_revisions
      (ref,organization_id,connection_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256,created_by)
    SELECT 'icr_startup_'||ref,organization_id,id,1,'kodex-system/synthetic#api_key',
      '60000000-0000-4000-8000-000000000001'::uuid,'1',repeat('a',64),created_by
    FROM control_plane.integration_connections WHERE ref=@connection_ref
    RETURNING id,connection_id
)
UPDATE control_plane.integration_connections c
SET credential_revision_id=credential.id,state='CONNECTED',masked_credentials_state='CONFIGURED',version=version+1
FROM credential WHERE c.id=credential.connection_id;
