-- Восстанавливает только ACTIVE bindings отдельной disposable организации после first-bound expiry.
UPDATE control_plane.access_bindings SET valid_until=NULL
WHERE organization_id=$1::uuid AND state='ACTIVE';
