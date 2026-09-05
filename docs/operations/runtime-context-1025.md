---
id: OPS-RUNTIME-1025
title: Проекция Skills и памяти runtime-controller
type: operations
status: approved
owner: backend
version: 1.1.0
updated: 2026-09-05
---

# Граница проекции

Источники: #1025, #1026, #1046, #1018 и MVP-UI-37/61.
Контракт consumer: [OPS-RUNNER-1026](runner-context-1026.md).
Этот документ задаёт интеграционный контракт; фактические результаты проверок
указываются отдельно на итоговом SHA.

| Сценарий | Actor и authority | RPC и owner | Эффект controller и read path |
| --- | --- | --- | --- |
| Initial | workload mTLS и подписанный exact grant | CP ClaimExecution, immutable RuntimeRevision | Полная проверка digest, typed snapshot, новый Pod и immutable projection |
| Continuation | Новый server-owned turn/attempt | CP новая revision после закрытия предыдущего графа | Новый Pod; resume только с owner-разрешённым неизменным context digest |
| Retry | Новый grant/fence, не прежний ticket | CP новая attempt/revision | Новые projections; чужой или прежний callback отклоняется |
| Skill file | Init mTLS, ticket, execution binding и точный pin | CP ReadExecutionArtifact с lease/fence/generation | Membership в snapshot до RPC, bounded bytes и exact digest после RPC |
| Memory | Полномочия из owner revision, не filesystem path | CP snapshot с binding/revision/retention | Read-only typed records, без knowledge-artifact fallback |
| Renew | Та же active lease/attempt/fence | CP RenewExecution | Нельзя менять snapshot продлением lease |
| Warm reuse | Точная system session и compatibility digest | CP desired revision и claimed turn | Только одинаковые pins; changed context не передаётся старому Pod |
| Cancel/terminal/delete | Owner закрывает граф и grants | CP authoritative execution readback | Cleanup принадлежащих Pod/ticket/config/RBAC/network, result read остаётся у CP |
| Retention/revoke | CP eligibility на новой revision и fenced read | CP owner lifecycle | Не выдаётся новая authority; runner ограничивает активный процесс retention deadline |

Controller не создаёт собственных доменных событий и не переписывает состояние
CP. Idempotency, отзыв grant и terminal graph принадлежат существующим owner
командам. Kubernetes objects сверяются по exact metadata и содержимому;
AlreadyExists не означает принятие неизвестной проекции.

## Файловая граница

`runtime-context` является отдельным emptyDir с sizeLimit 520Mi: 512Mi bounded
Skill content и запас для manifest/typed memory. Init монтирует его RW в
`/workspace/context`, role и provider RO; credential relay его не получает.
Внутри context нет дополнительных mounts, subPath или mount propagation.
Существующие UID 10001/10002, fsGroup 29000 и writable workspace сохраняются.
EmptyDir не является authority/replay store: immutable pins принадлежат CP.

Проверена документация Context7 `/kubernetes/website`: emptyDir sizeLimit,
per-container readOnly mounts и ограничение нерекурсивных read-only mounts.
Новых egress permissions и TLS fallback нет.

## Hydration и callback

Controller отображает CP `skill_bundles`/`memory_records` в shared typed
snapshot после назначения organization/project/agent из execution owner.
Пустые списки канонически `[]`, timestamps UTC. Snapshot digest и полный
RuntimeRevision digest пересчитываются до credential materialization.
Неполные provenance, timestamps, binding/version, retention или manifest pins
закрыто отклоняются. `skills.json`/`memories.json` включают тот же context digest.

Файловый HTTPS callback использует существующий execution route и ровно пять
query fields из OPS-RUNNER-1026. Selector не выдаёт authority: ticket, mTLS,
execution headers и membership проверяются до owner RPC. После RPC
сверяются project/ref/revision/size/digest и фактический SHA-256 bytes.
Receive budget отдельного RPC равен pin size + 64KiB metadata, без изменения
лимитов остальных методов. Ошибка не возвращает частичный файл или raw body.

## Интеграционные зависимости

Принятые runner checkpoints `23774ee12`, `8be345b6` перенесены как зависимости.
CP snapshot `78a64f854` зависит от последовательности CP commits от
`1cf399a5` до него; она перенесена без изменения owner реализации.
Generated integration registry при конфликте пересоздан из объединённых YAML,
с сохранением полного main-каталога #1028 и CP Mattermost metadata.

Дополнительный CP checkpoint `2bb8df5ba` наполняет snapshot при claim и
проверяет exact Skill file membership, текущие bindings, root actor eligibility,
lease/fence/generation через существующий ReadExecutionArtifact; read bounded
32MiB. Его зависимости также перенесены без редактирования owner реализации.
В CP checkpoint `98a71da1e` включён `b20884535`: ClaimExecution получает context
digest предыдущей revision через принадлежащую session запись session_storage,
сверяет organization/session и сбрасывает CodexSessionID до sealing новой
revision при changed/missing context. Неизменный context сохраняет разрешённый
resume. Controller не меняет CodexSessionID после проверки owner digest.

Shared workspace policy включает `/workspace/context=READ_ONLY`. Producer и
consumer пересобираются с одной версией shared policy: её digest изменился,
старый snapshot с четырьмя правилами не принимается. Узкие изменения runner
сначала проверены в интеграционном WT #1025, затем перенесены root в полный
runner PR #1058. Это зависимость controller, не отдельная альтернативная
реализация проверки workspace.
CP `8125c13db`, включённый в `98a71da1e`, отображает shared V1 в entity вместе
с canonical digest. Прежнее расхождение четырёх и пяти правил устранено.
Для этой проверки runner/controller commits перенесены поверх точного CP
checkpoint без изменения `services/internal/control-plane`, Proto и generated
client. Предыдущий `1360c249` сохранён ссылкой
`kodex-agent/issue-1025-before-cp98a`; завершённые WT #1031 и EMAIL не менялись.

## Writable readiness

Readiness не выполняет filesystem I/O внутри HTTP handler. Один monitor
запускает защищённый runner с закрытым режимом `runtime-workspace-canary` без
credentials из environment и без сетевых клиентов. Бюджет проверки 2 секунды,
после SIGTERM даётся 1 секунда на cleanup, затем процесс принудительно
завершается и ожидается через Wait. Проверка повторяется через 5 секунд;
результат старше 10 секунд закрыто отклоняется. Stop отменяет и ожидает monitor.
Startup и завершающая проверка записи используют тот же bounded helper.

Canary проверяет quota только writable дерева, выполняет create/read/atomic
replace/read/delete и удаляет временный каталог. При кооперативной отмене
cleanup выполняется через открытые directory handles. При неотзывном зависании
filesystem и принудительном kill readiness остаётся отрицательной; восстановление
Pod/следующая attempt очищает outbox существующим server-owned reset, без выдачи
остатков как результата. Зависшее ядро не объявляется исправной файловой системой.

Официальный контракт Go `os/exec` CommandContext, Cancel и WaitDelay проверен
через Context7 `/websites/pkg_go_dev_go1_25_3`.

## Локальные доказательства

- `TestHydratesTypedContextAndPublishesOnlyExactRecords`: typed mapper, schema,
  digest, scope, attempt и отказ при unsealed drift.
- `TestOwnerSealedContextResumeSurvivesProtoAndProjection`: Proto roundtrip,
  owner-selected resume/reset, exact shared workspace digest, материализация
  Pod и отказ при подмене CodexSessionID после sealing.
- `TestContextHydrationRejectsInvalidPinsBeforeMaterialization`: nil/provenance,
  invalid timestamp, traversal, oversize и retention.
- `TestContextArtifactRouteBindsOwnerReadAndDoesNotExposeMismatches`: exact
  callback, owner RPC cardinality и отсутствие bytes при mismatch.
- `TestContextMountAdmissionEvaluatesGeneratedPodAndRejectsDrift`: исполнение
  действительных CEL mount-выражений на generated Pod и отрицательных fixtures.
- `TestRetryMaterializesNewRevisionAndCleanupKeepsNewAttempt`: новые Pod и
  projections, removed context не переносится, cleanup не удаляет новую attempt.
- `TestEnsureWarmRejectsStaleContextCompatibilityBeforeReplacement`: устаревший
  compatibility digest удаляет прежний warm Pod до следующего reconcile.
- `TestWorkspaceCanaryProcessIsBoundedAndReaped`: реальный child process,
  timeout/kill/Wait, bounded output и safe denial.
- `TestCanaryCancellationAfterWriteCleansTemporaryFiles` и
  `TestWorkspaceCanaryWithNonRootProcess`: cleanup после отмены и writable
  create/read/replace/delete в настоящем non-root процессе.
- `TestWorkspaceReadinessUsesOnlyFreshSnapshot` и
  `TestWorkspaceMonitorCancellationJoinsCheckAndClearsReadiness`: probe без I/O,
  отказ на stale/missing snapshot и cancel/join.
- `test-go-toolchain-contract`: все local replacements включены в Docker context;
  controller Dockerfile копирует `libs/go/secretbrokerapi`.

CEL проверяется существующей в репозитории версией `cel-go v0.30.0`;
актуальные Compile/Program/Eval APIs сверены через Context7 `/cel-expr/cel-go`.
Это тест конкретной mount boundary, не замена полной Kubernetes admission
проверки или live rollout. Последние выполняются только по отдельному допуску.

## Проверка CP checkpoint 98a71da1e

Локальная проверка включает controller и runner `go test -race ./...`, shared
runtimecontract race, controller `go vet ./...` и `go build ./...`.
CP unit/caster проверяются без правок owner-кода:

```bash
cd services/internal/control-plane
go test -race ./internal/repository/postgres/platform ./internal/transport/grpc \
  -run 'Test(Runtime|CastRuntime)' -count=1
```

Из корня выполняется disposable PostgreSQL suite:

```bash
timeout 240s bash scripts/tests/control-plane-postgres-test.sh \
  '^TestBootstrapComponent$/(memory_records|skill_bundle|direct_run|session_archive|runtime_environment|role_image_promotion)'
make check-proto-codegen test-authority-policy-codegen test-go-toolchain-contract
docker build --file services/internal/runtime-controller/Dockerfile \
  --tag kodex-runtime-controller:issue-1025-cp98a .
```

PostgreSQL fixture проверяет миграции/reapply, memory/skill lifecycle,
continuation/cancel/retry, archive restore/GC, exact image admission и pinned
runtime environment. Это локальный PostgreSQL, не staging. Context7
`/protocolbuffers/protobuf-go` использован для проверки Proto Marshal/Unmarshal;
сверяются семантические поля и canonical domain digest, не порядок wire bytes.

Оба Kustomize-профиля проверены отдельно в области controller: non-root,
read-only root, issuer/grant-agent, readiness, exact network и context admission.
Общий `make test-web-only-release` на этой интеграции остаётся **FAIL**:
`release references Kubernetes Secrets without a producer: email-bridge-mailbox-projection`.
Это отдельная CP/EMAIL integration dependency, а не разрешение скрыть Secret
allowlist или изменить mailbox policy в controller. Общий release gate требует
исправления владельцем и повторного запуска на интегрированном SHA.
Staging, Kubernetes apply/import, live providers и browser E2E: **NOT RUN**.
