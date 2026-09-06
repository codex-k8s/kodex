-- name: memory_record_insert :one
INSERT INTO control_plane.memory_records(ref,organization_id,project_id,agent_id,created_by)
SELECT $2,$1::uuid,project.id,agent.id,$5::uuid FROM control_plane.projects project
LEFT JOIN control_plane.agents agent ON agent.organization_id=project.organization_id AND agent.project_id=project.id AND agent.ref=$4 AND agent.state<>'ARCHIVED'
WHERE project.organization_id=$1::uuid AND project.ref=$3 AND project.lifecycle='ACTIVE' AND ($4='' OR agent.id IS NOT NULL)
RETURNING id::text;
