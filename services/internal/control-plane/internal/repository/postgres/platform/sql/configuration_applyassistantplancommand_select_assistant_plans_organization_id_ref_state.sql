-- name: configuration_applyassistantplancommand_select_assistant_plans_organization_id_ref_state :one
SELECT id::text,conversation_ref,operations,version FROM control_plane.assistant_plans WHERE organization_id=$1::uuid AND ref=$2 AND state='PROPOSED' FOR UPDATE
