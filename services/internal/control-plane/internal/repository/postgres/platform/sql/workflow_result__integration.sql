-- name: workflow_result__integration :many
-- Единственный алгоритм находится в owner function миграции031; scope назначает caller.
SELECT proof.operation,proof.bounded_input,proof.resource_scope,proof.result_summary,
       proof.response_digest,proof.provider_effect_ref,proof.lineage_failure
FROM control_plane.workflow_receipt_lineage_proof(
    @organization_id::uuid,@node_id::uuid,@root_run_id::uuid,
    @runtime_revision_id::uuid,@lease_id::uuid,@receipt_ref) proof;
