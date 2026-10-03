UPDATE control_plane.runtime_secrets
SET state = 'REVOKED'
WHERE organization_id = @organization_id::uuid AND ref = @secret_ref;
