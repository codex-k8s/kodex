---
id: OPS-OPENCODE-1982
title: Опциональный OpenCode runtime
type: operation
status: draft
owner: developer
version: 0.1.1
updated: 2026-11-07
---

# Опциональный OpenCode runtime

## Совместимость MCP-каталога

`runtime-controller` публикует `required: []` для object schemas без обязательных
полей. Значение `null` недопустимо: строгий MCP-клиент OpenCode отвергает весь
каталог, хотя командная проверка `opencode mcp list` может завершиться с кодом0.
Готовность проверяется по статусу подключения и реальному вызову разрешённого
инструмента с результатом, а не только по успешному текстовому ответу модели.

Синтетическая регрессия вызывает настоящий JSON-RPC `tools/list` для agent,
SYSTEM и PROJECT профилей, проверяет вложенные input/output schemas и компилирует
их. Пустой optional input остаётся допустимым; обязательные поля, ограничения
длины и запрет неизвестных аргументов проверяются отдельно. Изменение формы
каталога сохраняет прежнюю проверку прав при вызове инструментов.

Для адресного обновления этого unit существующий опциональный
`--component-image-profile` принимает `component: runtime-controller`. Профиль
закрепляет полный прежний release lock, обе source SHA и прежний/новый immutable
image в том же registry. Меняется только выбранный контейнер с provenance
annotations; sidecars, service account, volumes и replica count сохраняются.
Обычный render без профиля сохраняет прежние images. Допуск workload resume
с прежними lifecycle/idle и authority-проверками остаётся обязательным; профиль
сам по себе не разрешает перезапуск активного выполнения.

Unit #1982 добавляет engine к существующей job `agent-runner`, сохраняя
обычный Codex при отсутствии опции. Это полный путь конфигурации, account,
immutable runtime input, broker, управляемых tools, callback и deployment;
отдельный автономный orchestration graph не создаётся. Пока статус документа
draft, перечисленные здесь незавершённые пути не считаются принятыми.

## Поддержанный профиль

Первая версия поддерживает `engine=opencode`, OpenCode1.19.5 и
`tool_mode=managed_mcp`. Chat Completions идёт через явно заданный HTTPS
gateway. Собственные shell/file-write/task/web/skill/plugin tools не доступны.
Remote MCP `kodex` обслуживается существующим scoped bridge и gateway.
Выбор native tools или Codex overlay/effort/session несовместим с этим профилем
и должен закрыто отклоняться до запуска. Это ограничение профиля, не обход
допуска workspace или внешних действий. Codex сохраняет собственный профиль.

Провайдер использует отдельный ephemeral HOME/XDG внутри защищённой границы.
Project config не читается; native state удаляется после остановки процесса.
Continuation использует owner-pinned `SessionContext` Kodex. OpenCode session
ID не записывается как Codex UUID, JSONL rollout не имитируется. Native resume
OpenCode и перенос его внутренних SQLite-файлов не заявляются поддержанными.
Сохранность канонической истории подтверждается отдельным turn/restart тестом.

## Данные и ownership

`ModelProviderConfiguration` —закрытый несекретный контракт Git-managed
deployment: source revision, engine/version/tool mode, provider ID, HTTPS
base URL, exact /32 или /128 address pins, allowlist моделей и ограничений
запроса. Пределы владельца не выдаются за доказанные возможности модели.
Дубликаты, неизвестные поля, credentials, wildcard/loopback/metadata адреса,
неоднозначные URL и пустые пределы отклоняются.

Новая опция не разрешает UI молча перезаписывать Git-managed endpoint.
Account выбирается прежней provider policy и сохраняет session affinity.
Gateway credential имеет отдельный `auth_mode=gateway-api-key` и привязку
к exact provider definition. Legacy Codex decoder его не принимает: старый
компонент не должен переслать gateway key внешнему провайдеру при смешанном
rollout. Секретный codec используется только backend materialization и broker;
его payload не является runtime configuration и не включается в RunnerInput.

Provider получает ключ из защищённой credential projection, проверенной по
account/revision/UID/resourceVersion/content digest, и передаёт его только
процессу OpenCode через отдельную переменную. Generated configuration содержит
environment references. Remote MCP не создаёт credential-bearing shell child.
Другие role secrets/обычное окружение не наследуются процессом провайдера.

## Сквозная карта

| Шаг | Владелец и граница |
|---|---|
| Deployment config | Renderer проверяет typed descriptor и создаёт immutable configuration revision; default Codex остаётся прежним без опции |
| Registration/default | CP bootstrap регистрирует Git-owned definition/profiles; выбор default явный, существующие UI-owned agent configurations не затираются |
| API-key account | Existing owner command/reservation → secret-broker → canonical gateway credential, без ChatGPT login |
| Model catalog | Owner task несёт exact definition key; секретный observer выбирает только соответствующий configured HTTPS endpoint, без redirect или fallback |
| Launch/claim | CP проверяет profile/model/account/environment, фиксирует descriptor в свежей RuntimeRevision и digest |
| Materialization | RPC → runtime-controller → canonical RunnerInput; unknown/invalid descriptor и несовместимый image/profile закрыто отклоняются |
| Transport | Controller подписывает малую gateway route projection; proxy требует exact host/path/address pins, TLS и server-owned grant |
| Execution | UID-isolated broker проверяет input/context/auth pins, запускает pinned OpenCode с managed-only configuration |
| Tool call | OpenCode → local bridge → existing MCP gateway → fresh lease/capability/integration grant/Human Gate → effect receipt/result/audit |
| Result | Completed messages, partial usage и terminal verdict идут прежним callback; provider state не становится authority |

## Lifecycle

| Переход | Результат и источник истины |
|---|---|
| Create/claim | Только прежняя owner transaction создаёт Run/lease/attempt; конфигурация входит в immutable digest |
| Streaming/tool | Published items bounded; reasoning/raw errors не публикуются. MCP gateway владеет effect audit, adapter не создаёт второй native audit |
| Model error/auth reject | Один существующий failed callback; явная ошибка учётной записи не переключает provider/account/model |
| Timeout/cancel | Завершается process group и ожидается writer stop; gateway effect может остаться UNKNOWN, повтор не выдаётся за безопасный |
| Completion | Только реальный terminal stop, process join и workspace проверки; no fabricated archive и no inferred external success |
| Retry/delegation | Свежая RuntimeRevision через прежний owner graph; root initiator и immutable policy не берутся из prompt |
| Continuation/restart | История от CP, новые native process/state; прежние side effects не воспроизводятся startup-ом |
| Configuration update | Новый descriptor/revision; старый активный snapshot не переписывается. Совместимость proxy/profile/image проверяется до apply |
| Disable | Известно неподдержанный engine/profile блокируется; автоматического fallback нет |

OpenCode1.19.5 нормализует отсутствующие usage values в0; поэтому численные
наблюдения adapter отмечает PARTIAL, отсутствие наблюдения —UNKNOWN. Эти
показатели не являются billing, балансом или разрешением по квоте.

## Проверки и поставка

Обязательны closed descriptor/secret codec, wrong engine/account/model,
MCP-only permissions, egress/SSRF/redirect/address pins, bounded JSONL,
terminal failure/truncation, model auth errors, cancellation, tool/result/final,
continuation/restart, отсутствие утечки и Codex regression. Живой CLI smoke не
заменяет установленный role→gateway путь. Workspace V2 остаётся под #1953/#1964.

Все компоненты публикуются по exact source/image/schema pins. OpenCode образ
не объявляется штатным без qualification #1977; local build и green Pod не
являются приёмкой. Configuration/SQL changes не требуют новых бизнесовых
состояний Run: graph/events/lifecycle остаются у прежних владельцев.

Проверены Context7 `/anomalyco/opencode/v1.18.29` и официальный source
OpenCode tagv1.19.5, commit810c0eba1a7348036f8bdf61e6632d0c36c6876c.
В executable этой версии bundled `@ai-sdk/openai-compatible`2.0.41.
Квалификация выполняется именно на1.19.5; соседняя версия Context7 не является
доказательством совместимости. Источники:
[providers](https://opencode.ai/docs/providers/),
[permissions](https://opencode.ai/docs/permissions/),
[pinned CLI source](https://github.com/anomalyco/opencode/blob/v1.19.5/packages/opencode/src/cli/cmd/run.ts).

## Включение

Обычный renderer без новых аргументов сохраняет Codex. Для OpenCode передать
`--model-provider-configuration /operator/provider.json`; только явный
`--model-provider-default-model` выбирает его для новых сотрудников и
помощников. Existing UI selections не перезаписываются. Provider definition
доступен в существующем каталоге, поддерживает только API key. Конфигурация
имеет `schema=kodex.model-provider/v1`, `managed_by=git`, exact source SHA,
`engine=opencode`, `engine_version=1.19.5`, `tool_mode=managed_mcp`,
`provider_id`, HTTPS `base_url`, exact `/32` или `/128` `address_cidrs` и
allowlist `models` с `id`, `context_window_tokens`, `maximum_output_tokens`.
Ceilings задаёт оператор; это не утверждение о полном контексте модели.

Descriptor должен создаваться из закреплённого deployment source, а не
содержать SHA собственного файла. Renderer создаёт immutable ConfigMap,
проецирует его CP/SB и включает тот же descriptor в egress policy. Gateway
использует operator-approved literal addresses без ambient DNS. До apply
оператор сверяет DNS и доступность адресов; смена адресов требует нового
Git descriptor. HTTPS hostname и CA проверяются всегда. Endpoint нельзя
поменять под существующим provider ID/credential: нужен новый ID и account.

`python3 tools/install/model-provider.py prepare-auth --configuration
/operator/provider.json --api-key-file /operator/gateway-key --output
/operator/auth.json` создаёт secret-only канонический файл. Каталог0700,
вход/выход0600; существующий выход не перезаписывается. Ключ передаётся
существующему `materialize-secrets.sh --provider-auth-file` и сохраняется в
поддержанном Kubernetes backend, без фиктивной ChatGPT авторизации.
Повторный bootstrap сохраняет authoritative current credential.
Производный config OpenCode содержит только environment references.

Optional `agent-runner-opencode` собирается через закрытый target
`opencode-runtime` и включает прежний Codex. `agent-runner` по умолчанию не
содержит новый binary. В первой версии optional installation использует
standard RoleImage; documents environment закрыт до отдельной квалификации
этого варианта. Сетевой CA bundle/прокси предоставляет существующий защищённый
runtime; отдельный сетевой обход для модели не создаётся.

## Текущие доказательства

Локально прошли descriptor/auth/input contracts, runner и prompt tests,
secret-broker gateway catalog failures, CP targeted tests, native disposable
PostgreSQL bootstrap/restart/endpoint-conflict и полный synthetic render.
Минимальный `opencode-binary` image собран с проверкой archive SHA и CLI
версии. Это не полный qualified RoleImage. Native model/tool/continuation,
отмена и ошибочный ключ в установленном runtime требуют отдельного acceptance
receipt с source/image/config pins. Независимое review не выполнялось по
прямому ограничению владельца на субагентов.

Обнаруженный существующий встроенный OpenAI capability snapshot истёк
02.11.2026; production legacy catalog закрыто отклоняет источник. Parser tests теперь
используют фиксированное время внутри validity window; отдельный negative
тест проверяет expiry до отправки credential.
Срок не продлевается без повторной проверки provenance. Это отдельное
ограничение прежнего каталога; gateway не использует этот capability source.
