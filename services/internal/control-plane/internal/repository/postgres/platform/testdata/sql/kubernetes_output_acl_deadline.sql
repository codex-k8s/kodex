-- Все строки принадлежат отдельной disposable организации этого теста.
UPDATE control_plane.access_bindings SET valid_until=$2
WHERE organization_id=$1::uuid AND state='ACTIVE';
