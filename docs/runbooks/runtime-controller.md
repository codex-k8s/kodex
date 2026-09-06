---
id: RUN-MC-008
title: Диагностика runtime-controller
type: runbook
status: approved
owner: sre
version: 3.1.0
updated: 2026-09-05
---

# Диагностика runtime-controller

## Role Pod path

### Ограниченное завершение (#1073)

Role Pod имеет exact `terminationGracePeriodSeconds=150`, закреплённый также
admission. Три обычных контейнера не имеют гарантированного порядка SIGTERM;
relay drain60s сохраняет финальный credential commit, runner имеет отдельные
57s чтения provider result,60s completion и20s cleanup.

Выход одного provider/relay контейнера не позволяет немедленно заменить
измеренный результат пустым failure: controller ждёт callback120s. При
execution timeout либо добровольном shutdown StopTurn удаляет только Pod,
сохраняя ticket, projection и NetworkPolicy для exact callback. Текущая lease
продолжает проходить owner RenewExecution; отказ owner закрывает execution,
не продлевая отозванный grant. После истечения grace используется прежний
неизменный failure receipt, cleanup выполняется после durable owner success.

Controller прекращает claim, но удерживает Kubernetes leadership и callback
listener до завершения bounded drain. Потеря leadership является отдельным
закрытым отказом, включая потерю уже во время drain. Максимум drain210s
покрывает stop10 + callback120 + failure retry60 + cleanup10 и запас10s;
callback listener имеет ещё5s, controller Pod grace240s оставляет время
на закрытие listener и telemetry. Это не расширяет deadline owner grants.

При ручной проверке сверить metadata Pod grace, leader Lease holder до/после
завершения, terminal receipt и отсутствие раннего удаления execution ticket/
NetworkPolicy. Не выводить ticket data. Потеря ACK либо отказ refresh остаются
ошибкой, а не подтверждением credential effect. Owner cancel может запретить
поздний callback; drain не обещает приём данных после owner terminal.

| Переход | Авторитетный результат | Закрытие |
| --- | --- | --- |
| Обычный terminal | Exact CompleteExecution receipt с измеренным Usage | Callback удаляет runtime material после owner commit |
| Provider timeout/refresh failure | Частичный результат и FAILED receipt, без подтверждения неизвестного refresh | Независимый completion, без повторного provider effect |
| Pod SIGTERM | Текущий lease и результат после provider/relay drain | Callback120s предшествует fallback |
| Controller timeout/shutdown | StopTurn, действующая owner lease, bounded receipt readback | Сначала join текущих turns, затем listener/election/dependencies |
| Owner cancel/revoke | Owner отказывает в renew/callback | Немедленное закрытие, без replacement grant и обещания late Usage |
| Leadership lost | Дальнейшие claims и drain renewal запрещены | Stop/delete текущего Pod и coordinator close |
| Slow peer | Relay deadline закрывает incomplete request без commit | Listener/connection/callback cancel и join в пределах60s |

Локальные regression используют fake Kubernetes readback, настоящий HTTP
listener callback и owner RPC stub; runner companion — настоящий Unix relay.
Live/SRE deployment и совместный acceptance пока NOT RUN. Context7 проверен
для `/golang/go` и `/websites/kubernetes_io`.

После companion локально прошли полный controller race/vet/build и
`make test-web-only-release`: app1.063s, callback1.892s,
credentialprojection1.024s, workload1.779s. Для issuer требуется отдельный
authority companion `a2883dff26496ff4669b69666e310e2433a27708`: обычный issuer
не гарантирует доступность fresh proof в течение controller drain.

Для каждой обычной attempt controller создаёт новый Pod exact promoted role
image. Проверить authoritative execution, RuntimeRevision digest, attempt,
fence/generation, image `repository@sha256`, runtime ABI, ServiceAccount,
resources, PVC и callback ticket. Display role name, prompt или caller-supplied
Kubernetes locator не являются authority.

`kodex.agent-runner-input.v7` должен пройти schema validation. Mutable
tag, image вне promoted repository, ABI mismatch, stale fence, extra container,
broad ServiceAccount или host access закрыто отклоняются admission.

Проверять runtime ticket можно только по metadata: `immutable=true`, labels,
owner Pod и annotations exact RuntimeRevision/runtime config/environment
digests. Не выводить `.data`, `stringData`, decoded `runtime.json` или process
environment. Turn ticket должен содержать ровно `runtime.json` и execution
token; наличие credential value в ticket, control-plane response, runner input,
логах или audit является инцидентом.

Для credential projection сверить только metadata descriptor Secret Broker:
namespace, name, UID, `resourceVersion`, content digest, expiry и exact
project/session/turn/attempt/lease/generation/RuntimeRevision/input binding.
Pod annotations и volume должны ссылаться на тот же immutable
`runtime-credentials-<40 hex>`. `provider-runtime` получает `provider-auth.json`
и разрешённые env keys из этого Secret; `role-runtime` не должен иметь
`env.secretKeyRef` или credential mount. Не читать Secret data. Несовпадение
любого поля требует остановить новые materializations; обход через mutable
Secret или ручную правку ticket запрещён.

Runtime ConfigMap должен быть immutable и содержать ровно девять непустых
файлов: `runtime.json`, `workspace-policy.json`, `inputs.json`, `results.json`,
`skills.json`, `memories.json`, `mcp.json`, `callback.json`,
`provider-auth.sha256`. Их annotations должны совпадать с Pod по organization,
project, session, turn, attempt, execution/MCP binding и всем policy digests.

Skills и Memory являются отдельными typed snapshots, не tools/knowledge.
Сверить metadata exact binding/revision/digest и наличие `context_snapshot`;
содержимое Memory summary и Skill files в диагностику не выводить.
Controller проверяет полный RuntimeRevision digest после hydration. Отсутствие
нового snapshot в producer не исправляется fallback на mutable catalog.

Отдельный `runtime-context` emptyDir (520Mi) монтируется ровно в
`/workspace/context`: init RW, role/provider RO, credential relay без mount.
Admission запрещает alias, subPath и вложенные mounts. Canary не заменяет
проверку реального RO filesystem перед запуском provider.
Подробный callback-контракт и интеграционные зависимости:
[`OPS-RUNTIME-1025`](../operations/runtime-context-1025.md).

## Временные файлы transfer

При причине readiness `artifact_spool` сверить metadata тома `artifact-spool`
(disk emptyDir2Gi, mount только у controller), UID10001 и fsGroup29000,
настройки `RUNTIME_CONTROLLER_ARTIFACT_SPOOL_DIRECTORY` и
`RUNTIME_CONTROLLER_FILE_TRANSFER_TIMEOUT`. Не читать содержимое private
каталога или файловых дескрипторов процесса. Временные файлы unlink сразу после
открытия; отсутствие pathname во время передачи является ожидаемым.
Исчерпание двух transfer slots возвращает503 без частичного body. Нельзя
обходить отказ увеличением unary message limit или общим writable root.
Локальное воспроизведение: `make test-runtime-controller-artifact-transfer`;
он проверяет stream/spool и оба profile renders без обращения к кластеру.

## Always-hot assistant

Проверить одну desired system revision, один warm Pod, heartbeat, resource
limits и observed `READY`. Idle Pod не имеет active Turn. При restart или
revision change controller заменяет materialization; readiness помощника не
может быть положительной до фактического callback/provider warm path.

Assistant runtime получает contextual descriptor и только закрытые tools,
соответствующие server-owned allowed operations. `propose_assistant_metadata`
может предложить bounded title, а `propose_configuration_plan` передаёт только
explicit operations с target/parameters/before/after. Ни один из этих tools не
применяет план и не выдаёт runtime новые полномочия.

Обычный и assistant runtime могут вызвать `propose_run_metadata`. После каждого
terminal MCP call controller отправляет одну bounded проекцию через
`RecordRunToolCall`: tool, safe parameters, exact capability/grant, outcome,
duration и safe result. Ошибка проекции считается ошибкой рабочего path и может
быть безопасно повторена по тому же idempotency key; raw arguments/result в
control-plane не отправляются.

## Probes

- `/healthz` — controller process;
- `/readyz` — локальный рассчитанный snapshot Kubernetes observation,
  authority sidecar и worker loop;
- control-plane/provider/integration/interaction service не вызываются на probe;
- working-path outage возвращает typed `Unavailable` и bounded retry.

Readiness execution Pod проверяет фактический writable result path операциями
create, write, file `fsync`, atomic rename, directory `fsync`, read и delete.
Canary обязан удалить временные файлы. Допустимы только safe reasons:
`READ_ONLY`, `QUOTA_EXCEEDED`, `PATH_OUTSIDE_WORKSPACE`, `RUNTIME_IO_ERROR`.
Reason не должен содержать local path или file body. Для первых трёх проверить
mount mode/quota/symlink либо traversal; `RUNTIME_IO_ERROR` означает прочую
ошибку storage и требует проверки node/PVC events без чтения payload.

Матрица runtime Pod: UID 10001 role/init, UID 10002 provider, UID 10003 relay,
`fsGroup=29000`, `readOnlyRootFilesystem=true`, dropped capabilities и
`RuntimeDefault` seccomp; provider использует только утверждённый optional
AppArmor profile и отдельный seccomp режим для bubblewrap. `input`, `knowledge`,
runtime ConfigMap, ticket, callback TLS и credentials read-only. На запись
допустимы workspace/result outbox, session PVC, `/tmp` и закрытые UDS volumes.

Kubernetes transport failure допускает bounded LKG. Signature/digest mismatch,
revision rollback/conflict, expired ticket или grace period немедленно закрывают
materialization. Один и тот же отказ/restore логируется только как transition.

## Cancel, retry, cleanup

Controller не делает Run terminal по состоянию Pod. Cancel приходит как
server-owned graph command, закрывает attempt/grants/leases и затем Pod. Retry
имеет новую attempt/RuntimeRevision/Pod; старый Pod не переиспользуется.
Cleanup разрешён только после signed handoff и authoritative terminal readback;
PVC следует отдельной retention policy. Controller удаляет Pod, ticket,
ConfigMap, execution ServiceAccount, Role/RoleBinding и NetworkPolicy. Secret
Broker recovery удаляет credential projection после owner validation вернул
revoked, terminal, expired или changed binding. Проверить отсутствие старой
attempt и наличие новой; результаты читать через authoritative
`ControlPlaneQueryService.GetArtifact`/`ListArtifacts` и
`ArtifactTransferService.DownloadArtifact`, не через Pod/PVC.
`RuntimeWorkService.ReadExecutionArtifact` используется только для inputs
активной lease; после terminal этот RPC закрыто отклоняется.

Для глобального system-assistant без project холодный запуск пока блокирует
project-required credential projection policy. Не назначать фиктивный project
и не обходить Secret Broker. Совместимый warm turn работает в своей точной
organization/session boundary; холодный путь требует согласования #1023/#1024.

## Локальная проверка

```bash
cd services/internal/runtime-controller
GOWORK=off go test ./internal/credentialprojection ./internal/workload ./internal/app ./internal/callback
cd ../../..
make test-web-only-release
```

При диагностике tool-call projection дополнительно сверить соответствие tool и
capability, наличие grant в immutable RuntimeRevision и отсутствие ключей
`secret`, `token`, `password`, `credential`, `payload` или `raw` в safe
parameters.
