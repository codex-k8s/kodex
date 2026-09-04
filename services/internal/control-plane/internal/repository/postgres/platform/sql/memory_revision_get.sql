-- name: memory_revision_get :one
SELECT control_plane.memory_revision_projection(revision.id)
FROM control_plane.memory_record_revisions revision
JOIN control_plane.memory_records record ON record.id=revision.record_id
WHERE record.organization_id=$1::uuid AND record.ref=$2 AND revision.ref=$3;
