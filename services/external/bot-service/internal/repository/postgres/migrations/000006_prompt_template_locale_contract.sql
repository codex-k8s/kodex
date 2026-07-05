-- +goose Up
-- Historical migration kept for ordering. Prompt locale rules now live in
-- embedded Markdown seed files and in runtime prompt contract rendering.
select 1;

-- +goose Down
select 1;
