-- name: configuration_source__retire_draft :exec
UPDATE control_plane.managed_configuration_revisions SET state='DISCARDED'
WHERE configuration_set_id=$1::uuid AND state IN ('DRAFT','VALID');
