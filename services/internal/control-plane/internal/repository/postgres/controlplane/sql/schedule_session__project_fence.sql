-- name: ScheduleSessionProjectFence
-- Тот же project graph key использует resource__insert.sql для SESSION.
-- Раннее получение fence делает последующее candidate read авторитетным.
SELECT pg_advisory_xact_lock(hashtextextended(
    @organization_id::text || ':' || @project_id::text,
    0
));
