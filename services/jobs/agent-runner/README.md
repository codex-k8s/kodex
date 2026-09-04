---
id: SVC-MC-017
title: agent-runner
type: service
status: approved
owner: developer
version: 3.1.0
updated: 2026-09-04
---

# agent-runner

`agent-runner` — защищённый runtime ABI внутри каждого promoted role image. Это
не legacy service и не общий long-lived bot. Для обычного turn отдельный runner
стартует внутри нового execution-scoped Pod этой роли; системный помощник имеет
отдельный warm режим.

## Граница ответственности

Runner:

- читает и строго валидирует immutable `kodex.agent-runner-input.v6`;
- подтверждает exact organization/project/run/node/session/turn/attempt/fence,
  полный execution binding и MCP binding через execution-scoped callback;
- применяет готовые server-materialized instructions без повторного rendering;
- init container материализует exact VFS manifests/files;
- role runtime повторно проверяет manifests, размеры и digests без записи в
  read-only input mounts;
- материализует provider binding и MCP config;
- запускает provider runtime прямым `exec` без shell workflow;
- передаёт coalesced safe progress;
- завершает child processes и формирует signed bounded terminal handoff.

Runner не создаёт Project/Agent/Run, не вычисляет graph lifecycle, не принимает
actor/owner/lineage из prompt, не читает PostgreSQL и не обращается к Kubernetes
API. У него нет Mattermost token, registry writer/admin или managed integration
credentials. External channel delivery выполняет optional interaction adapter
после core terminal transaction.

## Role image

Role image содержит собственное окружение роли: OS packages, языки, CLI,
браузеры, OCR/office tools либо другое ПО. После недоверенного installation
step supply chain добавляет trusted `kodex-init` и
`kodex-agent-runner`, фиксирует runtime ABI и допускает exact digest.

Таким образом, образ юридического сотрудника может содержать PDF/OCR, образ
аналитика — Python/R, а образ разработчика — compiler/Git tools. Выбор display
role name не выдаёт инструмент; authority определяется RuntimeRevision,
capabilities и grants.

## MCP

MCP остаётся runtime-протоколом. Каждый server/tool имеет стабильную
типизированную schema, timeout, required flag и allowlist. Config генерируется
из server-owned RuntimeRevision; secret values в TOML не записываются.

Provider process читает только broker-owned immutable Secret subPath и
публичный pinned digest из ConfigMap. Role runtime не получает credential
mounts или secret env. MCP proxy и callbacks проверяют один exact binding:
project/session/turn/attempt/lease/fence/generation/method/input digest и
RuntimeRevision digest.

- platform MCP: `delegate_agent`, `invoke_integration` по exact grants и
  `propose_configuration_plan` только для системного помощника;
- integration MCP: специализированные adapters `integration-gateway`;
- generic external API proxy и произвольная command authority запрещены.

Terminal completion, child callback и публикация bounded artifacts идут через
защищённый runner callback contract. Продолжение Session создаёт владелец
состояния через специализированный owner API, а Human Gate материализуется из
опубликованной WorkflowVersion; эти действия не объявляются отдельными MCP
tools.

Долговечное ожидание Human Gate не удерживает Pod. Control-plane фиксирует
`WAITING_HUMAN`, закрывает текущую attempt и после решения создаёт новую
RuntimeRevision/Pod. MCP timeout не используется как многодневный wait.

## Warm mode

Warm runner системного помощника стартует provider session заранее и получает
server-owned turns последовательно через защищённый callback. Idle loop не
считается Turn. При revision mismatch, stale ticket или callback loss runtime
закрыто отклоняет работу; controller восстанавливает desired Pod. Turn с новыми
files не переиспользует warm Pod и получает собственную init materialization.

## Workspace

`workspace-init` с UID 10001 заполняет `input` и `knowledge`; в role/provider
containers эти mounts read-only. Session PVC смонтирован только в
`/workspace/.kodex/state`. Writable workspace/result outbox ограничен 1 GiB,
10 000 files и Kubernetes `emptyDir.sizeLimit`; root filesystem read-only,
`fsGroup=29000`, seccomp и optional provider AppArmor согласованы с admission.

До готовности runtime выполняется bounded canary create/write/`fsync`/atomic
replace/read/delete с cleanup. Диагностика ограничена точными reason codes
`READ_ONLY`, `QUOTA_EXCEEDED`, `PATH_OUTSIDE_WORKSPACE`, `RUNTIME_IO_ERROR` и
не содержит body либо path.

## Безопасность вывода

Raw provider JSONL, stdout/stderr, arbitrary tool payload, prompts и secret
values не публикуются в logs, NATS или WebSocket. Runtime возвращает stable
safe code/message key и bounded status. Пользовательский текст локализуется по
проверенной locale из YAML i18n, а runtime diagnostics остаются на английском.

## Immutable RuntimeRevision

Execution binding охватывает identity и все применимые поля materialization:
exact model/reasoning overlay, runtime/provider policy, готовый prompt и его
version/digests, promoted image, image tools, environment/secret descriptors,
effective capabilities, grants, delegation targets, VFS attachment sets и
files, session context, workspace policy и Codex session. Изменение одного поля
без новой server-owned binding отклоняется до provider process.

Runner не разрешает `latest` refs и не вычисляет permissions из локального или
caller state. Config overlay проходит strict TOML decode с запретом unknown и
authority keys. Model и reasoning проверяются по закрытому runtime catalog,
каждый image tool обязан существовать как executable в exact image, а MCP
readiness требует точного каталога tools, соответствующего RuntimeRevision.

Для initial Agent, Process stage и Automation task передаётся как точное user
message, а готовый prompt хранится в `AGENTS.md`. Для Continuation и Retry user
message дополнительно содержит exact revision/attempt и текущие
model/reasoning/image/tools/MCP/files/config bindings, а также обязательные
`workflow-stage`, `automation`, `session-continuation` и
`effective-capabilities` blocks.

VFS inputs материализуются только после digest verification. Skills
представлены exact `environment_tools` выбранного promoted image, memories —
version-pinned knowledge artifacts в read-only `/workspace/knowledge`. Эти
проекции не дополняются из mutable catalogs во время turn.

## Workspace и результат

`/workspace/input`, `/workspace/knowledge` и provider credential snapshot
read-only; запись разрешена только в attempt workspace с bounded quota.
Readiness выполняет реальный create/read/atomic replace/read/delete canary через
те же `openat`/`O_NOFOLLOW` primitives, что и рабочий outbox. При успешном
`workspace-write` runner атомарно публикует
`workspace-write-result.json` с exact RuntimeRevision, attempt и execution
binding provenance, затем включает файл в bounded completion artifacts.

Runner является процессом внутри ephemeral либо warm role Pod. Отдельных
Deployment, Service и Kubernetes RBAC у него нет. ServiceAccount не получает
token; default-deny и warm exact egress находятся в `runtime-workloads`, а
per-attempt NetworkPolicy/RBAC materializes runtime-controller из immutable
environment policy. Наблюдаемость процесса использует общий Go runtime;
готовность workload и terminal safe codes наблюдаются владельцем lifecycle —
runtime-controller.

## Локальная проверка

```bash
make test-agent-runner
```

Требуются Go, jq, kubectl, yq и bubblewrap с доступным user namespace.
Проверка не использует API credentials и не обращается к живому кластеру.

## Карта критериев #1026

| Критерий | Конкретное доказательство |
| --- | --- |
| Immutable input, отсутствие caller authority | `runtimecontract.TestRunnerInputRejectsRuntimeMaterializationDriftAndRevokedGrant`: изменение task/model/prompt/image/tool/file/credential/grant нарушает binding |
| Initial, workflow, automation, continuation, retry | `app.TestMaterializedPromptCoversAllRunnerLaunchKinds`; `TestBuildInitialPromptUsesExactServerTask`; `TestBuildContinuationPromptIncludesExactRevisionDeltaAndServiceBlocks` |
| Exact model/reasoning/config | `codex.TestPrepareHomeMaterializesOnlyBoundEnvironment`; `TestTurnStartPinsModelReasoningAndPersonalityOnEveryAttempt`: `gpt-6-astra`, effort/personality передаются в `turn/start`, включая resume |
| Отказ до процесса | `codex.TestExecuteLocalRejectsUnknownSelectionBeforeProcessOrCredentialAccess`; `readiness.TestCheckMCPRejectsUnknownMissingAndDuplicateToolsBeforeProviderStart` |
| VFS и session isolation | `app.TestWriteInputManifestsUsesCanonicalFullCatalog`; `TestMaterializeInputArtifactsRejectsManifestMismatchBeforeWorkspaceMutation`; `TestNextAttemptClearsOutboxWithoutFollowingForeignSymlink`; `runtimecontract.TestWarmCompatibilityDigestIgnoresTurnIdentityAndRejectsRuntimeDrift` |
| MVP-UI-61, положительная запись | `app.TestWorkspaceSubprocessWriteAndCompletionProvenance`: отдельный детерминированный процесс в bubblewrap создаёт вложенный файл, читает, заменяет inode через rename, читает и удаляет; runner собирает result и exact revision/attempt provenance в валидный completion |
| Отдельные отрицательные записи | `app.TestWorkspaceSubprocessRejectsProtectedWrites`: immutable, credential, foreign, symlink, traversal; `workspace.TestRunCanaryRejectsSymlinkEscape` |
| Bounded completion | `app.TestCompletionKeepsProvenanceAtArtifactLimit`; `TestCollectArtifactsDoesNotBlockOnFIFO`; `callback.TestCompleteRejectsMismatchedAttemptBeforeTransport` |
| Readiness/deploy | `make test-agent-runner`: runtimecontract, runner, v6 schema assertions и локальный render `runtime-workloads`, ServiceAccount/RBAC/default-deny/exact warm egress |

Subprocess acceptance проверяет реальные операции файловой системы, но заменяет
недетерминированную модель тестовым процессом. Это не live Codex/API smoke и не
доказательство полного развёрнутого пути UI → controller → role Pod → storage.
Полный baseline и общее product/security/architecture review выполняются
отдельно на интегрированном SHA. Контракт согласован с #1025: полный wire input
защищён общим execution binding, bounded input проверяется по digest, warm
compatibility учитывает стабильный профиль доступа, а не attempt-local identity.
Новый альтернативный input contract runner не вводит.

## Жизненный цикл и полномочия

| Переход | Producer → runner → авторитетный результат |
| --- | --- |
| Initial Agent | Проверенный actor/grant у владельца → immutable revision/input → exact task и готовый `AGENTS.md` → execution-scoped completion |
| Workflow stage | Опубликованная стадия и server-materialized service block → тот же runner path → completion конкретной node/attempt |
| Automation | Владелец запуска materializes automation block/revision → тот же runner path → completion конкретной attempt |
| Continuation | Владелец закрывает прежнюю attempt и назначает новую revision → явное user message с bindings и service blocks → новый completion |
| Retry | Новая attempt/grant/revision от владельца → свежий outbox, прежняя session только по pinned archive → новый completion; старые artifacts не переиздаются |
| Revoked/cancel/expiry | Авторитетный callback/MCP отказывает по lease/grant; runner не создаёт replacement grant и не вычисляет permissions из prompt |
| Terminal | Broker завершает provider, runner собирает bounded artifacts → callback с execution binding, revision и attempt → owner transaction и дальнейшие события принадлежат runtime-controller |

Runner не владеет PostgreSQL/OCC, pagination, schedule или terminal events.
Его граница: immutable input → validated profile → MCP readiness тем же
авторизованным путём → provider → `/v1/executions/{lease}/complete`.
Серверный отзыв действующего grant проверяется callback, а не локальным
пересчётом immutable snapshot.

Через Context7 проверена документация OpenAI Codex: `config.toml`,
`CODEX_HOME`, MCP configuration и app-server `thread/resume`/`turn/start`
(`effort`, `personality`). Источник:
https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md.

Schema: `contracts/runtime-controller/v6/agent-runner-input.schema.json`.
Supply chain: `docs/domains/images-supply-chain.md`.
