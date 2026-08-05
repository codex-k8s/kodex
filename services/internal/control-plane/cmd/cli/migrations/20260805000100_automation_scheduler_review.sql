-- +goose Up
CREATE TABLE control_plane.automation_scheduler_cursors (
    organization_id uuid NOT NULL,
    operation text NOT NULL CHECK (operation IN ('DUE', 'CLAIM')),
    last_project_id uuid,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, operation)
);
ALTER TABLE control_plane.automation_scheduler_cursors OWNER TO control_plane_owner;
ALTER TABLE control_plane.automation_scheduler_cursors ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.automation_scheduler_cursors FORCE ROW LEVEL SECURITY;
CREATE POLICY automation_scheduler_cursors_scope
    ON control_plane.automation_scheduler_cursors
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND (control_plane.runtime_scope()).project_id IS NULL
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND (control_plane.runtime_scope()).project_id IS NULL
    );
GRANT SELECT, INSERT, UPDATE ON control_plane.automation_scheduler_cursors TO control_plane_runtime;

ALTER TABLE control_plane.interaction_delivery_work
    ADD COLUMN notification_room_id uuid,
    ADD COLUMN notification_policy text,
    ADD COLUMN scheduled_outcome text,
    ADD CONSTRAINT interaction_delivery_schedule_policy_closed CHECK (
        notification_policy IS NULL OR notification_policy IN (
            'ALWAYS', 'ON_ACTION', 'ON_FAILURE', 'ON_ACTION_OR_FAILURE', 'AUDIT_ONLY'
        )
    ),
    ADD CONSTRAINT interaction_delivery_scheduled_outcome_closed CHECK (
        scheduled_outcome IS NULL OR scheduled_outcome IN (
            'no_action', 'action_taken', 'requires_human', 'failed'
        )
    ),
    ADD CONSTRAINT interaction_delivery_schedule_route_complete CHECK (
        (notification_policy IS NULL AND scheduled_outcome IS NULL AND notification_room_id IS NULL)
        OR (notification_policy IS NOT NULL AND scheduled_outcome IS NOT NULL AND notification_room_id IS NOT NULL)
    );

ALTER TABLE control_plane.schedule_occurrences
    ADD CONSTRAINT schedule_occurrences_scope_id_unique
    UNIQUE (organization_id, project_id, id);
ALTER TABLE control_plane.runtime_executions
    ADD COLUMN schedule_occurrence_id uuid,
    ADD CONSTRAINT runtime_execution_schedule_occurrence_scope_fk
    FOREIGN KEY (organization_id, project_id, schedule_occurrence_id)
    REFERENCES control_plane.schedule_occurrences (organization_id, project_id, id);

-- +goose Down
ALTER TABLE control_plane.runtime_executions
    DROP COLUMN schedule_occurrence_id;
ALTER TABLE control_plane.schedule_occurrences
    DROP CONSTRAINT schedule_occurrences_scope_id_unique;
ALTER TABLE control_plane.interaction_delivery_work
    DROP CONSTRAINT interaction_delivery_schedule_route_complete,
    DROP CONSTRAINT interaction_delivery_scheduled_outcome_closed,
    DROP CONSTRAINT interaction_delivery_schedule_policy_closed,
    DROP COLUMN scheduled_outcome,
    DROP COLUMN notification_policy,
    DROP COLUMN notification_room_id;
DROP TABLE control_plane.automation_scheduler_cursors;
