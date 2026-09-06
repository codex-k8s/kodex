---
id: RUNBOOK-MC-AGENT-RUNNER
title: Диагностика agent-runner
type: runbook
status: approved
owner: sre
version: 2.2.0
updated: 2026-09-04
---

# Диагностика agent-runner

Runner работает внутри promoted role image конкретной attempt. Он не является
отдельным Mattermost bot и не обслуживает следующую обычную attempt после
terminal state.

## Startup

Проверить:

1. input schema `kodex.agent-runner-input.v7` и bounded file mode/size;
2. exact execution/revision/turn/attempt/fence;
3. trusted runtime ABI digest;
4. runtime-controller callback TLS/SPIFFE/ticket;
5. server-materialized instructions с четырьмя service blocks без оставшихся
   template expressions и VFS artifacts с digest verification;
6. exact runtime config/provider policy/overlay/environment/binding refs,
   versions и digests;
7. exact provider binding и generated MCP config;
8. Secret descriptors без values и process projection только в
   `provider-runtime`;
9. provider process UID без Kubernetes token/authority material.

До запуска Codex app-server runner повторно проверяет закрытые model/reasoning
пары, наличие каждого image tool как executable и точное равенство MCP tool
catalog ожидаемому RuntimeRevision. Ошибки этого этапа возвращаются как
`RUNTIME_PROFILE_UNSUPPORTED` или `RUNTIME_MCP_UNAVAILABLE`; raw input и
credential values в диагностику не включаются.

Runner не использует shell orchestration. Provider/CLI запускаются прямым
`exec` с typed arguments, управляемой process group и явным environment.

## Завершение трёх контейнеров (#1073)

Обычные контейнеры `role-runtime`, `provider-runtime` и
`provider-credential-relay` могут получить SIGTERM в любом порядке. Relay
сохраняет listener и текущую callback boundary не более 60 секунд после
сигнала. В этом окне остаются обязательными peer UID, exact lease/fence/
generation, RuntimeRevision и прежние credential pins. Новые grants и attempts
не создаются; owner revoke по-прежнему запрещает commit.

Каждое чтение relay ограничено deadline; по окончании drain отменяются callback
и accepted connection, закрывается listener, watcher и handler завершаются
до закрытия callback client. Отсутствие ACK не означает успешный refresh и
не разрешает слепой повтор. Уже измеренный Usage остаётся в частичном broker
result и в неизменном FAILED completion.

Согласованные бюджеты: provider process shutdown12s, refresh commit40s,
provider response read grace57s, отдельный runner completion60s и cleanup20s.
Role Pod требует150s; controller/admission companion находится в #1025/PR1063.
Проверять только metadata и безопасный receipt, не печатать auth payload.

Локальная проверка: `go test -race -p 1 ./internal/credentialrelay` в модуле
runner использует настоящий Unix listener, bounded payload/commit/ack после
отмены и медленного peer. Общие runner callback/broker tests дополнительно
проверяют частичный Usage и отсутствие повторного provider execution.
Live/SIGTERM в Kubernetes требует отдельного разрешённого контура.

Context7: `/golang/go` (cancel/deadline/закрытие connections) и
`/websites/kubernetes_io` (termination grace и порядок sidecar).

После relay companion локально прошли полный runner race/vet/build и
`make test-agent-runner`: app19.448s, codex6.431s, остальные пакеты PASS.
Сохранены тесты partial Usage исходного `2a7b47d350e6800696ee5ca061bc1f0e708fdd2a`;
его прежний PASS не использован вместо нового запуска. Общий integrated
baseline/review и реальный Kubernetes shutdown остаются NOT RUN.

## `config.toml`: материализация

Runner каждый раз создаёт `config.toml` из одной typed структуры со следующим
приоритетом:

1. server-owned RuntimeRevision назначает model, approval/sandbox,
   permissions, credential store, MCP и shell environment boundary;
2. canonical published overlay заполняет только разрешённые
   `model_reasoning_effort`, `personality`, `allow_login_shell = false` и
   `history.persistence`;
3. environment set добавляет только allowlisted process environment names и не
   меняет TOML authority fields.

Overlay повторно проходит strict TOML parse непосредственно перед записью.
Unknown/protected key, credential marker, non-canonical digest или попытка
включить login shell закрывают startup. Secret values приходят только через
execution-scoped Pod environment после exact controller projection и не
записываются в `runtime.json`, `config.toml`, safe effective-config readback,
logs, callback или artifacts.

При resume `turn/start` явно получает опубликованные `effort` и `personality`,
если они заданы overlay: значения предыдущей attempt не заменяют новый выбор.

## MCP

Каждый required server проходит тот же рабочий initialize path. Недоступный MCP
возвращает safe typed failure/`Unavailable`; fallback на universal proxy,
непроверенный endpoint или direct secret запрещён. Human Gate завершает текущую
attempt в долговечное ожидание, а после решения запускается новая attempt.

MCP readiness проходит через локальный UDS proxy, `SO_PEERCRED`, exact bearer,
execution/MCP binding headers и тот же mTLS callback endpoint, который
обслуживает рабочие `tools/list` и `tools/call`. Отдельного облегчённого probe
endpoint нет.

## Workspace acceptance

`/readyz` запускает create/read/atomic replace/read/delete canary во вложенном
каталоге `.kodex/outbox`. Для successful `workspace-write` проверить наличие
artifact `workspace-write-result.json` и поля `runtime_revision_ref`,
`runtime_revision_version`, `runtime_revision_digest`, `attempt`,
`execution_binding_digest`. Временный `.workspace-write-result.next` после
операции отсутствует.

До provider turn очищается outbox предыдущей attempt. При лимите completion
файл provenance имеет приоритет; FIFO не должен блокировать чтение outbox.
Subprocess acceptance использует детерминированный процесс агента в bubblewrap,
а не live вызов модели. Полная карта тестов находится в README runner.

Отказы `READ_ONLY`, `QUOTA_EXCEEDED`, `PATH_OUTSIDE_WORKSPACE` и
`RUNTIME_IO_ERROR` безопасны и не содержат реальный host path. Symlink,
traversal, foreign workspace/attempt и credential path должны закрыто
отклоняться.

## Terminal handoff

Runner останавливает child processes, закрывает progress, формирует bounded
result/artifact manifest и подписывает handoff. Raw stdout/stderr, JSONL,
provider response, prompt, arbitrary tool payload и secret values не входят в
handoff, logs или events. Runtime-controller удаляет Pod только после
authoritative owner readback.

Completion всегда несёт exact `runtime_revision_digest` и `attempt` как для
успеха, так и для safe failure. Runtime-controller отклоняет несовпадение с
зарегистрированной lease и не делает retry от имени старой attempt.

## Наблюдаемость и deploy

Runner не имеет отдельного Service/ServiceMonitor: это процесс role Pod, а не
long-lived control-plane deployable. Проверять `runtime-controller` readiness,
terminal safe code и Pod termination reason. Alert
`RuntimeControllerNotReady` ведёт в runbook lifecycle owner, после чего для
диагностики role Pod используется этот документ. Сам runner не получает Kubernetes RBAC и использует ServiceAccount
`agent-runner` с `automountServiceAccountToken: false`.

Static `runtime-workloads` render должен содержать namespace default-deny и
exact warm egress только к DNS, runtime-controller и egress-gateway. Exact
обычный turn policy создаёт runtime-controller из immutable environment policy;
расширять static egress по одному порту или wildcard destination запрещено.

## Warm assistant

Warm mode ждёт server-owned FIFO turns через callback. Idle не является Turn.
Stale revision/ticket отклоняются; controller пересоздаёт Pod. System assistant
не получает прямой DB/Kubernetes/secret storage access и выполняет configuration
actions только через typed platform MCP commands с полномочиями пользователя.

## Локальная проверка

```bash
make test-agent-runner
```
