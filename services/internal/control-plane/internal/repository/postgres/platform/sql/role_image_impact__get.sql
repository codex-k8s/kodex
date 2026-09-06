-- name: role_image_impact__get :one
SELECT plan.id::text, plan.snapshot, plan.version, plan.state, plan.created_at, plan.expires_at,
       plan.digest, plan.configuration_id::text, plan.revision_id::text, plan.artifact_id::text
FROM control_plane.role_image_impact_plans plan
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=plan.configuration_id
 AND configuration.organization_id=plan.organization_id
JOIN control_plane.managed_configuration_revisions revision ON revision.id=plan.revision_id
 AND revision.organization_id=plan.organization_id AND revision.configuration_set_id=configuration.id
JOIN control_plane.image_artifacts artifact ON artifact.id=plan.artifact_id AND artifact.organization_id=plan.organization_id
WHERE plan.organization_id=@organization_id::uuid AND plan.actor_id=@actor_id::uuid AND plan.ref=@plan_ref;
