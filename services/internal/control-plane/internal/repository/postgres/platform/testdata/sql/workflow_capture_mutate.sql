-- name: workflow_capture_mutate :exec
-- Только отрицательные проверки в rollback-only транзакциях одноразовой БД.
UPDATE control_plane.run_nodes SET
    workflow_captured_artifacts=CASE @mutation
        WHEN 'clear' THEN NULL
        WHEN 'empty-before-completion' THEN jsonb_build_object(
            'schema','kodex.workflow-capture.v1','runtimeRevisionId',@revision_id::text,'leaseId',@lease_id::text,
            'generation',@generation::bigint,'turnId',turn_id::text,'runtimeAttempt',attempt,
            'businessAttempt',workflow_business_attempt,'artifacts','[]'::jsonb)
        WHEN 'replace-batch' THEN jsonb_set(workflow_captured_artifacts,'{artifacts}','[]'::jsonb)
        ELSE workflow_captured_artifacts END,
    workflow_business_attempt=CASE WHEN @mutation='business-attempt' THEN 2 ELSE workflow_business_attempt END,
    attempt=CASE WHEN @mutation='runtime-attempt' THEN attempt+1 ELSE attempt END,
    state=CASE WHEN @mutation='rewind' THEN 'PLANNED' ELSE state END,
    materialization_state=CASE WHEN @mutation='rewind' THEN 'PLANNED' ELSE materialization_state END,
    turn_id=CASE WHEN @mutation='rewind' THEN NULL ELSE turn_id END
WHERE ref=@node_ref;
