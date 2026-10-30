-- Actual SSH extension проверяется целиком до обратной014 проекции.
DO $combined_effect$
DECLARE v_source text; v_extension text:=$combined_deployment$                    OR (requirement.value->>'Kind'='DEPLOYMENT' AND requirement.value->>'Operation'='ssh.compose.deploy'
                        AND invocation.operation='ssh.compose.deploy' AND invocation.resource_kind='SSH_TARGET' AND connection.definition_key='ssh-compose'
                        AND invocation.resource_scope->>'target_ref'=requirement.value->>'TargetRef'
                        AND invocation.ssh_compose_snapshot->'target'->>'ref'=requirement.value->>'TargetRef'
                        AND invocation.ssh_compose_snapshot->>'originRuntimeRevisionId'=revision.id::text
                        AND invocation.ssh_compose_snapshot->>'originLeaseId'=lease.id::text
                        AND invocation.ssh_compose_snapshot->'originGeneration'=to_jsonb(revision.generation)
                        AND invocation.ssh_compose_snapshot->>'deploymentRef'=invocation.ref
                        AND invocation.ssh_compose_snapshot->>'deploymentInputDigest'=invocation.input_digest
                        AND invocation.ssh_compose_snapshot->>'deploymentEffectKey'=invocation.effect_key
                        AND EXISTS (
                            SELECT 1 FROM jsonb_array_elements(origin.workflow_inputs) dependency
                            JOIN control_plane.run_nodes source ON source.id::text=dependency->>'sourceNodeId'
                                AND source.organization_id=origin.organization_id AND source.root_run_id=origin.root_run_id
                                AND source.workflow_step_origin_node_id IS NULL AND source.workflow_business_attempt=1
                                AND source.workflow_step_key=dependency->>'stepKey'
                            CROSS JOIN LATERAL jsonb_array_elements(source.workflow_attempts) accepted
                            CROSS JOIN LATERAL jsonb_array_elements(accepted->'results') result
                            JOIN control_plane.integration_effect_receipts merge_receipt ON merge_receipt.ref=result->>'receiptRef'
                                AND merge_receipt.organization_id=origin.organization_id
                            JOIN control_plane.integration_invocations merge_invocation ON merge_invocation.id=merge_receipt.invocation_id
                                AND merge_invocation.organization_id=origin.organization_id AND merge_invocation.state='SUCCEEDED'
                                AND merge_invocation.operation='github.pull_request.merge'
                                AND merge_invocation.effect_receipt_id=merge_receipt.id AND merge_invocation.effect_key=merge_receipt.effect_key
                                AND merge_invocation.input_digest=merge_receipt.input_digest
                            WHERE dependency->>'stepKey'=requirement.value->>'SubjectStepKey'
                                AND step.value->'DependsOn' ? (dependency->>'stepKey')
                                AND accepted=dependency->'receipt' AND accepted->>'accepted'='true'
                                AND result->>'kind'='INTEGRATION_RECEIPT' AND result->>'repository'=requirement.value->>'Repository'
                                AND result->>'headSha'=invocation.ssh_compose_snapshot->>'mergedSha'
                                AND merge_invocation.ref=invocation.ssh_compose_snapshot->>'mergeInvocationRef'

                        ))
                    $combined_deployment$;
BEGIN
 SELECT prosrc INTO STRICT v_source FROM pg_catalog.pg_proc
 WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure;
 IF (length(v_source)-length(replace(v_source,v_extension,'')))/length(v_extension)<>1
 OR md5(replace(v_source,v_extension,''))<>'7010547e11bc8d1e8f67ac56feaba030' THEN
 RAISE EXCEPTION 'combined SSH evidence guard changed';
 END IF;
END $combined_effect$;
