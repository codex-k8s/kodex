---
id: JOB-DOC-AGENT-RUNNER
title: Agent runner
type: service
status: approved
owner: backend
version: 2.1.0
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
| Session, Turn, FIFO, attempt | `control-plane` | exact `SessionID`, `TurnID`, attempt, input digest и Turn lease | `ClaimTurn`, `RenewTurn`, status/progress |
| RuntimeExecution | `control-plane` | execution version/fence/generation и agent-runner authority | только readback; terminal пишет `runtime-controller` |
| Kubernetes Pod/PVC/journal | `runtime-controller` | immutable v2 input, execution-scoped ServiceAccount и controller-owned ConfigMap | init materialization и signed terminal handoff |
| Provider account | `control-plane` RuntimeRevision | ID/version/SHA-256 и exact Codex auth digest | свежий `CODEX_HOME`; иной account закрыто отклоняется |
| Materialized bytes | Artifact owner + `interaction-gateway` object store | artifact ID/version/SHA-256, mTLS и bearer owner readback | запись только через `openat(O_NOFOLLOW)` в `/workspace` |
| Mattermost delivery | `interaction-gateway` | durable `interaction_delivery_work` из owner transaction | runner никогда не получает Mattermost token |

Защищённые RPC runner: `CheckReadiness`, `ClaimTurn`, `RenewTurn`,
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
6. Runner заново создаёт `config.toml`, `auth.json`, env allowlist и required
   loopback MCP binding. Локальный proxy добавляет bearer и для readiness, и
   для рабочих запросов применяет один exact upstream TLS 1.3/mTLS/SNI/CA path,
   запускает exact `codex app-server --strict-config --listen stdio://`,
   выполняет `initialize/initialized`, затем `thread/start` или
   `thread/resume`, `turn/start` и строго разбирает newline-delimited JSON-RPC.
   Turn lease продлевается независимо от provider transport.
7. Progress/status создают durable owner deliveries. Final Markdown chunks,
   files и images собираются только из bounded outbox.
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
| renew | control-plane | каждые 10 секунд | version/attempt/generation/token |
| progress/status | control-plane | specialized RPC | execution+turn tuple и sequence |
| capacity retry | runner, внутри той же допустимой attempt | 1/3/5 минут | только exact `CodexErrorInfo=serverOverloaded`; context отменяет ожидание |
| auth/policy/config/cyber blocked | control-plane terminal owner | handoff `BLOCKED` | не классифицируется как capacity |
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
symlink/hardlink и общий payload больше 512 KiB отклоняются.

`CODEX_HOME` всегда существует в session PVC. Перед каждым ходом создаются
новые `config.toml` и secret `auth.json`; environment содержит только `PATH`,
`HOME` и `CODEX_HOME`. MCP bearer остаётся в runner-owned loopback proxy и не
передаётся процессу Codex. Структурированный thread ID принимается только из
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

Resume разрешён только для `CodexSessionID`, связанного с тем же provider
binding в RuntimeExecution. Для восстановленного session control-plane также
закрепляет exact `.matter-codex/state/codex-home/sessions/YYYY/MM/DD/rollout-*.jsonl`,
SHA-256 и `codex-app-server-rollout-v1` provenance исходного execution.
Mismatch, symlink, hardlink, special file или oversized rollout останавливают
запуск до `thread/resume`. Перед каждым turn повторно проверяется digest
установленного `auth.json`; account snapshot не выводится.

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
