# Архитектура MVP

## Выбранный стартовый подход

MVP реализуется как один backend-сервис `matter-codex`, но с явными внутренними модулями:

- `mattermost` - slash commands, bot posts, interactive actions, thread updates.
- `orchestrator` - state machine run/step и переходы flow.
- `runtime` - создание Kubernetes pod/job/PVC для agent run.
- `github` - операции repository, branch, PR, comments, review status.
- `credentials` - безопасные metadata и ссылки на Kubernetes Secrets.
- `agents` - agent profiles, prompt templates, render контекста.
- `audit` - журнал действий и безопасных событий.

Такой старт быстрее отдельного набора микросервисов, но не смешивает доменные ответственности.

## Mattermost integration

На первом этапе используется внешний bot-service:

- slash command `/agents`;
- Mattermost bot account token для REST API;
- posts в channel/thread через `/api/v4/posts`;
- interactive message actions/buttons с callback URL в bot-service;
- delayed responses через `response_url` для долгих команд;
- interactive dialogs для форм добавления repo/token/profile после базового CLI-синтаксиса.

Mattermost server plugin откладывается, потому что он усложняет build, rollout и совместимость с Mattermost версией.

## State machine

Минимальные состояния run:

- `created`
- `queued`
- `developer_running`
- `developer_failed`
- `pr_opened`
- `review_running`
- `changes_requested`
- `fix_running`
- `approved_by_reviewer`
- `waiting_owner`
- `owner_approved`
- `owner_rejected`
- `merged`
- `blocked`
- `cancelled`

Run хранит текущий статус, а step хранит конкретную попытку agent execution. Переходы выполняет orchestrator, а не runner pod.

## Agent runner

Runner запускает `codex exec --json` в Kubernetes pod:

- рабочая директория монтируется из PVC;
- repository checkout выполняется внутри workspace;
- `CODEX_HOME` указывает на отдельный путь в PVC;
- `CODEX_API_KEY` передается только в env конкретного процесса;
- GitHub token передается только агентам, которым он разрешен;
- stdout JSONL парсится runtime-модулем и превращается в step events;
- финальное сообщение и ссылки на PR сохраняются как artifact summary.

Базовая команда исполнения должна быть параметризована профилем агента:

```bash
CODEX_API_KEY="${CODEX_API_KEY}" \
CODEX_HOME=/workspace/.codex \
codex exec --json \
  --cd /workspace/repo \
  --sandbox workspace-write \
  --ask-for-approval never \
  --model "${MODEL}" \
  "${PROMPT}"
```

`danger-full-access` допускается только как отдельная политика для полностью изолированного pod и после явного решения владельца.

## Kubernetes runtime

Для каждого agent step создаются:

- Kubernetes Job или Pod;
- PVC с workspace;
- ServiceAccount с минимальными правами;
- env/secret refs только для разрешенных credentials;
- labels `matter-codex.dev/run-id`, `step-id`, `agent-role`;
- cleanup policy после завершения run.

PVC живет дольше pod, чтобы developer/reviewer/fix шаги могли работать с одной веткой и логами. Retention задается политикой run.

## GitHub integration

MVP использует bot PAT из secret:

- проверить доступ к repo;
- создать branch;
- push commit;
- открыть PR;
- читать reviews и comments;
- оставить review или comment;
- получить merge status.

GitHub App остается целевым вариантом после MVP, потому что дает лучшую модель permissions, installations и audit.

## Credential policy

Система хранит:

- stable credential id;
- тип секрета;
- scope и разрешенные agent profiles;
- ссылку на Kubernetes Secret;
- время последней проверки;
- безопасный статус проверки.

Система не хранит и не показывает значения секретов в Mattermost thread, логах и prompt.

## База данных

Для скорости MVP допустима одна PostgreSQL database с отдельными таблицами и префиксами доменных модулей. Целевая совместимость с `kodex` требует, чтобы миграции и код не завязывались на cross-domain SQL как на бизнес-контракт.

Минимальные таблицы:

- repositories;
- credentials;
- openai_accounts;
- agent_profiles;
- prompt_templates;
- flows;
- runs;
- steps;
- artifacts;
- audit_events;

## Observability

Первый уровень:

- structured application logs;
- Mattermost thread status;
- Kubernetes pod/job status;
- short tail logs в step artifact;
- audit events в БД.

Полные pod logs остаются в Kubernetes/runtime и не копируются без retention policy.
