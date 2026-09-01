-- name: configuration_updateassistantplandraft_update_plan :exec
UPDATE control_plane.assistant_plans
SET summary=$2,operations=$3,state='DRAFT',current_revision=$4,validated_revision=NULL,
    content_digest=$5,validation_problems='{}',validated_at=NULL,version=version+1
WHERE id=$1::uuid
