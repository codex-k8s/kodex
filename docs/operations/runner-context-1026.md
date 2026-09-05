---
id: OPS-RUNNER-1026
title: Материализация Skills и памяти в agent-runner
type: operations
status: approved
owner: backend
version: 1.0.0
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

Runtime ошибки содержат только безопасную машинную причину, без bodies, paths,
provenance payload или credential. Deploy, live provider и общий baseline
выполняются только root после отдельного допуска. Ручное исправление snapshot,
chmod вместо read-only mount и unsafe fallback запрещены.
