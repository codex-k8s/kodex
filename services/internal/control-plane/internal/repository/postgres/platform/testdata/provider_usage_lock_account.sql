SELECT organization_id::text
FROM control_plane.provider_accounts
WHERE ref=$1 AND enabled AND state='AUTHORIZED'
FOR UPDATE;
