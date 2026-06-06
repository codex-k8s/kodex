-- +goose Up
ALTER TABLE platform_event_consumer_checkpoints
    ADD COLUMN IF NOT EXISTS retry_sequence_id bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS retry_attempt integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_error text NOT NULL DEFAULT '';

UPDATE platform_event_consumer_checkpoints
SET
    retry_sequence_id = COALESCE(retry_sequence_id, 0),
    retry_attempt = COALESCE(retry_attempt, 0),
    last_error = COALESCE(last_error, '');

ALTER TABLE platform_event_consumer_checkpoints
    ALTER COLUMN retry_sequence_id SET DEFAULT 0,
    ALTER COLUMN retry_sequence_id SET NOT NULL,
    ALTER COLUMN retry_attempt SET DEFAULT 0,
    ALTER COLUMN retry_attempt SET NOT NULL,
    ALTER COLUMN last_error SET DEFAULT '',
    ALTER COLUMN last_error SET NOT NULL;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'platform_event_consumer_checkpoints'::regclass
          AND conname = 'platform_event_consumer_retry_sequence_chk'
    ) THEN
        ALTER TABLE platform_event_consumer_checkpoints
            ADD CONSTRAINT platform_event_consumer_retry_sequence_chk CHECK (retry_sequence_id >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'platform_event_consumer_checkpoints'::regclass
          AND conname = 'platform_event_consumer_retry_attempt_chk'
    ) THEN
        ALTER TABLE platform_event_consumer_checkpoints
            ADD CONSTRAINT platform_event_consumer_retry_attempt_chk CHECK (retry_attempt >= 0);
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- Добавочная миграция только вперёд для исправления расхождения живой схемы.
-- Откат намеренно пустой, чтобы не удалять retry-состояние checkpoint.
