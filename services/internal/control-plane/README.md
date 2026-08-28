---
id: SVC-MC-001
title: Control-plane
type: service
status: approved
owner: backend
version: 1.2.0
updated: 2026-08-28
---

# Control-plane

`control-plane` — единственный авторитетный владелец универсальной web-first
модели Kodex. Сервис не зависит от Mattermost, GitHub, Kubernetes или
другой внешней интеграции как от пользовательского или lifecycle authority.

## Ответственность

Сервис хранит и изменяет:

- Organization, Subject, Project, permission registry, versioned application
  role, user/OIDC-group/service binding и effective access;
- Agent, RoleDefinition и immutable published Instruction;
- Workflow и его версии;
- Session, FIFO Turn, Run, RunNode, RunEdge и RunEvent;
- Human Gate, Artifact metadata/content и Schedule;
- version-pinned IntegrationDefinition, Connection credential revision
  metadata, typed Grant, IntegrationInvocation и immutable effect receipt;
- immutable RuntimeRevision, lease/fence, delegation и callback receipt;
- системного помощника, его protected core prompt, durable Session и warm
  desired state;
- semantic idempotency receipts, audit и transactional outbox;
- role image recipe/build/admission/promotion metadata.

Сервис не хранит provider и integration secret values, не создаёт Kubernetes
Pod и не выполняет внешние эффекты. Для credential интеграции он выполняет
только узкую server-owned материализацию одного data key в Secret
`kodex-system/kodex-integration-credentials`: значение находится в памяти лишь
на время запроса, а в PostgreSQL остаются UID, `resourceVersion`, digest и
immutable revision. Runtime materialization принадлежит `runtime-controller`,
чтение credential и внешние вызовы интеграций — `integration-gateway`, browser
boundary — `control-api-gateway`.

## Контракты

- Proto: `contracts/proto/controlplane/v1/control_plane.proto` и
  `contracts/proto/controlplane/v1/access.proto`;
- generated Go API: `libs/go/controlplaneapi/gen/controlplane/v1`;
- generated client composition: `libs/go/controlplaneclient`;
- domain events: `contracts/asyncapi/control-plane/v1/asyncapi.yaml`;
- machine policy:
  `deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json`;
- owner HTTP/WS mapping:
  `contracts/openapi/control-api-gateway/v1/openapi.yaml` и
  `contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml`.

Browser request не является источником actor, organization, permission,
root lineage или ownership. `control-api-gateway` предъявляет OIDC credential
по exact mTLS в `AuthorityProofResolverService`; `control-plane` разрешает
Subject, Organization и Project по PostgreSQL state, синхронизирует bounded
OIDC groups как identity read model и выпускает
короткоживущее proof. Рабочий RPC проходит local issuer/verifier и exact
operation binding из generated policy. Для worker используется отдельный
bounded application grant и server-owned high-watermark.

Credential connection настраивается двумя отдельными командами. Первая создаёт
Connection в `NOT_CONFIGURED` без secret metadata. Вторая после повторной
проверки authority и OCC материализует значение через exact Kubernetes RBAC,
создаёт immutable `IntegrationCredentialRevision` и переводит Connection в
`CONFIGURED`. Один idempotency key всегда соответствует одному детерминированному
data key; повтор с другим значением закрыто отклоняется.

## PostgreSQL

Fresh install последовательно использует baseline и forward-only расширения
для object storage, lifecycle расписаний, session archive, runtime configuration
и типизированных интеграций:

```text
cmd/cli/migrations/20260822000100_web_first_baseline.sql
cmd/cli/migrations/20260828000100_s3_artifact_content.sql
cmd/cli/migrations/20260828000110_schedule_archive_lifecycle.sql
cmd/cli/migrations/20260828000200_session_archive.sql
cmd/cli/migrations/20260828099500_runtime_environments.sql
cmd/cli/migrations/20260828099600_integration_backend_unit.sql
```

Canonical authority хранится только в `permission_registry`,
`application_roles`, immutable `application_role_versions` и
`access_bindings`. Объект `control_plane.memberships` является read-only SQL
view для старого presentation contract. Legacy membership-команды изменяют
canonical role/binding; exact resource binding в широкую project projection не
попадает.

Production SQL отсутствует в Go literals. Каждый запрос находится в отдельном
`internal/repository/postgres/platform/sql/*.sql` и встраивается отдельной
директивой `//go:embed` в именованную строковую переменную.

CLI принимает только безопасные команды:

```bash
CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE=/run/secrets/dsn \
  /usr/local/bin/control-plane-cli up
CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE=/run/secrets/dsn \
  /usr/local/bin/control-plane-cli status
```

DSN читается из файла и не выводится. Kubernetes Job
`deploy/k8s/base/control-plane/migration-job.yaml` вызывает `up` до rollout.
Legacy expand/backfill/contract и cutover path отсутствуют, потому что reset
поддерживает только fresh installation.

## Lifecycle расписаний

Owner-facing API предоставляет `ListSchedules`, специализированный
`GetSchedule`, `CreateSchedule`, `UpdateSchedule`, `SetScheduleEnabled` и
`ArchiveSchedule`. Archive является terminal soft transition, а не SQL
`DELETE`: Schedule остаётся доступен как read-only история, получает новый
version и больше не возвращает mutation actions.

Архивация одной PostgreSQL-транзакцией проверяет `MANAGE_SCHEDULES`,
idempotency и expected version, переводит Schedule в `ARCHIVED`, отключает его,
очищает `next_run_at`, отменяет нематериализованные `DUE|CLAIMED` occurrences и
очищает их lease/fence. Та же транзакция сохраняет audit,
`SCHEDULE_CHANGED` и command receipt. Уже `MATERIALIZED` occurrence и связанный
Run не отменяются: Run продолжает жить по immutable snapshot.

## Bootstrap

Application startup после успешной migration выполняет одну serializable
bootstrap transaction. Она создаёт:

- Organization и `installation-owner` claim contract;
- permission registry, пять immutable system role и OWNER binding системного
  Subject для внутренних worker operations;
- platform capabilities и safe default runtime profile;
- shipped IntegrationDefinition для synthetic HTTP и GitHub, а также
  совместимое определение Mattermost для существующего `interaction-gateway`;
- единственный Agent со stable key `system-assistant`;
- immutable published core prompt;
- долговечную system Session и warm runtime desired state.

Повторный bootstrap сверяет текущие revision/digest/content core prompt. Новая
поставляемая revision создаёт следующую immutable published Instruction и
переводит warm runtime в `RECOVERING`; rollback либо конфликт содержимого одной
revision закрывают startup. Database trigger и domain commands запрещают
удалить, отключить, архивировать или превратить системного помощника в обычного
Agent; опубликованную версию core prompt нельзя изменить либо удалить.

## Выполнение и события

Перед каждым turn/retry/continuation control-plane создаёт immutable
RuntimeRevision с exact Agent, instruction, capability/grant revisions,
promoted role image digest, runtime ABI, input digest и attempt. Обычный turn
claim-ит `runtime-controller`; системный помощник использует отдельную warm
revision. Delegation создаёт server-owned child Run/node/edge, callback имеет
один durable receipt.

Каждое изменение execution graph резервирует последовательный номер в пределах
root Run и одной транзакцией сохраняет RunEvent и outbox envelope. Relay
публикует bounded события в NATS JetStream:

```text
control_plane.run.<organization-ref>.<root-run-ref>.events
control_plane.platform.<organization-ref>.events
```

Gateway использует NATS только как сигнал доставки, а snapshot/catch-up читает
из авторитетного control-plane. Raw provider payload, stdout/stderr, JSONL,
секреты и файлы в события не попадают.

## Health и readiness

- `/healthz` проверяет только жизнь процесса;
- `/readyz` возвращает уже рассчитанный локальный snapshot и не делает
  сетевых вызовов на probe;
- background readiness проверяет только PostgreSQL, outbox, NATS и local
  authority verifier;
- недоступность соседнего business service не входит в readiness;
- OIDC JWKS обновляется независимо и использует двухминутный bounded
  last-known-good без продления при повторных ошибках;
- потеря и восстановление зависимости логируются только как переход состояния.

Межсервисный рабочий граф проверяется отдельным diagnostic/smoke path, а не
Kubernetes readiness.

## Локальные проверки

```bash
make test-go
make test-authority-policy-codegen
make test-control-plane-postgres
make test-web-only-release
```

`test-control-plane-postgres` запускает disposable PostgreSQL 18, выполняет
`goose up`, `status`, повторный `up`, два bootstrap и проверяет protected
integration lifecycle: WRITE invocation не выдаётся до отдельного Human Gate,
а повторное завершение читает единственную immutable effect receipt.
Production DSN и live data не используются.

## Развёртывание

Canonical application render создаётся только через
`tools/release/render-web-only.sh` из immutable release lock. Скрипт не
выполняет apply. Диагностика migration, bootstrap, authority и outbox описана
в `docs/runbooks/control-plane.md`.
