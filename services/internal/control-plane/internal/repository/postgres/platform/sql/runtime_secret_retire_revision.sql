-- name: runtime_secret_retire_revision :exec
UPDATE control_plane.runtime_secret_revisions SET state='RETIRED' WHERE secret_id=$1::uuid AND revision=$2::bigint;
