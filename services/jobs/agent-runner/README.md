---
id: JOB-DOC-AGENT-RUNNER
title: Agent runner
type: service
status: approved
owner: backend
version: 2.2.0
updated: 2026-08-04
---

# Agent runner

`agent-runner` — самостоятельно поставляемый Go job/runtime process. Один
процесс исполняет ровно один server-owned Turn attempt. Он не выбирает следующий
Turn, provider account, retry attempt, cancel или transport route и не хранит
авторитетное состояние Session/Turn/Process/Schedule/RuntimeExecution.

## Границы владения и доверия

| Вид | Авторитетный владелец | Доказательство runner | Эффект |
|---|---|---|---|
| Session, Turn, FIFO, attempt | `control-plane` | exact `SessionID`, `TurnID`, attempt, input digest и Turn lease | `ClaimTurn`, status/progress; обе lease продлевает heartbeat controller |
| RuntimeExecution | `control-plane` | execution version/fence/generation и agent-runner authority | только readback; terminal пишет `runtime-controller` |
| Kubernetes Pod/PVC/journal | `runtime-controller` | immutable v2 input, execution-scoped ServiceAccount и controller-owned ConfigMap | init materialization и signed terminal handoff |
| Provider account | `control-plane` RuntimeRevision | ID/version/SHA-256 и exact Codex auth digest | свежий `CODEX_HOME`; иной account закрыто отклоняется |
| Materialized bytes | Artifact owner + `interaction-gateway` object store | artifact ID/version/SHA-256, mTLS и bearer owner readback | запись только через `openat(O_NOFOLLOW)` в `/workspace` |
| Mattermost delivery | `interaction-gateway` | durable `interaction_delivery_work` из owner transaction | runner никогда не получает Mattermost token |

Защищённые RPC runner: `CheckReadiness`, `ClaimTurn`,
`GetRuntimeExecution` и `ReportRuntimeProgress`. `CompleteTurn`, generic work
claims и integration continuation RPC отсутствуют в профиле. Защищённые RPC
gateway materialization: `GetRuntimeMaterialization`. Terminal становится
авторитетным только после проверки Ed25519 handoff и
`CompleteRuntimeExecution` в owner transaction runtime-controller.

## Сквозной граф

1. `control-plane` создаёт `PENDING RuntimeExecution` с exact revision,
   provider binding и набором materializations.
2. `runtime-controller` фиксирует journal и создаёт immutable input, handoff
   ConfigMap, credential projection, отдельный `runtime-access-*`
   ServiceAccount и новый execution-scoped Pod. RoleBinding этого ServiceAccount
   видит только ConfigMap и credential Secrets данного execution. Это разрывает
   bootstrap-цикл: admission ещё не выполнен.
3. Init container по mTLS TLS 1.3 и bearer читает каждый object через
   `interaction-gateway → GetRuntimeMaterialization → private object store`.
4. Runner запускает required MCP readiness тем же SNI/CA/certificate/bearer
   путём, которым будет пользоваться Codex, затем захватывает Turn lease.
5. Повторный reconcile runtime-controller теперь допускает execution и выдаёт
   RuntimeExecution lease. Runner видит `ADMITTED/RUNNING` readback.
6. Trusted runner создаёт required MCP authority proxy на protected UDS с
   `SO_PEERCRED`, но не запускает Codex. Provider-side loopback bridge не имеет
   upstream bearer/mTLS и может обратиться только к этому UDS с локальной
   capability; рабочий и readiness запросы проходят один TLS/bearer путь.
   Отдельный `provider-runtime` UID 10002 не имеет mounts Kubernetes token,
   application grants, mTLS keys, MCP bearer, handoff key и authority socket.
   Он принимает один bounded запрос через UDS только от peer UID 10001,
   создаёт свежий `config.toml`, загружает exact provider credential через
   `account/read` и запускает exact
   `codex app-server --strict-config --listen stdio://`. Custom permissions
   запрещают model shell читать `CODEX_HOME`, `/proc`, projected secrets и
   authority paths; shell env не наследует provider state. `auth.json`
   удаляется после bounded завершения app-server.
   Adapter выполняет `initialize/initialized`, затем `thread/start` или
   `thread/resume`, `turn/start` и строго разбирает newline-delimited JSON-RPC.
   Turn lease продлевается независимо от provider transport.
7. Progress/status создают durable owner deliveries. Final Markdown chunks,
   files и images собираются только из bounded outbox. Крупные значения до
   256 MiB перед handoff уходят по exact execution grant в private
   content-addressed store `interaction-gateway`; handoff несёт immutable
   Artifact references, а ошибка отдельного файла остаётся bounded terminal
   Markdown и не уничтожает итог.
8. Runner подписывает один v2 envelope и атомарно обновляет exact handoff
   ConfigMap. Runtime-controller проверяет закрытый trust set, tuple, payload
   digests и живую RuntimeExecution lease.
9. `CompleteRuntimeExecution` одной owner transaction закрывает execution,
   Turn/Process, leases/grants и создаёт durable deliveries. Gateway выполняет
   at-least-once Mattermost effect и сохраняет provider receipt/readback.

## Lifecycle matrix

| Переход | Владелец | Runner | Идемпотентность и закрытый отказ |
|---|---|---|---|
| create/PENDING | control-plane | отсутствует | server-owned attempt/generation/revision/input |
| materialize/bootstrap | runtime-controller | init only | object version+digest и immutable ConfigMap |
| claim | control-plane | `ClaimTurn` | UUIDv5 execution key, exact FIFO head |
| admit | control-plane + runtime-controller | ждёт readback | допустим только после живой Turn lease |
| renew | control-plane + runtime-controller | runner не вызывает generic `RenewTurn` | controller heartbeat атомарно продлевает RuntimeExecution и TurnLease |
| progress/status | control-plane | specialized RPC | execution+turn tuple и sequence |
| capacity retry | runner, внутри той же допустимой attempt | 1/3/5 минут | только exact `CodexErrorInfo=serverOverloaded`; context отменяет ожидание |
| auth/quota/policy/config/cyber blocked | control-plane terminal owner | handoff `BLOCKED` | не классифицируется как capacity; quota не получает Retry до обновления account binding |
| cancel/SIGTERM | control-plane/runtime-controller | останавливает process group, 10-секундная grace | runner не назначает cancel authority |
| complete | runtime-controller + control-plane | только signed handoff | один winner; иной payload — conflict |
| crash/expiry | owners | handoff отсутствует | incident/watchdog/expiry, без generic complete |
| retry | control-plane | новый process/Pod | новая attempt, lease, grant и revision |
| successor | control-plane/runtime-controller | новый Pod, тот же retained PVC | свежие credentials/config/MCP; stale process не переиспользуется |
| archive/restore | runtime-controller + control-plane | app-server rollout JSONL остаётся в PVC | exact returned path/checksum/provenance фиксируются execution owner, входят в handoff и immutable PVC archive; перед resume runner повторно проверяет regular file и digest |

## Материализация и outbox

Git не обязателен. Snapshot repository не распаковывается автоматически.
Absolute path, `..`, symlink, hardlink, special file, duplicate path и выход из
workspace отклоняются. Запись выполняется через temporary regular file,
`fsync`, digest readback и atomic rename. Пользовательские outputs разрешены
только как top-level regular files в `.matter-codex/outbox`; directory,
symlink/hardlink, special file и выход за bounded 256 MiB на artifact
отклоняются. Одновременно runner удерживает не более 256 MiB проверенных outbox
bytes и не более 31 immutable refs; остальные per-artifact outcomes входят в
bounded terminal Markdown. Inline handoff остаётся малым; крупный Markdown, file и image
сначала проходят `AuthorizeRuntimeOutput`, private S3 put/readback и
`RegisterRuntimeOutput`. Runner не получает S3 credential.

`CODEX_HOME` всегда существует в session PVC. Перед каждым ходом provider
container создаёт новый `config.toml`, проверяет exact digest `auth.json` и
держит его только для app-server до завершения turn. Custom permission profile
выбирает exact base `read-only` → `:read-only` либо `workspace-write` →
`:workspace`, затем задаёт deny-read для `CODEX_HOME`, `/proc`, projected
secrets и authority paths; shell env имеет только bounded `PATH` и `HOME`.
Настоящий session MCP bearer и mTLS keys остаются в trusted runner-owned UDS
authority proxy. Provider-side loopback bridge не содержит этих credentials;
его соединение проверяется по `SO_PEERCRED` UID 10002. App-server получает
только свежую локальную capability через `bearer_token_env_var`; model shell не
наследует её и не имеет network allow.
`danger-full-access`, неизвестный режим и caller sandbox override закрыто отклоняются.
Структурированный thread ID принимается только из
коррелированного `thread/start|resume` response и сверяется с server-owned ID.
Stderr и provider `message` ограничиваются и отбрасываются: ни одно из них не
становится authority. JSON-RPC ограничен 1 MiB на строку и 100000 сообщений;
фактический rollout JSONL из возвращённого `thread.path` — 64 MiB.
`CodexErrorInfo` разбирается как закрытый tagged union схемы 0.144.1. Только
`serverOverloaded` допускает retry 1/3/5; `usageLimitExceeded`, `unauthorized`,
`cyberPolicy`, неизвестный либо отсутствующий variant формируют безопасный
owner-visible terminal без capacity retry. Любой server request, включая
approval, user input, dynamic tool, token refresh и attestation, отклоняется:
runner не выдаёт себе дополнительных полномочий.

Resume разрешён только для server-owned последней terminal Codex lineage той
же Session и того же logical provider binding ID. Credential version/digest
берутся из свежей RuntimeRevision и могут измениться при reauth той же logical
учётной записи. Lineage не зависит от cleanup state; archive restore остаётся
отдельной проверяемой границей. Для восстановленного session control-plane также
закрепляет exact `.matter-codex/state/codex-home/sessions/YYYY/MM/DD/rollout-*.jsonl`,
SHA-256 и `codex-app-server-rollout-v1` provenance исходного execution.
Mismatch, symlink, hardlink, special file или oversized rollout останавливают
запуск до `thread/resume`. Перед каждым turn повторно проверяется digest
`auth.json`; эта проверка выполняется до `ClaimTurn`, а отрицательный результат
после admission становится owner-visible signed `BLOCKED` handoff с device-code
действием. Account snapshot не выводится.

Mattermost run card создаётся durable owner delivery. Для `QUEUED`, `CLAIMED`,
`ADMITTED` и `RUNNING` она содержит Stop, а для утверждённых `FAILED` и
`EXPIRED` — Retry.
`interaction-gateway` повторно проверяет Mattermost actor, channel/root card,
callback token и replay receipt, затем вызывает специализированный
`ManageRuntimeAction`. Control-plane сам разрешает exact owner graph: Stop
закрывает queued либо active execution, Retry создаёт новую attempt,
RuntimeRevision и grant. Stale card, duplicate terminal и cancel/complete race
закрыто отклоняются; runtime-controller останавливает Pod только после owner
cancel readback. Любой terminal execution сразу удаляет execution-specific Pod
с provider sidecar; retained PVC остаётся единственным warm state, а successor
всегда получает новый Pod и свежие mounts.

Если provider завершился, но staging хотя бы одного output не прошёл,
terminal становится `FAILED`, а trusted runner сохраняет protected checksum
journal и exact source digest на retained PVC. Card Retry создаёт новую attempt,
которая повторяет только owner staging и signed handoff: app-server/model не
запускается, уже зарегистрированные refs не дублируются. Journal удаляется
только после принятого recovery handoff.

Перед каждым `EnqueueTurn` и созданием свежей RuntimeRevision
`interaction-gateway` вызывает существующий
bot-service transport producer по TLS 1.3/mTLS. Тот разрешает exact
channel/root/bot `AgentSession`, создаёт новый cryptographically random
immutable `TokenSecretRef` для exact execution/turn/attempt и возвращает только
revision/digests. Монотонная binding revision и current tuple меняются одной
AgentSession transaction; predecessor bearer сразу отклоняется, а Secret
удаляется best-effort reconciliation. Exact равный readback идемпотентен.
`BindSessionMCP` закрепляет server-owned credential
binding; credential broker копирует exact Secret только в trusted container.
Required proxy обращается к
`matter-codex-bot-service.mattercodex-system.svc:8443/mcp/sessions/<session>`
с exact SNI/CA/client certificate и настоящим execution-fenced session bearer.
Stale secret, revision, execution tuple или чужая Session закрывают readiness
до `ClaimTurn`.

## Проверенная документация Codex

Для закреплённого `@openai/codex@0.144.1` проверены официальные разделы
Configuration Reference, Environment Variables, Authentication and sessions,
CLI command reference, Model Context Protocol и Non-interactive mode. Они
подтверждают семантику `CODEX_HOME`, secret `auth.json`, `config.toml`,
`codex login --device-auth` и required MCP. Provider adapter дополнительно
сверен с официальным upstream `codex-rs/app-server/README.md`,
`codex-rs/protocol/src/error.rs` exact tag `rust-v0.144.1` и схемами, созданными
закреплённой командой `codex app-server generate-json-schema`: app-server
использует newline-delimited JSON-RPC, `thread/resume`, `turn/interrupt` и
закрытый `CodexErrorInfo`. `codex exec --json` проверен только как историческая
CLI-возможность и не является runtime contract этого unit. Попытка получить
те же актуальные материалы через Context7 выполнена, но сервис вернул
исчерпание месячной квоты; поэтому использованы официальные источники OpenAI.

## Lifecycle процесса

Image использует проверенный root-owned `mattercodex-init` как PID 1,
subreaper и signal proxy; shell wrapper отсутствует. Runner создаёт отдельную
Codex process group, сначала отправляет exact `turn/interrupt`, после bounded
deadline завершает process group, а init возвращает исходный exit code runner.
Readiness открывается только после materialization, MCP, control-plane working
path, Turn claim и RuntimeExecution admission. Cleanup выполняется до закрытия
clients; его bounded contexts создаются от неотменённого base context.
Base `NetworkPolicy` не содержит внешнего wildcard: `render-agent-runner.sh`
подставляет непустой Ed25519 handoff trust и добавляет только утверждённые
Kubernetes API и provider `/32|/128` endpoints. Без этих additive policies или
с placeholder key environment закрыто не готово к deploy.
Vault Kubernetes role `internal-rpc-authority-agent-runner` обязан принимать
только ServiceAccount с префиксом `runtime-access-` в `mattercodex-system`;
статическая общая Pod identity запрещена, иначе параллельные execution получили
бы объединённые handoff permissions. После удаления Pod controller удаляет
ServiceAccount и exact Role/RoleBinding с UID/resourceVersion preconditions.

Environment manifest создаётся только через `scripts/render-agent-runner.sh`
с exact `--handoff-key-id`, canonical 32-byte `--handoff-public-key-base64`,
Kubernetes API CIDR/ports и provider CIDR. Public key не является секретом, но
его key ID обязан соответствовать `handoff-private-key` CredentialBinding
активной RuntimeRevision. Staging и production overlays не содержат пригодного
к запуску placeholder trust.

## Отрицательная ручная проверка

- изменить artifact SHA/version либо `Content-Length`: init обязан завершиться;
- подменить SNI/CA/client certificate или bearer MCP: readiness остаётся false;
- заменить projected file symlink/hardlink или сделать writable: invocation
  закрыто отклоняется;
- передать unknown JSON-RPC method/authority field, неизвестный response ID,
  два terminal event либо session ID другого account: terminal handoff не
  создаётся;
- вернуть `usageLimitExceeded`, `unauthorized`, `cyberPolicy`, unknown или
  missing `codexErrorInfo`: retry 1/3/5 не выполняется, owner получает
  безопасный terminal code без provider text;
- вернуть approval/user-input server request: runner отвечает protocol error,
  отправляет interrupt и не расширяет authority;
- завершить Pod без handoff: runtime-controller фиксирует incident;
- повторить тот же handoff: no-op; изменить payload: conflict;
- отключить gateway после owner commit: delivery остаётся durable и reclaimable;
- запустить следующий Turn: создаётся новый Pod и свежая RuntimeRevision, а PVC
  сохраняет session state.

Тяжёлый integration/E2E/deploy/render/lifecycle контур отложен в
[Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).
