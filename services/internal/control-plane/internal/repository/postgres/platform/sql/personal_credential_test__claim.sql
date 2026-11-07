-- name: personal_credential_test__claim :exec
UPDATE control_plane.integration_personal_credential_tests
SET state='CLAIMED',version=version+1,generation=1,lease_ref=@lease_ref,fence_digest=@fence_digest,
 workload_instance=@instance,lease_expires_at=@expires_at
WHERE organization_id=@organization_id::uuid AND id=@id::uuid AND state='DUE' AND version=@version
