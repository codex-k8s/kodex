-- name: artifact_revision_create :one
INSERT INTO control_plane.artifact_revisions(artifact_id,ref,revision,file_name,media_type,size_bytes,digest,
    source,scan_state,object_receipt_ref,preview_state,created_by,created_at)
VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::uuid,transaction_timestamp())
RETURNING id::text
