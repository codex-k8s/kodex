-- name: role_image_impact__search_items :many
SELECT item.ref
FROM control_plane.role_image_impact_items item
JOIN control_plane.role_image_impact_plans plan ON plan.id=item.plan_id
JOIN control_plane.runtime_environment_sets environment ON environment.organization_id=plan.organization_id AND environment.ref=item.snapshot->>'EnvironmentRef'
JOIN control_plane.projects project ON project.id=environment.project_id AND project.organization_id=plan.organization_id
LEFT JOIN control_plane.agents agent ON agent.organization_id=plan.organization_id AND agent.ref=item.snapshot->'Consumer'->>'AgentRef'
WHERE item.plan_id=@plan_id::uuid
 AND strpos(lower(environment.name||' '||environment.ref||' '||project.name||' '||project.ref||' '||COALESCE(agent.name,'')||' '||COALESCE(agent.ref,'')),lower(@query))>0
ORDER BY item.ref;
