-- name: platform__commands_changeagentbinding_append_knowledge_artifact :exec
UPDATE control_plane.agents
SET knowledge_artifact_refs=array_append(knowledge_artifact_refs, $3)
WHERE organization_id=$1::uuid
  AND ref=$2
  AND NOT ($3=ANY(knowledge_artifact_refs))
