---
doc_id: PLN-KODEX-MVP-DELIVERY-0001
type: delivery-plan
title: "kodex — план поставки MVP"
status: active
owner_role: EM
created_at: 2026-05-27
updated_at: 2026-05-27
related_issues: [78, 281, 282, 294, 380, 582, 586, 698, 895, 909]
related_prs: []
related_docsets:
  - docs/platform/**
  - docs/domains/**
  - docs/delivery/coordination/**
  - refactoring/images/wave5/**
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-27-mvp-delivery-roadmap"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-27
---

# План поставки MVP

## TL;DR

- GitHub Issues в корневом backlog являются якорями и напоминаниями, но не заменяют полный план MVP.
- Ближайшая цель MVP: получить сквозной рабочий контур, который можно проверить в Kubernetes через smoke-проверки, логи pod и затем через операторский интерфейс.
- Первый серверный deploy начинается до реализации полноценного фронтенда, как только готов минимальный backend-набор сервисов, миграции, манифесты и smoke-команды.
- `staff-gateway` делается перед первым полноценным web-console, потому что он фиксирует OpenAPI-границу для операторских экранов.
- Фронтенд строится по утверждённым черновым макетам `refactoring/images/wave5/**`, без попытки считать их pixel-perfect спецификацией.
- После удаления `dupl-go` baseline каждый новый Go-срез должен проходить `make dupl-go` без оговорок про общий baseline.

## Назначение

Этот документ задаёт маршрут до MVP и порядок параллельной работы доменных агентов. Он не заменяет доменные документы в `docs/domains/**` и не переписывает архитектурную канонику в `docs/platform/**`.

Документ нужен, чтобы:

- планировать не только по открытым GitHub Issues, но и по фактической цепочке поставки;
- понимать, когда начинать deploy на серверный Kubernetes-контур;
- не откладывать визуальную проверку интерфейса до полного завершения всех доменов;
- держать понятный порядок появления `staff-gateway`, web-console и операционных проверок;
- не терять связь между backend-срезами и утверждёнными макетами `wave5`.

## Цель MVP

MVP считается достаточным, когда владелец может через платформу выполнить первый управляемый сценарий:

1. Создать или подключить проект и репозиторий.
2. Подготовить bootstrap/adoption PR с `services.yaml`, локальными инструкциями и минимальной проектной политикой.
3. Запустить агентный `Run` по задаче или follow-up.
4. Получить историю активности, сводку вызовов инструментов, результат acceptance и provider-native артефакт.
5. Получить Human gate, approval или запрос обратной связи, ответить через операторскую поверхность.
6. Провести риск-проверку или release decision package на безопасных ссылках и summaries.
7. Увидеть runtime jobs, slots, pod readiness, smoke-статус и короткие диагностические хвосты без чтения сырого секрета или raw payload.
8. Проверить всё это сначала через smoke/логи Kubernetes, затем через `staff-gateway` и web-console.

## Не входит в MVP

- Полный SaaS-биллинг, платёжные интеграции и коммерческий кабинет.
- Полноценный магазин платных пакетов как внешний продукт.
- Все внешние каналы связи; достаточно контрактов и одного минимального рабочего пути.
- Полное покрытие GitLab на уровне GitHub, если оно не нужно для первого dogfooding-контура.
- Полная автономная эксплуатация без ручного участия владельца.
- Векторные хранилища, долгосрочная память агентов и knowledge storage, кроме явно утверждённого исследовательского среза.

## Управляющие принципы

- Поставлять маленькими PR, но проверять прогресс по сквозному сценарию.
- Не делать фронтенд без `staff-gateway`, если экран требует живых данных и действий.
- Не ждать фронтенд для первого deploy: backend-сервисы должны раньше пройти Kubernetes-проверку.
- Не смешивать gateway-ответственность: `staff-gateway`, `integration-gateway` и будущий `user-gateway` остаются разными сервисами.
- Не считать открытые Issues полным планом: крупные Issues могут оставаться эпиками, а delivery-срезы создаются по мере уточнения.
- Не возвращать `dupl-go` baseline; общие механические повторы выносить в `libs/go/**`, доменную логику оставлять в сервисах-владельцах.

## Этапы поставки

### Этап 0. Стабилизация качества перед продолжением

Статус: завершён.

Результат:

- локальные дубли в `agent-manager`, `interaction-hub`, `governance-manager` сокращены;
- межсервисные механические повторы вынесены в `libs/go/**`;
- `tools/lint/dupl-baseline.txt` удалён;
- `make dupl-go` должен оставаться чистым в новых PR.

### Этап 1. Сквозной backend-срез

Цель: закрыть минимальную backend-цепочку без UI.

Состав:

- `project-catalog`: завершить связку bootstrap/adoption policy import, worker/consumer или явный service-command путь от safe provider signals к проверенной проекции.
- `provider-hub`: базовое gRPC-чтение safe scan snapshots и provider merge signals для project/adoption контура есть; producer-side smoke проверяет GitHub bootstrap/adoption webhook fixtures -> safe merge signal -> outbox path; GitLab parity делать только при отдельном решении владельца.
- `agent-manager`: завершить Human gate refs, ожидания владельца и переходы follow-up/acceptance без прямого GitHub/GitLab write.
- `interaction-hub`: связать owner inbox и request lifecycle с агентным сценарием.
- `governance-manager`: безопасно принимать и обогащать review/risk/release refs по одному owner-домену за срез.
- `runtime-manager` и `fleet-manager`: использовать уже готовые jobs, slots, placement и health как основу для серверной проверки.
- `platform-mcp-server` и `codex-hook-ingress`: оставить как инструментальную поверхность и вход hook events, не переносить туда бизнес-состояние.

Критерий завершения:

- smoke или CLI-команды могут пройти основной backend-сценарий без web-console;
- сервисы пишут безопасные события и диагностические summaries;
- ошибки видны через логи, readiness и smoke-проверки, а не только через unit-тесты.

### Этап 2. Первый серверный deploy backend-контура

Цель: начать проверять платформу в Kubernetes до фронтенда.

Входные условия:

- у сервисов MVP есть Dockerfile, Kubernetes manifests, migration jobs, env inventory, health/readiness, smoke/runbook;
- есть нормализованный `services.yaml` с версиями внутренних и внешних образов;
- в Kubernetes доступен Kaniko или согласованный совместимый builder для сборки образов без Docker daemon;
- в Kubernetes доступен реестр образов для внешних зависимостей, зеркалированных образов и образов, собранных платформой;
- секреты берутся через согласованный контур, без раскрытия в Issues, PR, логах и документации;
- `make test-go`, `make lint-go`, `make dupl-go`, миграционные проверки и рендер манифестов проходят в применимой части.

Порядок:

1. Проверить базовые зависимости кластера: PostgreSQL, platform event log, Keycloak или выбранный IdP-контур, ingress, Kaniko и реестр образов.
2. Выполнить план backend deploy только на чтение: проверить deploy inventory, render/kustomize manifests, image refs, service dependencies и live foundation refs без `kubectl apply`, jobs и push образов.
3. Проверить, что внешний образ можно зеркалировать в реестр, а внутренний образ можно собрать через Kaniko и сохранить в тот же реестр.
4. Развернуть сервисы с готовыми манифестами и миграциями после отдельного разрешения владельца.
5. Проверить readiness, gRPC-связность, event-log/outbox, migration jobs и smoke-команды.
6. Зафиксировать ошибки в логах pod и закрывать их малыми PR.
7. Не подключать web-console как обязательный критерий этого этапа.

Критерий завершения:

- backend MVP-контур развёрнут на серверном Kubernetes-контуре;
- Kaniko или совместимый builder собирает внутренний тестовый образ без Docker daemon;
- реестр образов принимает внешнюю зависимость и внутренний образ, собранный платформой;
- агенты могут использовать логи pod и smoke-статус при проверке PR;
- владелец может видеть фактическое состояние deploy через runbook/smoke, даже без фронтенда.

### Этап 3. `staff-gateway`

Цель: зафиксировать OpenAPI-границу для операторского интерфейса.

Состав:

- ручки чтения и записи для командного центра, проектов, репозиториев, задач, PR/MR, входящих решений, executions/jobs/slots и базовых настроек;
- агрегация данных через gRPC вызовы сервисов-владельцев;
- отсутствие собственной бизнес-истины в gateway;
- безопасные DTO для web-console: без raw provider payload, секретов, transcript, stdout/stderr и kubeconfig;
- OpenAPI-контракт, генерация Go server/client и генерация TypeScript client для фронтенда.

Когда делать:

- после того как понятен минимальный набор живых экранов по сквозному backend-срезу;
- до первого полноценного фронтенд-среза с живыми данными.

### Этап 4. Web-console MVP

Цель: дать владельцу визуальную проверку основного сценария.

Источник UX:

- утверждённые черновые макеты: `refactoring/images/wave5/index.md`;
- общий style guide: `refactoring/images/wave5/ui-style-guide.md`;
- макеты являются составом экранов, блоков и действий, но не pixel-perfect контрактом.

Первый набор экранов:

- командный центр: `refactoring/images/wave5/01-command-center/screen.md`;
- рабочее пространство `Issue`: `refactoring/images/wave5/02-issue-workspace/screen.md`;
- рабочее пространство `PR/MR`: `refactoring/images/wave5/05-pr-workspace/screen.md`;
- входящие и approvals: `refactoring/images/wave5/06-inbox-and-approvals/screen.md`;
- executions/jobs/slots: `refactoring/images/wave5/07-executions-jobs-slots/screen.md`;
- проекты и репозитории: `refactoring/images/wave5/08-projects-and-repositories/screen.md`;
- внешние аккаунты и интеграции: `refactoring/images/wave5/09-integrations-and-accounts/screen.md`;
- пользователи и доступы: `refactoring/images/wave5/10-users-and-access/screen.md`;
- onboarding и empty states: `refactoring/images/wave5/11-onboarding-and-empty-states/screen.md`.

Второй набор экранов:

- редактор flow: `refactoring/images/wave5/03-flow-editor/screen.md`;
- каталог ролей: `refactoring/images/wave5/04-role-catalog/screen.md`;
- каталог пакетов: `refactoring/images/wave5/12-package-catalog/screen.md`;
- организации и группы: `refactoring/images/wave5/13-organizations-and-groups/screen.md`;
- серверы и кластеры: `refactoring/images/wave5/14-fleet-servers-clusters/screen.md`;
- биллинг и затраты: `refactoring/images/wave5/15-billing-and-costs/screen.md`;
- релизы и автоматизация: `refactoring/images/wave5/16-release-policy-automation/screen.md`.

Критерий завершения:

- владелец может пройти основной сценарий через UI;
- экранные действия идут через `staff-gateway`, а не напрямую во внутренние сервисы;
- где backend ещё не готов, используются явно помеченные заглушки, а не скрытые фейковые данные.

### Этап 5. Интегрированная проверка и dogfooding

Цель: перейти от “сервисы запускаются” к “платформа помогает разрабатывать себя”.

Проверки:

- bootstrap/adoption сценарий на реальном репозитории;
- запуск агентной задачи с ролью, руководящими пакетами и workspace context;
- создание provider-native Issue/PR/comment/review signal;
- Human gate или approval через owner inbox;
- governance/risk/release package на безопасных refs;
- runtime job/slot visibility и logs;
- визуальная проверка через web-console.

Критерий завершения:

- ошибки можно диагностировать через UI, логи pod и smoke-скрипты;
- владелец может оставлять feedback по интерфейсу, а агенты — чинить по наблюдаемому состоянию;
- новые срезы можно проверять на серверном контуре без ручного чтения всех внутренних БД.

### Этап 6. Укрепление до MVP

Цель: убрать ручные хвосты, которые мешают использовать платформу для собственной разработки.

Состав:

- базовая security-проверка и governance уязвимостей по #380;
- dev/demo seed-данные и fixture-наборы по #294;
- минимальный self-improve контур внешних документационных репозиториев по #78;
- решение по knowledge storage и памяти агентов по #586 только после отдельной проработки;
- эксплуатационные runbooks, monitoring и rollback для всех MVP-сервисов.

## Распределение агентов после стабилизации `dupl-go`

- Агент #1: `project-catalog`, bootstrap/adoption, связь provider signals с импортом проверенной политики.
- Агент #2: `provider-hub` и `integration-gateway`, provider snapshots/signals, webhook/callback контур и provider parity по отдельному решению.
- Агент #3: `agent-manager`, flow/run/session/activity/acceptance/follow-up/Human gate refs и workspace context.
- Агент #4: `interaction-hub`, owner inbox, feedback, approval, Human gate, callbacks и связь с gateway/MCP.
- Агент #5: `codex-hook-ingress`, качество Go helpers после удаления baseline, hooks/skills и ops/realtime feed при необходимости.
- Агент #6: `governance-manager`, risk/gate/release decision refs и безопасное обогащение из owner-доменов.

## Когда создавать новые Issues

Открытые GitHub Issues остаются верхнеуровневыми якорями. Новые Issues нужно создавать не на каждую мелкую правку, а когда есть самостоятельный проверяемый срез:

- новый сервис или deploy-контур;
- новый gateway-контракт;
- backend-операция, которая закрывает часть сквозного сценария;
- фронтенд-экран или группа экранов;
- cross-domain integration с отдельной приёмкой;
- технический cleanup, который блокирует все PR.

Если срез является частью уже открытого крупного Issue, PR должен ссылаться на крупный Issue, но не обязан закрывать его, пока критерии эпика не выполнены.

## Проверки перед переходом к фронтенду

- Сквозной backend-срез проходит smoke без UI.
- `staff-gateway` имеет OpenAPI и сгенерированные клиенты.
- Основные экраны имеют подтверждённые источники данных или явно помеченные временные заглушки.
- Серверный deploy позволяет агентам смотреть логи pod, readiness и smoke-вывод.
- В UI не появляются поля, для которых нет owner-сервиса или понятного источника данных.

## Риски

- Риск: агенты продолжают закрывать разрозненные Issues без движения к сквозному сценарию.
  Решение: каждый следующий срез привязывать к этапам этого плана.
- Риск: фронтенд начнётся до готового `staff-gateway` и начнёт зависеть от внутренних gRPC/API.
  Решение: UI-срезы с живыми данными начинать только после OpenAPI-границы.
- Риск: первый deploy откладывается до “идеального MVP”.
  Решение: backend deploy начинать раньше, как только есть минимальные манифесты и smoke.
- Риск: черновые макеты устареют относительно backend-реальности.
  Решение: при фронтенд-срезах обновлять рядом `screen.md` и, если нужно, макеты в `refactoring/images/wave5/**`.
