UPDATE control_plane.provider_accounts SET version = version + 1
WHERE organization_id = $1::uuid AND ref = $2;
