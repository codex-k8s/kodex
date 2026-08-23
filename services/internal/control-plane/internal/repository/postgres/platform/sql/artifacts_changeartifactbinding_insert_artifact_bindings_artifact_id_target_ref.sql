-- name: artifacts_changeartifactbinding_insert_artifact_bindings_artifact_id_target_ref :exec
INSERT INTO control_plane.artifact_bindings(artifact_id,target_kind,target_ref,created_by) SELECT $1::uuid,'KNOWLEDGE',a.ref,$3::uuid FROM control_plane.agents a WHERE a.organization_id=$2::uuid AND a.project_id=$4::uuid AND a.ref=$5 ON CONFLICT DO NOTHING
