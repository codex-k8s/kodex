-- name: runtime_catalog__native_model_compatible :one
SELECT NOT EXISTS (
 SELECT 1 FROM control_plane.session_storage storage
 LEFT JOIN control_plane.runtime_revisions revision
   ON revision.id = storage.runtime_revision_id
  AND revision.organization_id = storage.organization_id
  AND revision.session_id = storage.session_id
 WHERE storage.organization_id = $1::uuid AND storage.session_id = $2::uuid
   AND storage.native_checkpoint IS NOT NULL
   AND (revision.safe_snapshot->>'runtimeModel' IS DISTINCT FROM $3
     OR revision.safe_snapshot->>'runtimeKey' IS DISTINCT FROM $4)
);
