---
id: OPS-EMAIL-1378
title: Один Run для RoleImage, workspace и почтового эффекта
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-09
---

# Область

Issues #1378/#1371/#1031/#1223. Combined profile расширяет существующий
`email-agent-acceptance.mjs`; обычные email и RoleImage profiles сохраняются.
Приложения, API, Proto, SQL, grants/security и runner images не меняются.
Повторная сборка образа для этой оснастки не нужна; staging должен уже
обслуживать runtime-controller с безопасным MCP result из #1371.

Combined не переносит ACK между журналами и не создаёт второй Run. Он читает
неизменный завершённый CFG journal через `existingFixture`, повторно выполняет
`prepareRuntimePlan`, закрепляет email pins, nonce и hash общего задания.
Модель сначала выполняет ровно прежний `workspaceAcceptanceTask` через
настоящий shell, затем один `invoke_integration`. При отказе первой части
задание требует остановки до почты. Эти инструкции не заменяют Gate/grant;
оснастка обнаруживает повторные вызовы и частичное выполнение.

# Входы и планирование

К обычному private email profile из OPS-EMAIL-1371 добавляется:

```json
{
  "combined": {
    "version": 1,
    "fixtureState": "/owner/private/completed-cfg.jsonl",
    "fixtureSHA256": "<точный SHA256 bytes неизменного CFG журнала>",
    "runnerProvenance": "/owner/private/runner-provenance.json",
    "runnerProvenanceSHA256": "<точный SHA256 bytes provenance>"
  }
}
```

Обязателен существующий `agentRef`, совпадающий с CFG project/Agent. Новый
Agent не создаётся. Runtime configuration/account/model/default effort и один
email grant назначаются штатным UI/API до plan. RoleImage/environment binding
должны совпадать с завершённой prepare/advance/restore fixture. Mailbox/секреты/
publication/HEALTH не повторяются. Grant изменяет capability digest: старый
план остаётся историей, новый plan готовится до первого Run intent.

`runner-provenance.json` выдаёт root из проверенного build/exact base image:

```json
{
  "version": 1,
  "kind": "RUNNER_BINARY_PROVENANCE",
  "sourceRevision": "<40 hex exact build source>",
  "baseImage": "registry.example/runner@sha256:<digest из CFG HEADER runnerDigest>",
  "binaryPath": "/usr/local/bin/kodex-agent-runner",
  "binarySHA256": "<SHA256 runner binary этого exact base>"
}
```

Для сохранённого local OCI archive поддержан read-only verifier из
[OPS-RUNNER-1382](runner-binary-provenance-1382.md); он сохраняет source/input,
manifest/config/layers и hash бинаря без rebuild/Pod access.

Это ожидаемый hash, полученный до Run из trusted base provenance, не результат
измерения исполняемого Pod. Самосогласование actual Pod как expected запрещено.
Если build provenance недоступен, preparation блокируется. CLI не извлекает
ожидаемый hash из Pod и не принимает arbitrary executable path. Profile,
provenance и CFG bytes закреплены; смена файлов закрыто останавливает запуск.

Нужен актуальный CAPTURED component manifest: clusterUID (kube-system UID),
kodex-system namespaceUID и compatibility. Owner session — обычный свежий
limited storageState без выдачи QA SSH/exec. Inputs 0600, directories 0700.
Код запускается из чистого exact checkout; HEADER включает source SHA.

# Lifecycle и authority

| Шаг | Команда / authority | Idempotency, state, readback |
| --- | --- | --- |
| plan | Cookie+CSRF owner GET, существующие CP readers: RoleImage admission/promotion, runtime/effective grants/account/catalog, email mailbox/grant | Ни Run, ни provider effect; общий nonce, task hash, exact image/binding/credential-free provenance |
| launch | Единственный POST `/api/v1/runs` через прежний canonical session client/CP owner CreateRun | fsync INTENT до POST; один owner key. ACK повторно читается без POST; unresolved/UNKNOWN закрывает повтор |
| runtime-binding | GET того же Run и runtime-revision-diff; IMAGE равен exact promoted artifact | Пока QUEUED — PENDING без output; BOUND сохраняется отдельно атомарно, immutable 0600 файл для root observer |
| observe | Root на назначенном k3s node, только kubectl GET / CRI inspect / /proc metadata+exe | Никаких apply/exec в Pod/Secret reads. Exact cluster/staging/Pod/CRI/process до и после; output exclusive, без overwrite |
| capture | Прежний MCP/owner event capture плюс общий runtime binding и `verifyWorkspaceAcceptance` | Один invocation, exact input hash, native CODEX_SHELL, три CLEAN artifacts с nonce/attempt/revision/exec-binding digest; Pod observation совпадает |
| receipt | Прежний owner GET email-effect-receipt | Combined capture обязателен; только EFFECT_CONFIRMED+SUCCEEDED Run/invocation даёт PASS. SMTP accepted не означает delivered |
| Gate/cancel/retry/restart | Существующие owner transitions, без новых полномочий helper | WAITING_HUMAN/PENDING не PASS; второй attempt, изменённый result, multiple invocation, UNKNOWN и частичный workspace не закрываются новым Run |

Новых событий нет. Источник runtime/email результата — существующие CP
RunEvent/ToolCall/audit/receipt; SRE proof является отдельным измерением
исполняемого процесса, а не источником application authority.

# Порядок одного запуска

После отдельного owner GO на один provider Run и один конкретный mail effect:

```bash
EMAIL_QA_ARGS=(--origin https://control.kodex.works
  --storage-state "$EMAIL_QA_SESSION" --profile "$EMAIL_QA_PROFILE"
  --serving-manifest "$EMAIL_QA_MANIFEST" --state "$EMAIL_QA_JOURNAL"
  --timeout-ms 60000)
node tools/dev/email-agent-acceptance.mjs plan "${EMAIL_QA_ARGS[@]}"
```

Root заранее запускает observer на k3s node. `--binding` пока отсутствует;
observer ждёт его bounded время. Родительские каталоги binding/output — 0700.
Оператор передаёт только safe binding metadata с API машины на node через
принятый private transport; credentials/storageState на node не нужны.

```bash
sudo -n node tools/release/runtime-pod-observe.mjs \
  --context "$KODEX_RELEASE_CONTEXT" --binding "$REMOTE_RUNTIME_BINDING" \
  --output "$REMOTE_RUNTIME_OBSERVATION" --timeout-ms 1200000
```

Команда выше предназначена отдельному контролируемому handle; её ожидание
не должно блокировать запуск и связь с владельцем. Затем API оператор:

```bash
node tools/dev/email-agent-acceptance.mjs launch "${EMAIL_QA_ARGS[@]}" \
  --confirm START-ONE-STAGING-EMAIL-RUN
node tools/dev/email-agent-acceptance.mjs runtime-binding "${EMAIL_QA_ARGS[@]}" \
  --runtime-binding-output "$LOCAL_RUNTIME_BINDING"
```

Если runtime-binding вернул PENDING, допустимо повторить только этот GET шаг.
BOUND file создаётся через fsync+exclusive atomic link; существующий не
перезаписывается. Root передаёт его теми же bytes в absent remote binding path.
Нельзя скопировать в него старый plan или другой Run. Root после observer PASS
получает output обратно приватно, сохраняя bytes SHA:

```bash
node tools/dev/email-agent-acceptance.mjs capture "${EMAIL_QA_ARGS[@]}" \
  --runtime-observation "$LOCAL_RUNTIME_OBSERVATION"
node tools/dev/email-agent-acceptance.mjs receipt "${EMAIL_QA_ARGS[@]}"
```

Capture можно повторять для того же Run без POST. Gate/PENDING — не PASS.
`WORKSPACE_SCAN_PENDING` требует только последующего чтения того же artifact,
а не нового Run/probe. Timeout команды не доказывает отсутствие provider effect.
Нет автоматического login, продления absolute session policy, continuation,
retry, send/reconcile/cleanup или повторного HEALTH.

# Что именно измеряет observer

Runtime-controller создаёт managed **Pod**, не Job. Observer фиксирует один
Pod с mode=turn, staging label и exact revision/project/session/turn hashes и
attempt. Проверяет lease и execution-binding digest, UID, spec digest, node,
containerIDs/start timestamps/restartCount=0 и четыре digest-pinned образа:
workspace-prepare/workspace-init/role-runtime/provider-runtime. Init containers
должны завершиться с exit0, runtime containers — быть ready/running.

Readback требует фактический imageID approved manifest digest. OCI index с
другим platform child пока закрыто отклоняется; без отдельного index→child
provenance не объявлять такой вариант PASS. Смена Pod/image/restart не скрывается
согласованием образов между контейнерами.

Для role-runtime CRI должен подтвердить Pod UID/name/namespace/containerID,
imageRef, attempt и точную ENTRYPOINT цепочку
`kodex-init entrypoint /usr/local/bin/kodex-agent-runner runtime-session`.
Observer находит единственный runner в том же PID namespace, проверяет fixed
argv и читает `/proc/PID/exe` через уже проверяемый `readHostProcess`. Повторно
сверяет startTicks/inode/device/hash/namespace, CRI и Pod identity. CRI env и
spec/env values не печатаются и не сохраняются, provider input/секреты не читаются.
Observer должен выполняться на node этого Pod: чужой CRI container ID отсутствует
и закрыто отклоняется. Без успевшего readback короткоживущего Pod — NOT RUN/FAIL,
не самоподтверждение hash файла из образа.

# Проверки и ограничения

`make test-email-combined-acceptance` — локальные standalone+combined public
CLI, настоящий workspace verifier/probe fixture и observer kubectl→CRI→proc
fixture. Fixture transport и synthetic executable bytes не считаются live.
Проверяются foreign scope/attempt/image, provenance drift, wrong binary,
process/Pod replacement, missing/partial workspace, Gate/PENDING, multiple
invocations, malformed/старый MCP result, lost ACK/UNKNOWN и atomic output.

Live provider/mail/node pull/running binary/Gate и доставка остаются NOT RUN до
отдельного owner GO и evidence. Одного positive Run недостаточно для quota
negative, полного IMAP/POP operation matrix, trust rotation или browser UI.
Cleanup synthetic письма — отдельная разрешённая операция позднее; никакого
автоматического удаления чужих сообщений или повторного SMTP при UNKNOWN.

При реализации через Context7 проверены Kubernetes Pod ContainerStatus/imageID
и node-local crictl inspection: [официальная документация](https://kubernetes.io/docs/tasks/debug/debug-application/_print/).
Exact runtime ENTRYPOINT/CRI профиль сверены с текущими agent-runner Dockerfile
и runtime-controller workload manager; произвольные container runtime layouts
не объявляются поддержанными без проверки.

После смены runner base (#1409) `combined.fixtureState` указывает на новый
терминальный журнал `role-image-forward-upgrade.mjs` из OPS-DOC-1262. Reader
проверяет linked predecessor, forward pins и полный rebind; provenance SHA
должна совпасть с новым immutable HEADER. Перенос completed checkpoint или
переписывание старого CFG HEADER запрещены. Прежние standalone CFG journals
продолжают читаться без изменения формата.
