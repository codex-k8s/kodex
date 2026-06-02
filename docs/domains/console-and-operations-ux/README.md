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

Для операторской сводки governance `staff-gateway` отдаёт `GET /v1/governance/summary`. Endpoint принимает ровно один safe selector: target, project/repository context, release candidate, release decision package id или integration ref; затем вызывает `governance-manager.GetGovernanceSummary` и возвращает уже подготовленную доменную модель чтения. Gateway не вычисляет risk/gate/release правила, не читает БД соседних сервисов и не обогащает provider/runtime/agent данные. Ответ содержит pending/completed decisions, risk class, gate/release outcomes, linked provider/agent/runtime evidence refs, bounded diagnostics, timestamps и versions; raw prompt, transcript, tool input/output, provider payload, webhook body, stdout/stderr, workspace paths, kubeconfig, secret values и большие детали через gateway не проходят.

## Web-console MVP

Первый активный `web-console` размещён в `services/staff/web-console` и использует Vue, Vite, TypeScript и Vuetify. Приложение получает типизированный API-клиент из `specs/openapi/staff-gateway.v1.yaml`, вызывает только `staff-gateway` и не обращается напрямую к БД, Kubernetes, внутренним gRPC-сервисам или сервисам-владельцам. Production-сборка не формирует доверенные `X-Kodex-Actor-*`: проверенный actor context добавляет trusted edge или backend-session слой перед `staff-gateway`. Ручные actor headers доступны только в явном local-dev режиме Vite.

Первый набор экранов:

- командный центр: каркас, карточки только по текущей странице входящих и последнему ручному поиску одного `Run`, отключённый диалоговый ввод и быстрые действия до появления соответствующих HTTP-контрактов;
- входящие и решения: список, карточка, безопасные детали и действия ответа через owner inbox endpoints;
- исполнения и среда: runtime summary и activity timeline одного `AgentRun` по введённому `run_id`;
- governance: операторская сводка доступна через `staff-gateway`, экран в `web-console` остаётся следующим frontend-срезом.

Текущая интерфейсная итерация доводит эти экраны до демонстрируемого состояния: shell адаптируется под узкую ширину, командный центр показывает отдельно работающие зоны и зоны, ожидающие подключение frontend или ещё не появившиеся `staff-gateway` endpoints, owner inbox использует master-detail паттерн с безопасными ошибками и ответом только через `allowed_actions`, а экран исполнений явно работает как поиск одного `Run` по safe id.

Агрегированная витрина командного центра, список `Run`, создание `Issue`, запуск flow, чат с `agent-manager`, проектные списки и экран governance summary не подменяются демо-данными. Пока frontend не подключил соответствующий endpoint или в `staff-gateway` нет нужной HTTP-ручки, интерфейс показывает честные пустые или отключённые состояния.

## Карта Issue

- Доменная карта: `docs/delivery/issue-map/domains/console-and-operations-ux.md`.
