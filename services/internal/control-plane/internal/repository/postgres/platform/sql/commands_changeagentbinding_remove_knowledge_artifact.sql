-- name: platform__commands_changeagentbinding_remove_knowledge_artifact :exec
UPDATE control_plane.agents
SET knowledge_artifact_refs=array_remove(knowledge_artifact_refs, $3)
WHERE organization_id=$1::uuid
  AND ref=$2
