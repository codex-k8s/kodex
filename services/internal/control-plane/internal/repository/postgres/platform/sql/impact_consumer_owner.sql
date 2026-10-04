-- name: impact_consumer_owner :one
WITH consumer AS (
 SELECT agent.organization_id,agent.project_id,
     CASE WHEN agent.project_id IS NULL AND agent.system_key='system-assistant' THEN 'ORGANIZATION'
          WHEN agent.project_id IS NOT NULL AND agent.system_key IS NULL THEN 'PROJECT' ELSE '' END AS scope_kind
 FROM control_plane.agents agent
 WHERE $2='AGENT' AND agent.organization_id=$1::uuid AND agent.ref=$3 AND agent.state<>'ARCHIVED'
 UNION ALL
 SELECT workflow.organization_id,workflow.project_id,'PROJECT'
 FROM control_plane.workflows workflow
 WHERE $2='WORKFLOW' AND workflow.organization_id=$1::uuid AND workflow.ref=$3 AND workflow.state<>'ARCHIVED'
 UNION ALL
 SELECT schedule.organization_id,schedule.project_id,'PROJECT'
 FROM control_plane.schedules schedule
 WHERE $2='SCHEDULE' AND schedule.organization_id=$1::uuid AND schedule.ref=$3 AND schedule.lifecycle_state<>'ARCHIVED'
)
SELECT consumer.scope_kind,organization.ref,COALESCE(project.ref,'')
FROM consumer
JOIN control_plane.organizations organization ON organization.id=consumer.organization_id
LEFT JOIN control_plane.projects project ON project.id=consumer.project_id AND project.organization_id=consumer.organization_id
WHERE consumer.scope_kind='ORGANIZATION' OR (consumer.scope_kind='PROJECT' AND project.lifecycle='ACTIVE');
