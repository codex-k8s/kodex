---
id: SVC-MC-001
title: Control-plane
type: service
status: approved
owner: backend
version: 1.3.1
updated: 2026-09-04
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
- role image recipe/build/admission/promotion metadata;
- versioned prompt template, RoleImage, IntegrationDefinition и системную STT
  configuration с явным `managed_by`, source и immutable source revision;
- tenant-isolated VFS metadata для проектов, сущностей, запусков, workspace
  inputs/results, skills и memories.

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

## Lifecycle и authority map полного unit

Во всех строках actor и organization берутся только из проверенного
`AuthorityProof`; `projectRef`, resource ref, owner ref и consumer ref из
payload никогда не являются полномочием. Сервис повторно разрешает ресурс в
своей organization/project boundary. State-changing RPC требует exact
operation, idempotency key и, кроме create, expected version. Одна PostgreSQL
transaction фиксирует business state, receipt, audit и обязательный outbox.

| Сценарий | Actor и authority | RPC или command | Авторитетное состояние и переход | Результат и read path |
| --- | --- | --- | --- | --- |
| Prompt preview и Run | Для `RUN`/`SESSION` пользователь должен видеть фактический Project; synthetic preview доступен authenticated actor, а полный текст дополнительно требует `prompt.full.view` на точном target и fresh credential `auth_time` не старше пяти минут из проверенного ACR/AMR context | `ValidatePromptTemplate`, `PreviewPromptTemplate`, запуск/claim существующего Run lifecycle | Один renderer материализует `AGENT`, `WORKFLOW_STAGE`, `AUTOMATION` или `SESSION_CONTINUATION`; `RUN` выбирается только по exact requested `run_id`, `SESSION` — по exact Session, и preview повторно загружает сохранённый server-owned snapshot из `RuntimeRevision` | Безопасный preview не содержит contextual values; полный текст выдаётся только после permission и reauthentication, а digest совпадает с повторной материализацией сохранённого snapshot |
| Effective capabilities | Authority пользователя из access policy; принадлежность Agent, Workflow и Connection выводится из доменного состояния; Human Gate layer сервер получает из immutable WorkflowVersion по exact node/turn/attempt | Preview, создание `RuntimeRevision` или специализированный invocation lifecycle | Допустимый набор образует union Agent и eligible Connection grants, затем его сужают применимые user, Workflow и Human Gate layers; `nil` допустим только когда gate неприменим, явно пустой применимый layer закрывает весь набор | Клиент видит только итоговый закрытый набор и не может добавить capability через payload; пустой gate layer отсекает integration grant |
| Platform search | Проверенный Subject и canonical effective access к каждому Project/Agent/Workflow/Run/Artifact; legacy `PROJECT_MEMBERSHIP/VIEW` не даёт metadata eligibility | `SearchPlatform` | Tenant/project candidates, query и optional `projectRef`; immutable ordering `(relevance, created_at, kind, ref)` и filter-bound opaque cursor внутри repeatable-read страницы | `total`, страница результатов и `nextPageToken`; изменение `updated_at` между страницами не создаёт gap/duplicate, смена фильтра или repeated cursor отклоняются |
| VFS tree/search | Canonical effective access вычисляется отдельно для каждой ветви и instance binding | `ListVFSNodes`, `SearchVFS` | Input существует только по exact `Run/Turn -> AttachmentSet -> frozen item`; result — только по producer `run_id` и source `AGENT_RESULT/INTEGRATION_RESULT`; reuse output как input не меняет producer classification | Устойчивый `(path, ref)` cursor, `total`, tenant-isolated metadata без content/secret и без legacy project VIEW leakage |
| Managed revision | Project manager, а для system STT — organization manager | Специализированные create/validate/publish/history/impact/rebind RPC каждого kind | `DRAFT -> VALID/INVALID -> PUBLISHED`; published revision immutable, предыдущая становится историей; до selective rebind consumer продолжает использовать прежнюю pinned revision; на один kind у consumer существует одна binding | `ListManagedConfigurationHistory`, `GetManagedConfigurationImpact`, effective prompt, `GetRuntimeEnvironmentRoleImageConfiguration`, `GetIntegrationConnectionDefinitionConfiguration` либо `GetSystemSTTConfiguration` |
| UI/Git ownership | Manager с authority над тем же project | `DetachGitManagedConfiguration` или `CopyGitManagedConfiguration` | UI не изменяет Git-owned set. Detach передаёт set под UI, сохраняя опубликованную revision в history; copy создаёт отдельный UI-owned set с собственной revision lineage | Readback set содержит `managed_by`, source и immutable source revision |
| Environment | Project manager и существующий специализированный lifecycle | Create/publish/bind/rollback environment commands, `ListRuntimeEnvironmentAgents`, readiness query | Create и publish создают immutable revision; bind/rollback атомарно меняют exact Agent binding; readiness выводится из фактической image/provider/secret eligibility | Environment detail, versions, agents и readiness blockers |
| Runtime secret | Project manager; materialization только `secret-broker` grant | Prepare create/rotate/reveal/revoke и `RuntimeSecretWorkService` | Нормализованные metadata и content hash; secret value не хранится в PostgreSQL; completion связывает immutable credential revision, tenant, operation и attempt | `ListRuntimeSecrets` и `GetRuntimeSecret` возвращают metadata, state и opaque bounded cursor |
| Runtime workspace | Матрицу назначает только control-plane при создании immutable `RuntimeRevision`; browser и runner не передают paths или quota | `RuntimeRevisionSnapshot.workspace_policy` | Revision `1`, root `/workspace`, longest-prefix rules `/workspace/input=READ_ONLY`, `/workspace/knowledge=READ_ONLY`, `/workspace=WRITABLE`, quota 1 GiB и 10 000 файлов; digest входит в revision digest | Consumer закрыто отклоняет запись с `READ_ONLY`, `QUOTA_EXCEEDED`, `PATH_OUTSIDE_WORKSPACE` или `RUNTIME_IO_ERROR`; audit/provenance не содержит file body |
| Agent avatar | Actor с `agent.avatar.manage` на exact Agent | `UploadAgentAvatar`; прежние `SetAgentAvatar`/`RemoveAgentAvatar` остаются для уже существующего Artifact | До S3 создаётся durable reservation с server-owned key; после materialization одна owner transaction фиксирует Artifact/content descriptor/binding/Agent/audit/event/receipt. OCC/commit failure запускает guarded exact-descriptor compensation, abandoned reservation забирает expiry worker | `GetAgent`/`ListAgents` возвращают только finalized avatar; `RESERVED/MATERIALIZED/COMPENSATING` не видны как active Artifact, cleanup не удаляет object при существующей exact content reference |
| Role image | Project manager | Специализированный managed revision lifecycle и существующий recipe/build/admission lifecycle | Typed recipe validation; published revision immutable; rebind только к существующему runtime environment | History/impact в control-plane; `GetRuntimeEnvironmentRoleImageConfiguration` возвращает exact binding revision runtime-controller; build и promotion остаются отдельными специализированными переходами |
| Integration definition | Organization manager владеет organization-wide definition lifecycle; rebind дополнительно требует `integration.manage`, вычисленный на exact Connection | Специализированный managed revision lifecycle | Set не имеет project scope; typed operations, risk, approval и resource contract; definition key обязан совпасть с server-resolved Connection; один `project.manage` rebind не разрешает | History/impact и `GetIntegrationConnectionDefinitionConfiguration` возвращают pinned binding; внешний effect выполняет integration gateway |
| System STT | Organization manager; consumer использует `platform.stt.use` | Специализированный lifecycle, `GetSystemSTTConfiguration` | JSON validation фиксирует provider account, model, language и permission; readiness повторно проверяет provider eligibility | Version-pinned descriptor и blockers без credential material |
| Model/provider account | Organization viewer/manager по exact operation | `ListModelCapabilities`, device verify/reauthorize, API-key account delete | Модель доступна только при enabled definition и eligible authorized credential revision; delete разрешён только API-key account и создаёт terminal `REVOKED` tombstone с durable cleanup; если device verify уже получил credential, но owner transaction/OCC не commit, control-plane проверяет `ProviderMaterializationReferenced` по exact descriptor и компенсирует только непривязанный material | Каталог возвращает reasoning efforts, eligible account refs, детерминированные blockers и закрытый `safe_status_reason`; после terminal/compensated path credential material не остаётся |
| Automation | Project manager | Schedule create/update/enable/archive | `CUSTOM` хранит нормализованный пятичастный cron; каждая occurrence фиксирует exact schedule revision | Schedule detail и occurrence/run read path; retry не переиспользует старую revision |

Renderer разрешает только server-known contextual variables: user,
organization, project, agent, workflow, automation, task, node, gate,
environment и tools. `inputFiles`, `sessionFiles`, `runFiles`,
`workflowFiles` и `gateFiles` являются типизированными коллекциями с bounded
metadata, workspace path, scope, directory и manifest ref; file body в template
data не входит. Для каждого неиспользуемого service slot renderer всё равно
добавляет явный service block. Неизвестная переменная, динамический lookup,
неизвестный target или невозможное пересечение authority завершаются закрытым
отказом до сохранения Run.

Managed configuration не публикует самостоятельный domain event: binding
меняется в owner transaction вместе с audit/idempotency receipt, а consumers
получают exact version-pinned snapshot через перечисленные защищённые read RPC.
Это фиксирует явное отсутствие event и авторитетный rejoin path.

## Producer, client, consumer, readiness и deploy

`control-plane` владеет source Proto и producer implementation. Generated API
и `controlplaneclient` регистрируют каждую operation и являются единственным
клиентским contract surface этого изменения.

| Producer contract | Клиент и consumer | Readiness рабочего пути | Deploy ownership и граница |
| --- | --- | --- | --- |
| Prompt preview, managed revisions, search/VFS, model catalog и provider lifecycle | `control-api-gateway` и staff PWA вызывают generated client | Exact authority operation, PostgreSQL и local verifier; downstream не входит в `/readyz` | Deployment `deploy/k8s/base/control-plane`; gateway/PWA implementation не меняется в этом unit |
| Атомарная загрузка avatar | Generated `controlplaneclient` публикует `platform.command.agents.avatar.upload`; будущий owner HTTP/UI adapter вызывает streaming RPC | Exact OIDC proof, project binding, `agent.avatar.manage`, PostgreSQL reservation и S3; expiry cleanup принадлежит тому же process lifecycle | Producer готов в control-plane; positive gateway/PWA consumer не реализуется в Issue #1019 и должен использовать новый RPC вместо client-side upload/set sequence |
| Version-pinned `RuntimeRevision` с materialized prompt, capabilities и workspace policy | `runtime-controller` claim-ит revision, `agent-runner` исполняет её | Existing claim/grant/fence path и runtime environment readiness; consumer обязан проверить workspace policy digest, quota и longest-prefix rule до filesystem effect | Positive consumer work принадлежит Issues #1025 (`runtime-controller`) и #1026 (`agent-runner`); их реализации здесь не меняются |
| Pinned schedule occurrence и automation target | `automation-scheduler` читает специализированный command/read path | Consumer обязан проверять точную schedule revision и terminal state | Scheduler implementation вне scope; control-plane хранит authority и occurrence |
| Pinned RoleImage binding | `runtime-controller` вызывает `GetRuntimeEnvironmentRoleImageConfiguration` по server-resolved Environment ref | Exact workload operation, tenant, active Environment, immutable binding/revision и definition digest; отсутствие binding закрывает consumer path | Реализация применения новой конфигурации принадлежит runtime/image units и в этом unit не меняется |
| Pinned IntegrationDefinition binding и invocation | `integration-gateway` вызывает `GetIntegrationConnectionDefinitionConfiguration`; существующий invocation claim остаётся effect path | Exact workload operation, tenant, active Connection, совпадающий definition key и immutable binding/revision; credential, risk и Human Gate отдельно проверяются перед effect | Внешние эффекты и их readiness принадлежат gateway units; реализации `integration-gateway`, `egress-gateway` и `interaction-gateway` не меняются |
| Runtime secret metadata и одноразовая work operation | `secret-broker` получает exact workload grant | Broker completion подтверждает exact tenant, secret, operation, attempt и digest | Secret value остаётся вне PostgreSQL; secret-broker implementation вне scope |
| `GetSystemSTTConfiguration` | `stt-tts-service` читает pinned descriptor с `platform.stt.use` | `ready=true` только для eligible provider account/model и корректного permission key | STT execution и credential access принадлежат внешнему unit Issue #1020 и здесь не реализуются |

Отсутствие внешнего consumer не делает control-plane неготовым: рабочая
диагностика проверяет его exact RPC отдельно. Новый contract считается
доступным после применения migration, успешного bootstrap, готовности
PostgreSQL/outbox/NATS/local verifier и регистрации operation в generated
authority policy. Environment render продолжает использовать существующие
ServiceAccount, Deployment, migration Job, Service, NetworkPolicy и probes
`control-plane`; новых secret или внешнего egress для этого unit не требуется.
До обновления `runtime-controller` и `agent-runner` поле `workspace_policy`
считается опубликованной producer dependency, а не доказанным рабочим
filesystem path. Эти units не входят в Issue #1019 и не изменяются здесь.

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
cmd/cli/migrations/20260904000100_issue_1019_control_plane.sql
cmd/cli/migrations/20260904000200_issue_1019_remediation.sql
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
- avatar expiry worker после startup barrier забирает bounded batch и удаляет
  только object без exact `artifact_content` reference; сбой оставляет fenced
  `COMPENSATING` reservation для повторного claim.

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
