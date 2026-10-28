-- Изменяется только отдельная disposable schema clone.
ALTER TABLE control_plane.integration_invocations DROP CONSTRAINT ssh_compose_admission_block_shape;
