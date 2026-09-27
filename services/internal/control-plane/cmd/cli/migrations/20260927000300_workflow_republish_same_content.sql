-- +goose Up
SET ROLE control_plane_owner;

-- Возврат к прежней конфигурации создаёт новую версию с тем же digest.
-- Номер версии остаётся уникальным; digest подтверждает содержимое, а не историю.
ALTER TABLE control_plane.workflow_versions
    DROP CONSTRAINT workflow_versions_workflow_id_digest_key RESTRICT;
