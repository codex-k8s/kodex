---
id: SVC-MC-005
title: runtime-controller
type: service
status: approved
owner: developer
version: 3.3.0
updated: 2026-10-07
---

# runtime-controller

`runtime-controller` материализует server-owned execution attempts в
Kubernetes. Он не владеет Project, Agent, Session, Turn, Run lifecycle,
Human Gate, integration grant или terminal result.

## Кто запускает агентов

1. `control-plane` создаёт immutable `RuntimeRevision` и выдаёт exact attempt.
2. `runtime-controller` claim-ит работу и проверяет fence/generation.
3. Controller повторно вычисляет digest полного snapshot: image, environment,
   tools, grants, provider, role-image ABI, prompt, STT и workspace policy.
   Missing, stale или несовпадающий snapshot закрыто отклоняется.
4. Для обычного turn Secret Broker создаёт immutable credential projection,
   связанную с exact project/session/turn/attempt/lease/method/input digest.
5. Только после проверки descriptor создаётся новый Pod из exact promoted role
   image.
6. Защищённый `agent-runner` внутри image запускает provider runtime и MCP.
7. Terminal handoff проверяется control-plane; только после readback Pod
   удаляется. Retry/continuation получают новую attempt и новый Pod.

Каждая роль может использовать свой Docker image с собственными утилитами,
пакетами и программным окружением. `role-image-builder` собирает этот image через
BuildKit с process sandbox в обязательном Pod user namespace, а image admission проверяет provenance, SBOM, vulnerability
policy, signature, promotion и runtime ABI. Controller допускает только
`repository@sha256` из настроенного promoted repository.

## Runtime contract

Typed Skills/Memory materialization и карта owner→controller→runner описаны в
[`OPS-RUNTIME-1025`](../../../docs/operations/runtime-context-1025.md).
`skills.json` содержит только `RuntimeSkillBundle`, `memories.json` только
`RuntimeMemoryRecord`; environment tools и knowledge artifacts не подменяют
эти виды. Исполняемый input требует явный `context_snapshot`, даже для пустого
контекста. Changed pins меняют RuntimeRevision и warm compatibility digests.

Канонический input — `kodex.agent-runner-input.v8`, схема находится в
`contracts/runtime-controller/v8/agent-runner-input.schema.json`, типы — в
`libs/go/runtimecontract`. Input связывает organization/project/agent/session/
turn/run/node/attempt, revision digest, role image digest, bounded input,
capabilities и credential references. Payload не назначает owner или lineage.

`assistant_scope=NONE|SYSTEM|PROJECT` — единственный runtime discriminator;
старого `system_assistant` и совместимого decoder нет. PROJECT требует точные
`project_ref` и `assistant_profile_ref`, закреплённые immutable owner revision.
Оба помощника получают MCP настройки платформы, но только SYSTEM использует
организационную credential materialization и warm runtime. Выбор PROJECT в
контексте экрана не меняет SYSTEM resource authority. Собственная настройка
проектного помощника использует обычные project operations и exact Agent/environment
refs, без организационных credential или `systemAssistantRef`.

Protected init/runner входят в trusted runtime ABI role image. Provider process
работает без Kubernetes token и authority credential. Role Pod не получает
control-plane DSN, registry writer/admin, secret-store authority, Mattermost
token или managed integration credentials.

Каждая attempt получает immutable ConfigMap из девяти файлов:
`runtime.json`, `workspace-policy.json`, `inputs.json`, `results.json`,
`skills.json`, `memories.json`, `mcp.json`, `callback.json` и
`provider-auth.sha256`. Execution ticket Secret содержит только `runtime.json`
и одноразовый callback token. Provider и runtime env читаются из отдельного
immutable Secret Broker projection; controller не читает и не копирует её
значения. Session state использует PVC с exact organization/project/session
annotations. Повторное использование PVC с другой boundary запрещено.

Init container скачивает exact VFS artifacts через execution-scoped mTLS
callback, проверяет manifest/digest и завершает запись до запуска runtime.
`/workspace/input`, `/workspace/knowledge`, ConfigMap, ticket, credentials и
callback certificates смонтированы read-only в рабочих контейнерах.
`/workspace`, result outbox и session state доступны на запись через отдельные
bounded volumes; root filesystem всех контейнеров read-only.

## MCP

MCP не заменяется generic RPC. RuntimeRevision материализует только разрешённые
типизированные MCP servers/tools. Platform MCP tools отображаются в
специализированные control-plane commands; managed integration MCP выполняется
`integration-gateway`. Secret values и raw provider/tool payload не входят в
domain events или browser stream.

Callback и MCP requests несут exact organization/project/run/node/session/
turn/attempt, RuntimeRevision digest, input digest, method и binding digest.
Проверка capability/grant выполняется по той же RuntimeRevision и не расширяет
eligibility из payload.

Execution с owner `file_catalog` получает пять read-only инструментов:
`search_files`, `get_file_metadata`, `preview_file`, `get_file_manifest`, `read_file`.
Контроллер закрепляет descriptor в RuntimeRevision и execution/MCP digest до
materialization. Tools берут lease/fence/generation/catalog из проверенного
callback input; caller выбирает только объявленный purpose, query/page либо
exact entry/ref/revision/digest. Search и manifest ограничены 100 строками,
cursor — 512 символами, preview — 16KiB. Произвольного path/project selector
нет. Ответ проверяется по catalog, purpose, project и exact file pins до
выдачи агенту; activity содержит только purpose и catalog grant.

`read_file` читает UTF-8 без NUL страницами до16KiB: exact entry/artifact/revision/
digest сохраняются между вызовами, первый `offset_bytes=0`, следующий равен
`next_offset_bytes` предыдущего ответа. `eof=true` означает конец файла;
отдельная страница не доказывает полное чтение сотрудником. Перед каждой
страницей controller принимает полный источник в существующий private spool,
проверяет полный SHA/размер, owner Complete и EOF, валидирует весь UTF-8 текст,
затем снова проверяет exact metadata и текущие права. `chunk_digest` связывает
только страницу, `source_digest` — полностью проверенный источник. Тексты,
spool paths и credentials не входят в activity или диагностику. Offset внутри
UTF-8 rune отклоняется; размер страницы4..16384 байта обеспечивает progress.
Операция ограничена45секундами с резервом RequestTimeout для terminal audit;
общий MCP timeout60секунд и transfer limits сохраняются. Кэш отсутствует:
каждая страница заново читает источник, поэтому стоимость пропорциональна
числу страниц и размеру файла. Ошибка/отмена/отзыв не выдают partial text.

Локальная ошибка формы параметров возвращает `FILE_INPUT_INVALID` и безопасную
подсказку исправить вызов один раз по schema/exact manifest pins. Это не отказ
authority и не автоматический повтор прежнего запроса. Недоступный terminal
audit, owner denial, checksum или metadata mismatch сохраняют закрытый
`TOOL_UNAVAILABLE` без retry; тела/идентификаторы из input не отражаются в ошибке.

Новый каталог требует согласованной доставки controller, control-plane и
role image agent-runner с тем же закрытым набором из пяти file tools. До
активации exact role image новые executions с `file_catalog` не запускаются:
старый consumer закономерно отклоняет несовпадение фактического tools/list.
Catalog readiness не ослабляется. Execution без `file_catalog` сохраняет
свой прежний профиль; отсутствие artifact capability не выдаёт file tools.

Вклад owner642: `ccefadda86f25370924a5a4fd19f57d7ace7ae85`.
Локальные generated gRPC и authenticated callback tests проверяют все четыре
операции, подмену snapshot/file и отказ при недоступном обязательном audit.
Полное тело читается отдельным `StreamExecutionArtifact`: initial Proto request
связан с proof, owner повторяет текущую eligibility перед Complete. Controller
принимает chunks до64KiB и файл до512MiB в private spool; checksum, размер,
Complete и EOF проверяются до HTTP headers. Metadata возвращает относительный
download descriptor без credential; catalog selector сверяется через отдельный
GetExecutionFileMetadata перед body stream. Input/Skill callbacks используют
тот же stream, сохраняя собственные immutable pins.

Отдельный disk-backed `artifact-spool` emptyDir ограничен2Gi и монтируется
только controller по `/var/lib/kodex/runtime-controller/artifact-spool`, отдельно
от read-only authority `/run/kodex`. Alias `/var/run -> /run` не используется
для writable mount. Внутренний каталог0700 и files0600 принадлежат process UID;
файл unlink сразу после создания и исчезает при close/crash. Не более двух
полных transfers одновременно, readiness выполняет небольшой writable canary.
Timeout owner stream до2m, HTTP delivery имеет отдельный ограниченный бюджет.
Публичная локальная проверка: `make test-runtime-controller-artifact-transfer`.
Consumer agent-runner требует согласования HTTP timeout и отдельного local
download bridge; этот хвост и live protected transfer пока NOT RUN.
Эти проверки не заменяют live agent/workspace acceptance.

### Полное чтение конфигурации помощником

Native `get_configuration_catalog` для `WORKFLOW_CONFIGURATION` и
`AGENT_CONFIGURATION` возвращает только `configuration_page`, а не огромный
альтернативный полный JSON. Владелец по-прежнему разрешает точные
lease/fence/generation, organization/project, выбранные entity/ref/version;
controller проверяет полный закрытый snapshot и canonical SHA до страницы.
Внутренний Proto, права и авторитетное состояние не меняются.

Первое чтение использует `configuration_offset_bytes:0`. Продолжение передаёт
точные `next_offset_bytes` и `configuration_sha256` из предыдущего ответа;
отсутствующий или несовпадающий digest закрыто отклоняется. `maximum_bytes`
ограничен4..4096, page сохраняет UTF-8 boundary, общий размер, фактические
offset/next, `eof` и `page_sha256`. Полное чтение означает конкатенацию
непересекающихся text страниц доEOF и проверку общего digest. Страница не
считается полным snapshot. Объём JSON-encoded модельной выдачи не более8KiB:
при escaping уменьшается именно размер страницы с честным next offset,
не отбрасываются поля и не вставляется ellipsis.

Каждая страница заново проходит owner read и exact version: drift, terminal,
отзыв, чужой context или повреждённый snapshot не выдаёт partial text.
Не создаются файлы, credentials, новые grants, кэш или второй источник данных.

## System assistant

Системный помощник использует отдельный always-hot Pod. Reconciler поддерживает
exact desired prompt/runtime revision, heartbeat и resource limits. Idle не
является активным Turn; turns идут FIFO. После process/Pod restart warm runtime
восстанавливается до положительной assistant readiness. Turn с новой file
projection выполняется в отдельном execution Pod; warm Pod принимает только
совместимый turn без новых VFS files. Этот Pod не получает
DB, Kubernetes или secret-store authority.

## Health и readiness

- `/healthz` проверяет только жизнь процесса;
- `/readyz` читает локальный snapshot;
- control-plane, provider, integration и interaction gateways не входят в
  Kubernetes readiness;
- недоступный рабочий сосед возвращает typed `Unavailable`;
- Kubernetes observation может использовать bounded LKG только при transport
  failure; digest/signature/revision conflict или expiry закрывают путь сразу;
- отказ и восстановление логируются один раз как переход состояния.

Readiness `role-runtime` выполняет bounded create, write, `fsync`, atomic
rename, read, delete и directory `fsync` canary в result outbox с обязательным
cleanup. Наружу возвращается только `READ_ONLY`, `QUOTA_EXCEEDED`,
`PATH_OUTSIDE_WORKSPACE` либо `RUNTIME_IO_ERROR`, без file body и local path.

Terminal/cancel/delete/lease expiry закрываются в owner transition
control-plane. Controller удаляет execution Pod, ticket, ConfigMap,
ServiceAccount, Role/RoleBinding и NetworkPolicy; Secret Broker reconciler
удаляет ставшую невалидной credential projection. Retry/continuation всегда
создаёт новую attempt, lease, RuntimeRevision и projections. Результат читается
через `ControlPlaneQueryService.GetArtifact`/`ListArtifacts` и
`ArtifactTransferService.DownloadArtifact`, а не из удаляемого Pod.
`RuntimeWorkService.StreamExecutionArtifact` предназначен только для exact input
активной lease и после terminal не является путём чтения результата.

## Матрица жизненного цикла и полномочий

| Сценарий #1025 | Инициатор и authority | Owner-команда и результат | Consumer/cleanup |
| --- | --- | --- | --- |
| Claim/materialize | runtime-controller, mTLS + signed workload context | `ClaimExecution`: owner выбирает revision, attempt, fence, grants | `BuildTurnInput` пересчитывает digest; broker descriptor; immutable ConfigMap/ticket/PVC; Pod |
| Input/knowledge | init, callback mTLS + ticket + exact execution headers | `StreamExecutionArtifact`: active lease, pinned artifact/manifest, tenant; initial request digest и final owner read | bounded private spool, Complete/EOF/digest verification до выдачи bytes; read-only mount |
| MCP | runner, те же headers и MCP binding | специализированная owner-команда, точный effective grant | callback не объединяет grants проекта |
| Retry/continuation | owner graph command | прежние lease/grants закрыты; `ClaimExecution` получает новую revision/attempt | новый Pod/ticket/ConfigMap; прежний attempt не переиспользуется |
| Terminal | runner completion с attempt, revision, ticket | `CompleteExecution` сохраняет результат/receipt у owner | HTTP ACK после commit; bounded cleanup в handler; Artifact API сохраняет read path |
| Cancel/delete/expiry | owner transition; renew старой lease отклоняется | authoritative owner state, без нового controller domain event | удаление Pod/ticket/ConfigMap/RBAC/network; broker отзывает projection |
| Restart/partial materialization | избранный controller leader | owner lease/retry остаётся источником истины | `CleanupStaleTurns` убирает старые и orphan projections; PVC сохраняется по retention policy |

Глобальный system-assistant без project поддерживается shared execution binding
и совместимым warm path. Холодная credential projection такого turn пока
блокируется project-required policy Secret Broker и owner resolver. Подстановка
чужого project либо обход broker не допускаются; этот межсервисный пробел
требует согласования #1023/#1024 перед полной приёмкой #1025.

Shared ABI согласован с consumer #1026 (`e62b8d9996b02230cbe08443468881269d095f51`):
completion содержит обязательный `attempt`; execution/MCP binding покрывает
полный materialized input; workspace policy защищает `codex-home/auth.json`.
Каноническая JSON-модель сохраняет empty/nil optional collections после
round-trip. Warm compatibility сохраняет organization/session/agent и профиль
Kubernetes access, но не execution-local ServiceAccount, который меняется
между attempts. Полный runner в эту ветку не переносится.

## Локальная проверка

Warm reconcile различает отказ owner RPC и отсутствие `DesiredRevision` в
успешном ответе. Для `ReconcileWarmRuntime` и `ReportWarmRuntime` локальная
граница логирует только закрытый `grpc_code` и, для типизированного
`LocalAuthorityError`, существующий `Diagnostic`. Upstream message, details,
correlation, refs и исходная цепочка ошибок не сохраняются. Диагностика не
меняет authority, timeout, повтор reconcile или переходы readiness.

```bash
cd services/internal/runtime-controller
GOWORK=off go test ./internal/credentialprojection ./internal/workload ./internal/app ./internal/callback
```

Deployment: `deploy/k8s/base/runtime-controller`. Нормативная архитектура:
`docs/architecture/runtime-controller.md`.
