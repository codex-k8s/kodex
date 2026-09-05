-- name: context_binding_skill_target :one
SELECT bundle.id::text,bundle.project_id::text,''::text,revision.id::text,revision.ref,revision.digest,
    bundle.state='ACTIVE' AND revision.state='PUBLISHED' AND revision.scan_state='CLEAN'
FROM control_plane.skill_bundles bundle
JOIN control_plane.skill_bundle_revisions revision ON revision.bundle_id=bundle.id AND revision.ref=$3
WHERE bundle.organization_id=$1::uuid AND bundle.ref=$2 FOR SHARE OF bundle,revision;
