# Консоль и операционные интерфейсы

## Назначение

Домен описывает центральный чат с `agent-manager`, управление проектами, репозиториями и доступами, рабочие представления по задачам, операционные статусы и диагностику, UX для запуска flow и наблюдения за выполнением, а также экраны каталогов пакетов, контура серверов и кластеров, биллинга и настроек автоматизации.

## Что входит

- центральный чат с `agent-manager`;
- управление проектами, репозиториями и доступами;
- рабочие представления по задачам;
- операционные статусы и диагностика;
- UX для запуска flow и наблюдения за выполнением;
- экраны каталогов пакетов;
- экраны контура серверов и кластеров;
- экраны биллинга и настроек автоматизации.

## Gateway-поверхность

Первый backend-контур для консоли сотрудников — `staff-gateway`. Он отдаёт `web-console` OpenAPI для входящих решений владельца: список, карточку одного решения и отправку ответа `approve`, `reject`, `request_changes` или `answer`, если это разрешено текущим request. Gateway остаётся тонким: он принимает actor/request context, вызывает `interaction-hub` по gRPC и возвращает только safe refs, статусы, краткие summaries, timestamps и version. Собственная модель решений, прямой доступ к БД доменных сервисов, управление `Run`/session/governance decision/provider write и междоменная агрегация в этот контур не входят.

Список `owner inbox` поддерживает scope-фильтр, фильтры по kind/status/source owner/assignee/correlation, `include_diagnostics` и cursor pagination. Сортировку не выбирает клиент: её фиксирует `interaction-hub`, чтобы UI получал стабильный порядок с активными и срочными карточками выше. Карточка решения возвращает безопасные детали request, delivery/callback/response summaries, `allowed_actions`, timestamps и `version`; завершённые request не должны возвращать доступные действия. Ответ владельца отправляется с `expected_version` и `command_id` или `idempotency_key`, а gateway маппит ошибки `interaction-hub` в безопасные HTTP-статусы и коды без раскрытия raw payload.

Для операторского просмотра выполнения `staff-gateway` отдаёт `GET /v1/agent-runs/{run_id}/runtime-status`. Endpoint вызывает `agent-manager.GetAgentRunRuntimeStatus` и возвращает только безопасную сводку: `run_id`, `run_status`, `runtime_job_ref`, `runtime_job_status`, safe error code/summary, timestamps, `run_version`, `human_gate_waiting` и safe refs ожидания. Prompt body, secret values, kubeconfig, workspace paths, raw provider payload и большие логи через gateway не проходят; runtime job lifecycle остаётся у `runtime-manager`, а orchestration state — у `agent-manager`.

Для экрана истории действий `staff-gateway` отдаёт `GET /v1/agent-runs/{run_id}/activities`. Endpoint вызывает `agent-manager.ListAgentActivities` с typed фильтрами `activity_kind`, `status` и cursor pagination; фильтра по временному диапазону нет, потому что текущий gRPC-контракт его не содержит. Ответ содержит safe activity entries: refs/status/timestamps/tool metadata, safe summary, payload digest, bounded error, version и correlation id. Raw tool input/output, stdout/stderr, prompt body, transcript, provider payload, workspace paths, secret values и большие логи через gateway не проходят.

## Карта Issue

- Доменная карта: `docs/delivery/issue-map/domains/console-and-operations-ux.md`.
