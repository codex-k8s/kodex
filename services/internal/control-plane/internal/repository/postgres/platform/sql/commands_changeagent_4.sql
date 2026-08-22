-- name: platform__commands_changeagent_4 :one
SELECT p.ref,a.runtime_key,r.name,r.provider,r.model,r.runtime_revision,a.capabilities,a.knowledge_artifact_refs FROM control_plane.agents a JOIN control_plane.projects p ON p.id=a.project_id JOIN control_plane.runtime_profiles r ON r.stable_key=a.runtime_key WHERE a.ref=$1
