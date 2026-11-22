-- name: personal_credential_test__begin :exec
UPDATE control_plane.integration_personal_credential_tests SET state='PROBING',version=version+1,probe_admitted_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@id::uuid AND state='CLAIMED' AND version=@version
