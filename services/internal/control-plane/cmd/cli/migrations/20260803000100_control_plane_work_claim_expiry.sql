-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Применённая 20260731000500 остаётся неизменной. Эта forward-only migration
-- одинаково обновляет fresh-install и upgrade-path eligibility WorkClaim.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.work_claim_graph_is_active(
    claim control_plane.resources
) RETURNS boolean
LANGUAGE sql
STABLE
SECURITY INVOKER
SET search_path = pg_catalog, control_plane
AS $function$
    SELECT claim.kind = 'WORK_CLAIM'
       AND claim.state = 'ACTIVE'
       AND (claim.spec ->> 'expiresAt')::timestamptz > statement_timestamp()
       AND EXISTS (
            SELECT 1
              FROM control_plane.resources AS session
             WHERE session.id = (claim.spec ->> 'sessionId')::uuid
               AND session.organization_id = claim.organization_id
               AND session.project_id = claim.project_id
               AND session.kind = 'SESSION'
               AND session.state = 'ACTIVE'
               AND session.owner_actor_id = claim.owner_actor_id
       )
       AND EXISTS (
            SELECT 1
              FROM control_plane.resources AS turn
              JOIN control_plane.turn_attempts AS attempt
                ON attempt.turn_id = turn.id
               AND attempt.attempt = (claim.spec ->> 'attempt')::integer
               AND attempt.state IN ('QUEUED', 'CLAIMED')
               AND attempt.finished_at IS NULL
               AND attempt.authority_generation =
                   (claim.spec ->> 'authorityGeneration')::bigint
             WHERE turn.id = (claim.spec ->> 'turnId')::uuid
               AND turn.organization_id = claim.organization_id
               AND turn.project_id = claim.project_id
               AND turn.kind = 'TURN'
               AND turn.state NOT IN (
                    'SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DELETED'
               )
               AND turn.owner_actor_id = claim.owner_actor_id
               AND turn.spec ->> 'sessionId' = claim.spec ->> 'sessionId'
               AND turn.spec ->> 'processRunId' = claim.spec ->> 'processRunId'
               AND turn.spec ->> 'attempt' = claim.spec ->> 'attempt'
       )
       AND EXISTS (
            SELECT 1
              FROM control_plane.resources AS process
             WHERE process.id = (claim.spec ->> 'processRunId')::uuid
               AND process.organization_id = claim.organization_id
               AND process.project_id = claim.project_id
               AND process.kind = 'PROCESS_RUN'
               AND process.state NOT IN (
                    'SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DELETED'
               )
               AND process.owner_actor_id = claim.owner_actor_id
               AND (
                    (
                        coalesce(process.spec ->> 'parentProcessRunId', '') = ''
                        AND process.spec ->> 'rootSessionId' =
                            claim.spec ->> 'sessionId'
                        AND process.spec ->> 'rootTurnId' =
                            claim.spec ->> 'turnId'
                        AND process.spec ->> 'rootAttempt' =
                            claim.spec ->> 'attempt'
                    )
                    OR (
                        coalesce(process.spec ->> 'parentProcessRunId', '') <> ''
                        AND process.spec ->> 'targetSessionId' =
                            claim.spec ->> 'sessionId'
                        AND process.spec ->> 'targetTurnId' =
                            claim.spec ->> 'turnId'
                        AND process.spec ->> 'targetAttempt' =
                            claim.spec ->> 'attempt'
                    )
               )
       )
$function$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.work_claim_graph_is_active(
    control_plane.resources
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.work_claim_graph_is_active(
    control_plane.resources
) TO control_plane_runtime, control_plane_owner;

-- +goose StatementBegin
DO $readback$
DECLARE
    function_oid regprocedure :=
        'control_plane.work_claim_graph_is_active(control_plane.resources)'::regprocedure;
    definition text;
BEGIN
    SELECT pg_catalog.pg_get_functiondef(function_oid) INTO definition;
    IF position('statement_timestamp()' IN definition) = 0 THEN
        RAISE EXCEPTION 'work_claim_graph_is_active expiry predicate is absent';
    END IF;
    IF EXISTS (
        SELECT 1
          FROM pg_catalog.pg_proc AS procedure
          CROSS JOIN LATERAL pg_catalog.aclexplode(
              coalesce(
                  procedure.proacl,
                  pg_catalog.acldefault('f', procedure.proowner)
              )
          ) AS privilege
         WHERE procedure.oid = function_oid::oid
           AND privilege.grantee = 0
           AND privilege.privilege_type = 'EXECUTE'
    ) THEN
        RAISE EXCEPTION 'PUBLIC can execute work_claim_graph_is_active';
    END IF;
    IF NOT pg_catalog.has_function_privilege(
        'control_plane_runtime', function_oid, 'EXECUTE'
    ) OR NOT pg_catalog.has_function_privilege(
        'control_plane_owner', function_oid, 'EXECUTE'
    ) THEN
        RAISE EXCEPTION 'work_claim_graph_is_active grants are incomplete';
    END IF;
END
$readback$;
-- +goose StatementEnd

UPDATE control_plane.schema_state
SET version = 20260803000100, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260803000100 is forward-only: expired work claim authority cannot be reopened'
        USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
