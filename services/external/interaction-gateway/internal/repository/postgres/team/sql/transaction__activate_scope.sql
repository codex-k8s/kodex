-- name: transaction__activate_scope :exec
-- params: @arg1,@arg2,@arg3
SELECT interaction_gateway_activate_runtime_context(@arg1::uuid, @arg2::uuid, @arg3::text);
