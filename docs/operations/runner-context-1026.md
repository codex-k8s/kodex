---
id: OPS-RUNNER-1026
title: Материализация Skills и памяти в agent-runner
type: operations
status: approved
owner: backend
version: 1.1.0
updated: 2026-09-05
---

# Контракт Skills/Memory

Источник: #1026, #1046, #1018; принятые MVP-UI-37 и MVP-UI-61.
SkillBundle, KodexMemoryRecord, role instructions, AGENTS.md, environment tools
и knowledge artifacts остаются разными типами. Никакой VFS path не является
источником полномочий либо путём материализации.

## Producer и mounts

`libs/go/runtimecontract/context_snapshot.go` задаёт `RuntimeContextSnapshot`
со schema `kodex.runtime-context.v1`. Общая стыковка:

- `RunnerInput.ContextSnapshot *RuntimeContextSnapshot`, JSON `context_snapshot`;
- CP `RuntimeRevisionSnapshot.skill_bundles=69` отображается в `Skills`,
  `memory_records=70` в `Memories`; contract checkpoint CP `78a64f854`;
- CP сохраняет exact snapshot перед initial, workflow stage, automation,
  continuation и retry, включая явные пустые наборы;
- snapshot digest входит в RuntimeRevision и warm compatibility; изменение
  контекста несовместимо с повторным использованием старого materialized Pod;
- общий execution binding уже включает всё содержимое RunnerInput;
- отсутствие согласованного snapshot не разрешает взять latest, локальную
  память Codex, environment_tools или knowledge artifacts вместо него.

Snapshot содержит organization/project/agent из внешней execution lineage и
digest; каждый ресурс содержит revision ref/number/digest, exact binding
ref/version и provenance. Skill содержит bundle_ref, scan engine/digest/time,
name/description и file pins. Memory содержит record_ref, title/summary и retention.
CP проверяет lifecycle, project/agent scope, PUBLISHED/CLEAN, review и отсутствие
redaction до выдачи; эти mutable состояния не дублируются в runtime wire.
Request не может выдать себе binding либо review. Global assistant без Project
получает только явно пустые Skills/Memory, пока owner не определил иной scope.

File pin содержит относительный bundle path, artifact ref/revision,
`sha256:` digest и size. Байты не включаются в runtime JSON:
32 MiB/file, 64 MiB/bundle, 512 MiB суммарно. Snapshot остаётся в пределах
существующего 2 MiB input; превышение отклоняется, не обрезается.
Digest snapshot: SHA256 lowercase hex от Go JSON typed snapshot с пустым
полем digest. Порядок массивов сохраняется producer; consumer не пересортировывает
их перед проверкой.

| Компонент | Mount | Доступ |
| --- | --- | --- |
| runtime init | отдельный volume `/workspace/context` | RW до startup barrier |
| runner | тот же volume `/workspace/context` | RO |
| provider | тот же volume `/workspace/context` | RO |
| agent shell | наследует provider mount namespace | RO |
| agent workspace/outbox | существующий `/workspace` | прежний writable scope и quota |

Контекст не помещается в writable CODEX_HOME. Init полностью очищает предыдущие
`skills`, `memory`, `manifest.json`, получает файлы, проверяет их и публикует
manifest последним. Ошибка оставляет runtime неготовым. Provider не получает
authority credentials либо право обновлять это дерево. Права файлов 0440,
директорий 0750; группа 29000 наследуется от принадлежащего controller volume.
Проверка рабочего пути требует фактического read-only filesystem, одного hardlink,
точного набора файлов и отсутствия symlink, устройств, FIFO и traversal.

## Точный callback

Используется существующий HTTPS/mTLS execution-scoped путь controller:

`GET /v1/executions/{lease}/artifacts/{artifactRef}`

Закрытые query fields: `context_kind=SKILL_BUNDLE`, `skill_ref`,
`skill_revision_ref`, `skill_path`, `artifact_revision`. Существующие bearer и
execution headers неизменны. Controller сначала разрешает active execution,
полную RuntimeRevision/attempt/fence и точную запись snapshot, затем вызывает
принадлежащий CP защищённый exact artifact read. Совпадение ID без membership
недостаточно. Обычный artifact callback не становится raw proxy.

Ответ 200 содержит exact Content-Type, Content-Length и
`X-Kodex-Artifact-Digest`. Runner ограниченно пишет один поток и проверяет digest.
Чужой scope, removed binding, revoked grant, expired retention, incomplete
snapshot и несовпавшая revision отклоняются до процесса. Новых доменных событий
нет; owner read paths RuntimeRevision, claim и completion остаются у CP.

## Жизненный цикл

| Переход | Producer | Consumer |
| --- | --- | --- |
| Initial | CP разрешает grants/bindings, фиксирует snapshot | Init загружает exact files, runner проверяет перед запуском |
| Workflow stage | CP назначает stage/agent/attempt pins | Тот же проверенный путь, без чтения текущего stage state |
| Automation | CP назначает occurrence/attempt snapshot | Никакого самостоятельного поиска контекста по расписанию |
| Continuation | Новая RuntimeRevision и service blocks | Старое дерево не принимается по manifest attempt/revision |
| Retry | Новая attempt/grant, immutable pins | Материализация заново, без переноса mutable context |
| Renew lease | Та же attempt и snapshot | Нельзя подменить snapshot продлением lease |
| Remove/retention/revoke | Owner закрывает eligibility/grant | Новая попытка не получает удалённый контекст; expiry проверяется перед процессом |
| Complete/cancel/expiry | Owner закрывает execution graph | Процесс останавливается существующим lifecycle; completion exact attempt |

Для always-hot SystemAssistant manifest сохраняет session и exact snapshot,
но не номер очередной attempt: одинаковые pins допускают следующий turn без
записи в RO volume. Execution binding каждой attempt остаётся отдельным.
Изменение pins требует нового Pod. Обычный runner дополнительно сверяет
runtime revision, turn и attempt в manifest. Retention ограничивает deadline
активного процесса, а не только проверяется при старте.

Повторное использование provider thread допустимо только при совпавшем
predecessor context digest. При удалении или изменении контекста CP/controller
должны выдать новый thread с серверным continuation prompt, без прежнего
CodexSessionID: очистка файлов не удаляет память из истории старого thread.
Consumer не выводит полномочия из приватного архива Codex.

## Provider discovery

Перед thread/start либо thread/resume runner устанавливает единственный
extra root `/workspace/context/skills`, перечитывает skills/list с forceReload,
отключает найденные unbound Skills и сверяет exact name/description/path
вторым чтением. Отсутствующий pinned Skill или неотключённый посторонний Skill
закрыто останавливает запуск. В turn/start Skills передаются нативными input
items `type=skill`; память передаётся отдельно как `kodex.provider-memory.v1`
с полными typed records и immutable runtime/context digests.

Локальные `features.memories`, `memories.generate_memories` и
`memories.use_memories` явно выключены. Пустая память передаётся как `records:[]`,
а не пропускается. Исходный серверный prompt остаётся отдельным input item.

## Координация и проверка

Bohr владеет CP Proto/SQL/policy и shared RunnerInput wiring. Root владеет
controller hydration/callback lookup, warm replacement, workspace policy и
deploy/mount/admission render. #1026 не меняет эти файлы параллельно.
Эта карта фиксирует интерфейс интеграции, а не заявляет, что producer/controller
стыковка уже готова. Полный результат требует проверки интегрированного дерева.

Проверены Context7 `/openai/codex` и официальные
[Skills](https://learn.chatgpt.com/docs/build-skills),
[Memories](https://learn.chatgpt.com/docs/customization/memories).
SKILL.md имеет YAML frontmatter name/description и инструкции; memory local state
Codex не является платформенным API. Provider representation проверяется отдельно
от source digest и не подменяет принадлежащий серверу prompt.

Точные поля `skills/extraRoots/set`, `skills/list`, `skills/config/write` и
native Skill input сверены также с JSON Schema, сгенерированной самим
зафиксированным `@openai/codex@0.152.0` через
`codex app-server generate-json-schema`. Это проверка формата, не live model call.

| Критерий | Воспроизводимое доказательство | Граница проверки |
| --- | --- | --- |
| Exact snapshot, tenant, bindings, digest, retention | runtimecontract `TestRuntimeContextSnapshotExactPins`, `TestContextSnapshotIsBoundToExecutionAndWarmCompatibility`, `TestMemoryRetentionBoundsActiveExecution` | Локальные unit/race |
| Bounded fetch без arbitrary path | callback `TestSkillCallbackUsesExactExecutionAndContextPins`; contextfiles `TestPartialAndOverBudgetFetchNeverPublishManifest` | Fake HTTPS, без живого controller |
| Очистка removed context, новая attempt, warm pins | contextfiles `TestMaterializesExactSkillAndMemoryThenClearsRemovedContext`, `TestWarmContextReusesOnlyIdenticalPins` | Реальные временные файлы |
| Traversal, symlink, hardlink, FIFO, corruption | contextfiles `TestContextRejectsCorruptUnsafeAndAdditionalFiles`, `TestMaterializerDoesNotFollowOldSymlinks` | Реальная файловая система |
| RO context и writable workspace | `TestContextReadOnlyMountAndWritableWorkspaceProcesses` | bwrap, non-root UID 1000, create/read/replace/delete и запрещённые записи |
| Native Skills и typed Memory | codex `TestProviderContextUsesNativeSkillAndExactMemoryPins`, `TestProviderDiscoveryReconcilesExactSkillsInRealProcess` | Реальный fake app-server process, без model API |
| Публичный targeted профиль | `make test-agent-runner`; `go test -race -count=1 ./...` в runner и runtimecontract; `go vet ./...` в runner | Существующий schema/render; новые mounts проверяет root |
| Полная owner/controller стыковка и thread reset | Интеграционный тест после CP/controller изменений | NOT RUN в consumer WT |
| Live provider, staging, deploy, полный baseline | Отдельный допуск и итоговая проверка root | NOT RUN |

Runtime ошибки содержат только безопасную машинную причину, без bodies, paths,
provenance payload или credential. Deploy, live provider и общий baseline
выполняются только root после отдельного допуска. Ручное исправление snapshot,
chmod вместо read-only mount и unsafe fallback запрещены.

## Ограниченная проверка записи

Из интеграционного checkpoint controller `2f4619188` перенесены только
принадлежащие runner изменения и согласованное правило shared workspace
policy: `/workspace/context` read-only. CP consumer этого правила реализован
в #1046 (`8125c13db`, включён в `98a71da1e`); прежние snapshots с четырьмя
правилами несовместимы, поэтому требуется совместная поставка producer/runner.
Текущий draft дополнительно содержит exact controller prerequisite для
согласованного v7 контракта; после слияния #1025 его diff нормализуется.

Readiness больше не выполняет файловые syscall из HTTP handler. Monitor
запускает `runtime-workspace-canary` с бюджетом 2 секунды, затем 1 секунда
отводится на cleanup после SIGTERM. Процесс ограниченно завершается и
ожидается; credentials из environment не передаются. Проверка повторяется
через 5 секунд, результат старше 10 секунд отвергается. Остановка monitor
отменяет проверку, ожидает её завершения и очищает readiness snapshot.

Canary проверяет create/read/atomic-replace/read/delete в writable дереве,
исключает immutable context из writable quota, отвергает FIFO/hardlink и
удаляет временные файлы при кооперативной отмене. Зависший файловый syscall
не маскируется положительным health. Init и completion используют тот же
ограниченный helper. Проверки non-root записи, cancellation/cleanup,
timeout/kill/Wait и отсутствия I/O в readiness входят в runner race suite.
