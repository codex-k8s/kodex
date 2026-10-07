-- name: integration_grant_policy_credential_fixture :exec
WITH credential AS (
    INSERT INTO control_plane.integration_credential_revisions
      (ref,organization_id,connection_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256,created_by)
    SELECT 'icred_policy_fixture',organization_id,id,1,'kodex-system/kodex-integration-credentials#fixture',
           '60000000-0000-4000-8000-000000000001'::uuid,'1',repeat('a',64),created_by
    FROM control_plane.integration_connections WHERE ref=$1
    RETURNING id,connection_id
)
UPDATE control_plane.integration_connections connection
SET credential_revision_id=credential.id,state='CONNECTED',masked_credentials_state='CONFIGURED',version=version+1
FROM credential WHERE connection.id=credential.connection_id;
