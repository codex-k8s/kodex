-- name: email_authorization_insert :exec
INSERT INTO control_plane.email_authorizations(ref,organization_id,invocation_id,connection_test_id,
    source_ref,lease_ref,fence_digest,generation,semantic_input_digest,query_projection,decision_projection,expires_at)
VALUES($1,$2::uuid,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12)
ON CONFLICT(organization_id,source_ref,lease_ref,generation) DO NOTHING;
