-- name: email_mailbox_connection_apply :exec
UPDATE control_plane.integration_connections SET public_configuration=
    jsonb_set(jsonb_set(public_configuration,'{mailbox_id}',to_jsonb($4::text)),'{from_address}',to_jsonb($5::text)),
    updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND id=$2::uuid AND version=$3;
