SELECT id::text FROM control_plane.provider_accounts
WHERE organization_id = $1::uuid AND ref = $2 AND state = 'AUTHORIZED' AND enabled;
