-- name: email_receipt_update :exec
UPDATE control_plane.email_effect_receipts
SET outcome=$3,version=version+1,updated_at=clock_timestamp()
WHERE id=$1::uuid AND version=$2 AND outcome='UNKNOWN_OUTCOME';
