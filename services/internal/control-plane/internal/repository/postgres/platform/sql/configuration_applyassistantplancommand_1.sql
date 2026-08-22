-- name: platform__configuration_applyassistantplancommand_1 :one
SELECT id::text,conversation_ref,operations,version FROM control_plane.assistant_plans WHERE organization_id=$1::uuid AND ref=$2 AND state='PROPOSED' FOR UPDATE
