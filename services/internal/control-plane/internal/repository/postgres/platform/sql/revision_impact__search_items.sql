-- name: revision_impact__search_items :many
SELECT item.ref
FROM control_plane.revision_impact_items item
JOIN control_plane.revision_impact_plans plan ON plan.id=item.plan_id
LEFT JOIN control_plane.agents agent ON agent.organization_id=plan.organization_id AND agent.ref=item.snapshot->>'ConsumerRef' AND item.snapshot->>'ConsumerKind' IN ('AGENT','AGENT_CONTINUATION')
LEFT JOIN control_plane.workflows workflow ON workflow.organization_id=plan.organization_id AND workflow.ref=item.snapshot->>'ConsumerRef' AND item.snapshot->>'ConsumerKind'='WORKFLOW'
LEFT JOIN control_plane.schedules schedule ON schedule.organization_id=plan.organization_id AND schedule.ref=item.snapshot->>'ConsumerRef' AND item.snapshot->>'ConsumerKind'='SCHEDULE'
LEFT JOIN control_plane.projects project ON project.ref=item.snapshot->>'ProjectRef' AND project.organization_id=plan.organization_id
WHERE item.plan_id=@plan_id::uuid
 AND strpos(lower(COALESCE(agent.name,workflow.name,schedule.name,'')||' '||COALESCE(item.snapshot->>'ConsumerRef','')||' '||COALESCE(project.name,'')||' '||COALESCE(project.ref,'')||' '||COALESCE(item.snapshot->>'BindingRef','')),lower(@query))>0
ORDER BY item.ref;
