-- +goose Up
-- owner_run_graph_process_ids выполняет bounded breadth-first traversal до
-- материализации owner lineage. Очередь и visited set никогда не превышают
-- caller-supplied hard cap; tenant/owner predicates остаются внутри функции.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE INDEX resources_owner_process_parent_idx
    ON control_plane.resources (
        organization_id,
        project_id,
        owner_actor_id,
        (nullif(spec ->> 'parentProcessRunId', '')::uuid),
        id
    )
    WHERE kind = 'PROCESS_RUN' AND state <> 'DELETED';

CREATE FUNCTION control_plane.owner_run_graph_process_ids(
    p_organization_id uuid,
    p_project_id uuid,
    p_actor_id uuid,
    p_process_run_id uuid,
    p_hard_limit integer
)
RETURNS TABLE (process_id uuid, traversal_ordinal integer, graph_overflow boolean)
LANGUAGE plpgsql
STABLE
SECURITY INVOKER
SET search_path = pg_catalog, control_plane
AS $$
DECLARE
    ancestry uuid[] := ARRAY[]::uuid[];
    visited uuid[] := ARRAY[]::uuid[];
    pending uuid[] := ARRAY[]::uuid[];
    child_ids uuid[] := ARRAY[]::uuid[];
    current_id uuid := p_process_run_id;
    parent_id uuid;
    remaining integer;
    depth integer;
    root_found boolean := false;
    overflow_found boolean := false;
BEGIN
    IF p_hard_limit < 1 OR p_hard_limit > 1001 THEN
        RETURN;
    END IF;

    FOR depth IN 1..64 LOOP
        IF current_id = ANY(ancestry) THEN
            RETURN;
        END IF;
        SELECT nullif(resource.spec ->> 'parentProcessRunId', '')::uuid
        INTO parent_id
        FROM control_plane.resources AS resource
        WHERE resource.organization_id = p_organization_id
          AND resource.project_id = p_project_id
          AND resource.owner_actor_id = p_actor_id
          AND resource.id = current_id
          AND resource.kind = 'PROCESS_RUN'
          AND resource.state <> 'DELETED';
        IF NOT FOUND THEN
            RETURN;
        END IF;
        ancestry := array_append(ancestry, current_id);
        IF parent_id IS NULL THEN
            root_found := true;
            EXIT;
        END IF;
        current_id := parent_id;
    END LOOP;
    IF NOT root_found THEN
        RETURN;
    END IF;

    pending := ARRAY[current_id];
    WHILE cardinality(pending) > 0 AND cardinality(visited) < p_hard_limit LOOP
        current_id := pending[1];
        pending := pending[2:cardinality(pending)];
        IF current_id = ANY(visited) THEN
            CONTINUE;
        END IF;
        visited := array_append(visited, current_id);
        remaining := p_hard_limit - cardinality(visited) - cardinality(pending);
        IF remaining > 0 THEN
            SELECT coalesce(array_agg(child.id ORDER BY child.id), ARRAY[]::uuid[])
            INTO child_ids
            FROM (
                SELECT resource.id
                FROM control_plane.resources AS resource
                WHERE resource.organization_id = p_organization_id
                  AND resource.project_id = p_project_id
                  AND resource.owner_actor_id = p_actor_id
                  AND resource.kind = 'PROCESS_RUN'
                  AND resource.state <> 'DELETED'
                  AND nullif(resource.spec ->> 'parentProcessRunId', '')::uuid = current_id
                  AND NOT (resource.id = ANY(visited))
                  AND NOT (resource.id = ANY(pending))
                ORDER BY resource.id
                LIMIT remaining
            ) AS child;
            pending := pending || child_ids;
        END IF;
    END LOOP;

    overflow_found := cardinality(pending) > 0;
    IF NOT overflow_found THEN
        SELECT EXISTS (
            SELECT 1
            FROM control_plane.resources AS child
            WHERE child.organization_id = p_organization_id
              AND child.project_id = p_project_id
              AND child.owner_actor_id = p_actor_id
              AND child.kind = 'PROCESS_RUN'
              AND child.state <> 'DELETED'
              AND nullif(child.spec ->> 'parentProcessRunId', '')::uuid = ANY(visited)
              AND NOT (child.id = ANY(visited))
        )
        INTO overflow_found;
    END IF;

    RETURN QUERY
    SELECT item.process_id, item.ordinality::integer, overflow_found
    FROM unnest(visited) WITH ORDINALITY AS item(process_id, ordinality)
    ORDER BY item.ordinality;
END;
$$;

ALTER FUNCTION control_plane.owner_run_graph_process_ids(uuid, uuid, uuid, uuid, integer)
    OWNER TO control_plane_owner;
REVOKE ALL ON FUNCTION control_plane.owner_run_graph_process_ids(uuid, uuid, uuid, uuid, integer)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.owner_run_graph_process_ids(uuid, uuid, uuid, uuid, integer)
    TO control_plane_runtime;

UPDATE control_plane.schema_state
SET version = 20260809026310, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
DO $$
BEGIN
    RAISE EXCEPTION 'migration 20260809026310 is forward-only: owner lineage read contract cannot be discarded';
END;
$$;
