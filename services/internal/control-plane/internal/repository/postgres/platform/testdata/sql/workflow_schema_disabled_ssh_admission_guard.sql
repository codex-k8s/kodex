-- Изменяется только отдельная disposable schema clone.
ALTER TABLE control_plane.integration_invocations DISABLE TRIGGER ssh_compose_admission_block_immutable;
