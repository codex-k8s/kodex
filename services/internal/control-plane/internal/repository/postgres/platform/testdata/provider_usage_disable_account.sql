UPDATE control_plane.provider_accounts
SET enabled=false, state='DISABLED', version=version+1
WHERE ref=$1;
