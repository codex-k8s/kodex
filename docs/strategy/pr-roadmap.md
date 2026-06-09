# PR Roadmap

Цель - уложить реализацию в максимум 10 кодовых PR после согласования структуры проекта. Документационный PR с этой стратегией считается подготовительным и не входит в лимит кодовых PR.

## Подготовительный PR: strategy

Содержимое:

- vision;
- architecture;
- deployment plan;
- roadmap;
- open decisions;
- root `AGENTS.md`.

Ручная проверка:

- прочитать документы;
- проверить зафиксированные решения;
- утвердить структуру проекта и порядок PR.

## Кодовый PR 1: Mattermost bootstrap

Содержимое:

- структура репозитория;
- env validator;
- Kubernetes manifests/scripts для Mattermost single-server;
- PostgreSQL/PVC/Ingress;
- read-only smoke.

Ручная проверка:

- запустить preflight;
- после команды владельца раскатить Mattermost на сервер;
- открыть Mattermost по HTTPS;
- создать/проверить owner/admin пользователя.

## Кодовый PR 2: bot-service skeleton

Содержимое:

- backend service;
- config loader без печати секретов;
- `/healthz`;
- Mattermost bot token config;
- `/agents status`;
- создание дефолтных Mattermost-каналов;
- Kubernetes manifests для bot-service.

Ручная проверка:

- deploy bot-service;
- выполнить `/agents status`;
- проверить наличие `agents-control`, `agents-runs`, `agent-alerts`;
- увидеть thread-safe ответ от бота.

## Кодовый PR 3: storage and admin commands

Содержимое:

- PostgreSQL migrations;
- repositories;
- credentials metadata;
- OpenAI accounts;
- device-code authorization session для OpenAI accounts;
- agent profiles;
- agent profile `config.toml` overlays;
- Mattermost project/repo channel bindings;
- audit events;
- команды `/agents repo add`, `/agents token check`, `/agents openai auth`, `/agents profile list`.

Ручная проверка:

- добавить тестовый repo;
- авторизовать или проверить OpenAI account без вывода токенов;
- создать канал repo/project;
- проверить credential status без вывода значений;
- увидеть audit event.

## Кодовый PR 4: GitHub adapter

Содержимое:

- проверка repo access;
- branch/PR operations;
- чтение PR status/reviews/comments;
- webhook endpoint и HMAC check;
- safe GitHub smoke.

Ручная проверка:

- проверить доступ к repo;
- создать тестовый branch/PR или dry-run preflight;
- убедиться, что webhook reject работает без корректной подписи.

## Кодовый PR 5: Kubernetes runner foundation

Содержимое:

- runtime module на client-go;
- создание PVC и Job/Pod;
- ServiceAccount/RBAC;
- сбор pod status/log tail;
- cleanup policy.

Ручная проверка:

- запустить test runner job;
- увидеть PVC, pod status и завершение;
- проверить, что секреты не печатаются.

## Кодовый PR 6: Codex developer agent

Содержимое:

- runner image с Codex CLI;
- `codex exec --json`;
- выбор OpenAI account по agent profile/session;
- рендер `CODEX_HOME` и `config.toml` overlay;
- MCP binding smoke на безопасном примере;
- checkout repo;
- branch/commit/push/PR prompt contract;
- artifact parser.

Ручная проверка:

- запустить developer run на безопасной документационной задаче;
- получить PR;
- увидеть Mattermost thread с summary и ссылкой.

## Кодовый PR 7: reviewer agent

Содержимое:

- reviewer profile;
- PR diff/review prompt;
- GitHub review/comment/approval/request changes;
- status sync в Mattermost.

Ручная проверка:

- запустить review по существующему PR;
- получить approval или request changes;
- увидеть решение в thread.

## Кодовый PR 8: developer-review-loop

Содержимое:

- flow state machine;
- автоматический запуск reviewer после PR;
- fix prompt при request changes;
- лимит до трех попыток;
- blocked escalation.

Ручная проверка:

- запустить полный flow;
- проверить request changes -> fix -> repeat review;
- проверить блокировку после лимита на искусственном сценарии.

## Кодовый PR 9: owner gate and actions

Содержимое:

- Mattermost interactive buttons;
- owner approve/reject/rerun/stop;
- thread card refresh;
- owner decision audit.

Ручная проверка:

- нажать approve/reject/stop;
- проверить переходы статусов;
- убедиться, что merge не выполняется без явного решения.

## Кодовый PR 10: hardening and dogfooding

Содержимое:

- network/security defaults;
- retention cleanup;
- deploy/smoke polish;
- runbook;
- dogfooding на `matter-codex` или выбранном repo.

Ручная проверка:

- полный end-to-end run;
- повторный deploy;
- проверка cleanup;
- итоговый список production gaps.
