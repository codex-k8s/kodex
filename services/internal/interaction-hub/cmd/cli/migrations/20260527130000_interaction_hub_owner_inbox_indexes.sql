-- +goose Up
CREATE INDEX interaction_hub_requests_target_refs_gin_idx
    ON interaction_hub_requests USING gin (target_refs);

CREATE INDEX interaction_hub_requests_context_refs_gin_idx
    ON interaction_hub_requests USING gin (context_refs);

-- +goose Down
DROP INDEX IF EXISTS interaction_hub_requests_context_refs_gin_idx;
DROP INDEX IF EXISTS interaction_hub_requests_target_refs_gin_idx;
