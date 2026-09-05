-- name: email_credential_lock :one
SELECT id::text,version FROM control_plane.integration_connections
WHERE organization_id=$1::uuid AND ref=$2 AND definition_key='email' AND state<>'DELETED'
FOR UPDATE;
