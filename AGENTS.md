---
id: AGENT-DOC-001
title: Инструкции для агентов MatterCodex
type: agent-rules
status: approved
owner: manager
version: 2.2.0
updated: 2026-07-31
---

# Инструкции для агентов MatterCodex

Документ `AGENT-DOC-001` - источник рабочих правил для всех агентов, которые
работают с репозиторием `codex-k8s/matter-codex`.

## Общие правила

- Перед началом работы прочитать `AGENT-DOC-001`, связанный GitHub Issue, PR и релевантные документы по их кодам из `GOV-DOC-001`.
- Вся работа ведется только через GitHub Issues. Исключение - создание самого Issue.
- Большая работа оформляется как эпик с подзадачами. Подзадача должна быть отдельным GitHub Issue или явной checklist-ссылкой на Issue.
- Перед планированием эпика, параллельных волн или реализацией сервиса,
  gateway, PWA либо job прочитать `GUIDE-DOC-004`.
- Один unit проекта реализуется полностью одним большим PR: один внутренний
  сервис, один внешний gateway, одна PWA или одна самостоятельно
  развертываемая job. Дробить unit на PR по слоям запрещено.
- Каждый PR должен быть связан с Issue и содержать ручную проверку для владельца.
- Прямой push в `main` запрещен. Все изменения идут через отдельную ветку и PR.
- Перед изменением структуры сервиса или PWA прочитать `REPO-DOC-001`.
- Перед изменением Go-сервиса, gateway или Go job прочитать `GO-DOC-001` и
  `GO-DOC-003`; при работе с PostgreSQL или миграциями дополнительно прочитать
  `GO-DOC-002`, а при публикации или потреблении доменных событий -
  `GO-DOC-004` и `GO-DOC-005`. Перед добавлением либо копированием общего
  runtime-примитива прочитать `GO-DOC-006`.
- Перед изменением Proto/gRPC прочитать `CONTRACT-DOC-002`, а перед изменением
  AsyncAPI, domain event или NATS subject - `CONTRACT-DOC-003`.
- Перед изменением межсервисной авторизации, mTLS, JWS/JWKS, Vault, TLS
  lifecycle, security sidecar или `NetworkPolicy` прочитать `GUIDE-DOC-003`.
- Перед изменением защищённого вида, control plane, планировщика, фоновой
  задачи, owner gate, lease/claim/retry либо межсессионного делегирования
  прочитать `GUIDE-DOC-006` и составить полную матрицу жизненного цикла.
- Перед изменением Vue/TypeScript PWA прочитать `FE-DOC-001`.
- Перед заявлением результата проверки прочитать `GOV-DOC-003`.
- Production-действия выполняются только после отдельного подтверждения владельца.
- Если есть противоречие между договором, ТЗ, окружением, Issue или запросом владельца, остановиться и дать владельцу варианты решения.
- Не раскрывать секреты, token values, kubeconfig, SSH passwords, OAuth secrets, Vault root/unseal data, cookies, DSN, private keys и персональные данные.
- В документации, Issue и PR можно указывать только имена env/secret keys и факт настройки, без значений.
- Не давать developer/reviewer owner-level доступ.

## Язык

- Ответы агентов, GitHub Issues, PR, review comments, commit messages, кодовые комментарии и документация пишутся по-русски.
- Английский оставлять для имен файлов, code identifiers, API, env keys, GitHub labels и устойчивых технических терминов.
- Все runtime-логи, тексты ошибок исполняемого кода, CLI/API-диагностика и
  Prometheus `HELP` пишутся на английском. Это правило распространяется на
  сервисы, gateway, jobs и `libs/go`.
- Повторяющийся диагностический текст или ключ структурированного лога
  выносится в именованную константу в минимальной общей области.
- Не использовать лишние английские слова, если есть понятный русский вариант.
- В пользовательских текстах не смешивать языки без необходимости.

## Источники требований

Решения владельца и утвержденные документы `docs/product/**`,
`docs/architecture/**`, `docs/domains/**` и `docs/decisions/**` определяют
функциональные и архитектурные границы MatterCodex. История удалённой
Mattermost-first реализации доступна через Git и не является текущей каноникой.

Технические и управленческие правила адаптированы из
`codex-k8s/project-template` на зафиксированной ревизии, указанной в
`GOV-DOC-004`. Если источники противоречат друг другу, агент не придумывает
приоритет, а запрашивает решение владельца через manager.

## Актуальная документация по библиотекам

Если задача касается библиотеки, framework, SDK, CLI, cloud service или внешнего API, агент должен получить актуальные документы через Context7 MCP:

1. Найти идентификатор библиотеки через `resolve-library-id`.
2. Получить документы через `get-library-docs`.
3. В PR или ответе указать, какие документы были проверены.

Если Context7 MCP недоступен, нужно явно написать это в PR/ответе и использовать официальную документацию.

## Обязательная сквозная проверка контрактов

- Перед изменением OpenAPI, Proto, AsyncAPI, машинной policy или фонового
  процесса составить карту каждого затронутого
  сценария от инициатора до авторитетного владельца состояния и потребителя
  результата.
- Карта сценария должна явно связывать источник требования, actor и источник
  его полномочий, внешний endpoint, gateway mapping, внутренний RPC/command,
  владельца данных, idempotency/version, ответ или ошибку, переход состояния,
  событие и потребителей. Скрытое преобразование или «реализация определит
  позже» считается пробелом контракта.
- Поле request, токен обычного клиента и идентификатор из payload не являются
  источником authority. Actor, organization, provider connection и владение
  ресурсом выводятся из проверенного transport/signed context или состояния
  домена-владельца.
- При create owner-поля назначает сервер. При update/delete существующий ресурс
  сначала разрешается внутри доверенной owner/tenant boundary; только затем
  проверяются version и idempotency. Idempotency scope и `If-Match` не заменяют
  проверку владения.
- Несущие полномочия и исполняемые виды не обслуживаются универсальными
  `create|update|transition|delete`: для каждого вида нужен закрытый реестр
  специализированных команд. Actor/owner/root lineage назначаются или
  разрешаются сервером; самовыдача и полномочия на жизненный цикл всего проекта
  запрещены.
- Для каждого terminal, cancel, delete, retry и частичного перехода явно
  фиксируется одно из двух: атомарное событие с origin, condition, cardinality и
  consumers либо отсутствие события с авторитетным read path. Обобщать это
  правило сразу на несколько task kind без отдельной матрицы запрещено.
- Фоновая task связывается с точными workload, task, tenant, aggregate,
  version, attempt и immutable input snapshot. Retry создаёт новую attempt,
  event и grant; cancel/delete/terminal закрывают task и отзывают grant в
  owner-транзакции.
- Grant/claim фоновой работы связывается с точными workload, полным методом,
  session, turn, attempt, immutable input digest и монотонным поколением.
  Межсессионный дочерний процесс запускается только через принадлежащее серверу
  ребро делегирования и наследует root actor/policy/route; идентификаторы
  parent/child из payload не доказывают происхождение.
- Create, claim, renew, complete, cancel, delete, retry, lease expiry,
  dead-letter, `WAITING_OWNER` и `CHANGES_REQUESTED` проверяются как переходы
  полного графа выполнения. Одна транзакция владельца закрывает прежние leases,
  grants, claims и связанные агрегаты; частичный terminal envelope запрещён.
- Перед каждым turn/retry/continuation сервер создаёт свежую immutable
  `RuntimeRevision` из точного набора активных grants и версий/дайджестов
  зависимостей. Ссылочное событие имеет защищённый version-pinned read/rejoin
  path каждого
  consumer либо несёт полный безопасный snapshot.
- Новый gateway, worker, sidecar, verifier, reconciler или job считается
  реализуемой частью профиля только при наличии записи в реестре, интерфейса,
  конфигурации, deploy ownership, readiness/failure policy и способа доставки
  необходимых ключей или метаданных.
- Reviewer проверяет сквозной сценарий и системные аналоги, а не только
  корректность каждого файла по отдельности. Совпадение имён между форматами не
  доказывает совпадение типов, authority, lifecycle и ошибок.
- Полная материализация интерфейса включает producer/client operation profile,
  authority registration, generated adapter, consumer effect/inbox,
  readiness рабочего пути, deploy ownership и итоговый environment render.
  Декларация в policy или registry без исполняемого consumer не считается
  реализацией.
- Конфигурация с несколькими источниками записи хранит назначаемые сервером
  `managed_by=ui|git`, source и immutable revision; UI меняет Git-owned объект
  только через отдельный `detach|copy`. FTS/vector проекция хранит source
  version/content/model
  provenance и не расширяет tenant/owner eligibility.

## Обязательная проверка security boundary

- mTLS подтверждает transport peer, но не заменяет обязательный bearer token,
  authorization context, exact permission или replay protection. Client
  adapter собирает все утвержденные слои, а readiness проверяет тот же путь,
  что и рабочий RPC.
- Публичное и привилегированное чтение используют один авторитетный доменный
  eligibility rule для одиночного, пакетного, поискового и event path.
  Неизвестное состояние и скрытый ресурс закрыто отклоняются.
- Rollback/replay high-watermark принадлежит проверяющей стороне, переживает
  pod replacement и не управляется caller. `emptyDir` и in-memory state для
  этой границы недостаточны.
- Ротация ключей, signer certificate и CA описывается как forward-only
  protocol с overlap, crash recovery, пропуском обновлений и exact readback
  фактически обслуживаемого состояния.
- Secret transport использует TLS с exact SNI/hostname и доверенной CA.
  `skipTLSVerify`, plaintext fallback и wildcard egress запрещены.
- `NetworkPolicy` проверяется по итоговому environment render. Для database,
  broker, Vault, telemetry и Kubernetes API правило только по порту без
  exact destination запрещено.
- PostgreSQL RLS связывает scope с неизменяемым `session_user` и устойчивым
  поколением credential; caller-set GUC не является identity. Retire является
  forward-only и включает фактически достижимые `NOLOGIN`, revoke membership,
  termination и readback через минимальную controller identity.
- Результат чтения кэша до выдачи точно связывает tenant/project/kind/id/version,
  key и source/projection digests. Повреждение или mismatch всегда ведёт к
  авторитетному PostgreSQL fallback, а не к выдаче snapshot.
- Outbox не пропускает unpublished/terminal predecessor одного ordering key,
  сохраняет durable broker receipt дольше broker retention и разблокируется
  только bounded tenant-scoped audited repair/skip. Readiness сверяет exact
  durability, retention, message/bytes/age/dedup/delete limits.
- BuildKit и registry используют точные mTLS/application identities; pull,
  staging push, admin и promotion физически разделены. Promotion разрешается
  только после назначаемого сервером допуска по provenance, SBOM, заключению об
  уязвимостях и signature; node pull доказывает exact digest через достижимую
  границу DNS/CA/runtime без небезопасного запасного пути.
- После устранения security или architecture замечания исполнитель проверяет,
  является ли его причина общей. Общий инвариант закрепляется в
  `GUIDE-DOC-003` либо профильном техническом гайде, а не остается знанием
  одного PR.

## GitHub Issues и PR

- Issue должен быть понятен человеку без знания внутренней рабочей среды команды.
- Issue должен содержать задачу, контекст, источники, acceptance criteria, ручную проверку и ограничения.
- PR должен содержать:
  - что изменено;
  - связанное Issue;
  - как проверить вручную;
  - только фактически выполненные проверки активного профиля без удалённых,
    будущих или несуществующих suites;
  - ссылку на follow-up Issue для отложенного тестового контура, если он
    применим;
  - риски и rollback, если применимо;
  - подтверждение, что секреты не раскрыты.
- Документы, предлагаемые к слиянию, должны быть готовы к согласованию и иметь `status: approved` во frontmatter.
- Не предлагать к слиянию документы со `status: draft`. Если документ не готов, не включать его в PR и вынести доработку в отдельный Issue.
- Reviewer не вносит изменения в ветку автора без прямого запроса владельца. Reviewer оставляет review comments.

## Стратегия разработки и проверок

- Актуальный режим разработки, обязательные проверки и правила GitHub
  ruleset фиксируются в `GOV-DOC-003`.
- Для web-first платформы активен профиль `Web-first baseline`: форматирование,
  статический разбор, сборка, contract/codegen, unit, PostgreSQL component,
  frontend unit/build, browser E2E, security и render-проверки выполняются во
  всей применимой изменённой области.
- Тестовая оснастка является частью полного unit, когда без неё нельзя
  воспроизводимо доказать обязательный lifecycle, authorization, realtime или
  пользовательский сценарий. Простому glue-коду тест ради покрытия не нужен.
- Недоступная безопасная disposable-среда, браузер или внешний optional adapter
  фиксируются как `NOT RUN`, а ошибка существующей обязательной проверки — как
  `FAIL`. Ни один из этих исходов нельзя называть `PASS`.
- Отсутствие GitHub check не является успешным CI. Локальный запуск
  обозначается как локальный и привязывается к точному SHA.
- Staging/production проверки и само развёртывание требуют отдельного разрешения
  владельца; локальный baseline не обращается к живой среде и её данным.
- Проверка становится поддерживаемой, когда имеет публичную точку входа,
  ограниченный бюджет, однозначный ожидаемый результат и безопасные fixtures.
- Рецензирование относится к фактическому diff, архитектурным и security
  границам. После замечания исполнитель ищет системные аналоги во всем наборе
  изменений и связанных контрактах, документации, коде и конфигурации.
- Статически доказанный production defect, недостижимый обязательный path или
  fail-open boundary остаётся finding независимо от наличия live-проверки.
- Завершенный unit проходит одновременно продуктовое, security и архитектурное
  review на одном SHA. После исправлений все три направления повторно
  проверяют новый SHA; human gate запрещен, пока есть хотя бы одно незакрытое
  замечание или отсутствует явное подтверждение одного из направлений.
- Статус review thread `resolved` не является доказательством исправления:
  проверяются новый diff, исходный failure path и все найденные системные
  аналоги.
- Ошибки и предупреждения синтаксического анализатора или компилятора должны
  приводить к закрытому отказу до семантического использования восстановленной
  промежуточной модели.
- Проектная проза, комментарии и документация пишутся по-русски; runtime-логи,
  ошибки исполняемого кода, CLI/API-диагностика и Prometheus `HELP` — на
  английском. Идентификаторы, команды, канонические статусы, названия продуктов
  и неизмененный внешний вывод инструментов не переводятся. Глобальный
  словарный запрет без учета контекста не использовать.
- После перебазирования исполнитель сохраняет изменения актуального `main`,
  сравнивает область изменений с Issue и публикует переписанную ветку только
  через `--force-with-lease`.
- Слияние и переход к следующей волне допускаются только после обязательных
  проверок текущего профиля, закрытых review threads и отдельного ручного
  шлюза владельца.

## Обязательные lifecycle и observability invariants Go

При изменении Go-сервиса, gateway, job, `libs/go/observability`,
`libs/go/grpcserver`, метрик или alert rules developer и reviewer учитывают
следующие инварианты:

- единый предикат неожиданных gRPC-кодов охватывает полный утвержденный набор
  `Internal`, `Unavailable`, `Unknown`, `DataLoss` и не создаёт дублирующие
  error-log или error observer events;
- все значения metric labels происходят из закрытых множеств; произвольный
  HTTP method, route, operation или внешний status нормализуется в одно
  ограниченное fallback-значение с фиксированной кардинальностью;
- если операция возвращает частичный результат вместе с ошибкой, метрики уже
  совершенных внешних действий и устойчивых изменений состояния учитываются
  независимо от итогового outcome цикла;
- workers не совершают внешних действий до прохождения startup barrier,
  принадлежат явному cancel/join-контракту и завершаются до закрытия
  PostgreSQL, Redis и других используемых зависимостей;
- первый межсервисный consumer не запускает broker subscription, пока
  producer relay и durable inbox не прошли обязательные readiness-проверки;
  consumer effect, inbox row и cursor фиксируются одной транзакцией;
- tracing shutdown, Sentry flush и другие обязательные cleanup-операции
  получают независимые bounded contexts от неотмененного базового контекста;
  исчерпание бюджета одной операции не отменяет следующую;
- каждый alert содержит доступный абсолютный HTTPS `runbook_url`.

## Каноническая инженерная структура

- Новый внутренний Go-сервис размещается в `services/internal/<service>`.
- Новый внешний gateway размещается в `services/external/<gateway>`.
- Новая фоновая задача размещается в `services/jobs/<job>`.
- Новая PWA владельца и операторов размещается в
  `services/staff/control-center`. Другие audience-зоны добавляются только
  отдельным продуктовым решением.
- Плоские `services/<name>/domain`, параллельные пакеты `application`/`storage`, SQL в Go-строках и repository interfaces рядом с PostgreSQL-реализацией запрещены.
- Entities, values, enums, queries, domain services, repository ports, adapters, transports, clients и composition root размещаются строго по `REPO-DOC-001` и `GO-DOC-001`.
- Исходные OpenAPI, Proto и AsyncAPI находятся в `contracts/`; generated code не редактируется вручную.
- Переиспользуемый Go runtime, HTTP/gRPC boundary, transactional outbox,
  broker-neutral relay, NATS JetStream adapter и durable inbox размещаются в
  `libs/go`; сервисный код наблюдаемости содержит только бизнесовые метрики и
  находится в `internal/observability`.
- Любая операция внутреннего сервиса проходит канонический путь
  `transport -> caster -> domain service -> repository/client port -> adapter`.
  State-changing command атомарно фиксирует business state, idempotency
  receipt, audit и каждое обязательное domain event в одной PostgreSQL
  transaction.
- Синхронные внутренние вызовы выполняются по Proto/gRPC через generated
  client adapter. Асинхронные факты доставляются по AsyncAPI через
  PostgreSQL outbox, provider-neutral relay, NATS JetStream и durable
  PostgreSQL inbox/cursor. Прямое чтение БД другого сервиса и publish в NATS из
  handler/domain service запрещены.
- Общий Kubernetes-ресурс, устанавливаемый один раз на кластер или контур,
  размещается в `deploy/k8s/base/<capability>`. Ресурс, принадлежащий одному
  deployable, размещается в `deploy/k8s/base/<component>`. Копировать общий
  dashboard или иной общий ресурс в service overlay запрещено.
- Корневой `context.Background()` создается только в `main` production-бинаря.
  Контекст жизненного цикла и отдельный базовый контекст для ограниченного
  shutdown передаются вниз явно. Создавать `context.Background()` или
  `context.TODO()` в production-коде ниже entrypoint запрещено.
- Структурный рефакторинг нескольких сервисов выполняет один агент в одном
  согласованном PR без временных двойных источников правды. До продуктового,
  security и архитектурного review владелец принимает полную структуру diff.

## SRE code-first процесс

SRE-инфраструктура выполняется только по цепочке:

1. read-only preflight;
2. script/code в `infra/**`, `deploy/**` или `tools/**`;
3. PR;
4. review;
5. отдельное owner OK;
6. запуск того же скрипта/кода из репозитория;
7. отчет о результате в Issue.

SRE не выполняет установку или настройку сервера вручную без кода в PR.

SRE использует только env, явно выданные в prompt задачи. Значения env не печатать.

Kubernetes access роли относится только к runtime-среде агентов. Доступ к
кластеру MatterCodex или внешнему staging выдаётся только через project runtime
env, явно привязанные к роли SRE.

Production-действия выполняются только после отдельного подтверждения владельца.

## QA

- QA проверяет развернутый staging через пользовательские сценарии.
- QA перед проверками готовит markdown-план с чекбоксами по сервисам, endpoints, UI-сценариям и бизнес-правилам.
- QA создает отдельный GitHub Issue с label `bug` для каждого дефекта.
- Bug Issue должен содержать severity, окружение, шаги воспроизведения, expected, actual, evidence без секретов и ссылку на требование.
- QA не получает SRE SSH/root credentials.
- QA использует только project runtime env, явно привязанные к роли QA.
- Если staging недоступен, QA оформляет blocked-отчет и ссылается на SRE/bootstrap Issue.

## Роли

### manager

- Ведет backlog, scope и приоритеты.
- Создает эпики и подзадачи.
- Передает агентам задачи только со ссылками на Issue/PR/docs.
- Поддерживает список открытых решений владельца.
- Следит за тем, чтобы работа была вручную проверяемой.
- Корневой manager ведет координацию с владельцем и держит не более двух
  активных unit. Для каждого unit он создаёт отдельный дочерний manager-тред в
  рабочем чате.
- Дочерний manager ведет один полный unit, запускает исполнителя, затем
  одновременно продуктовый, security и архитектурный review и возвращает
  результат корневому manager.
- После пакета исправлений все три направления проверяют новый точный SHA.
  Допускается не более пяти автоматических циклов; шестой требует решения
  владельца.
- Manager не сливает PR. При нуле unresolved threads и трех явных
  подтверждениях он передает результат владельцу и ждёт human gate.

### product-manager

- Выполняет продуктовое review: требования, сценарии, роли, состояния,
  полномочия, ошибки и ручную приемку.
- Не придумывает отсутствующее бизнес-правило и не расширяет MVP без решения
  владельца.
- Для защищённого lifecycle сверяет матрицу обычных, terminal, cancel, delete,
  retry, expiry, owner-decision и continuation исходов; проверяет точное
  происхождение UI/Git-конфигурации, доставку и назначаемые сервером версии
  целевых объектов.

### SRE

- Ведет staging, Kubernetes dev/staging, Vault, SSO, мониторинг, логи, трассировку, backups и runbooks.
- Работает только code-first.
- Не раскрывает секреты и не передает SSH/root credentials другим ролям.
- Не делает production-действия без отдельного owner OK.

### architect

- Переводит договор и ТЗ в архитектуру, доменную карту, интеграционную карту, ADR и backlog-ready требования.
- Не расширяет MVP без решения владельца.
- Фиксирует открытые вопросы владельцу.
- В архитектурном review проверяет весь unit, сквозные контракты, границы
  слоев, данные, события, lifecycle, observability и deploy readiness.

### developer

- Реализует код и документацию только в рамках связанного Issue.
- Работает через отдельный worktree, ветку и один PR на полный unit проекта.
- Перед реализацией читает профильные документы `REPO-DOC-001`,
  `GO-DOC-001`, `GO-DOC-002`, `GO-DOC-003`, `GO-DOC-004`, `GO-DOC-005`,
  `GO-DOC-006`, `GUIDE-DOC-003`, `GUIDE-DOC-005`, `GUIDE-DOC-006`,
  `FE-DOC-001` и актуальные
  документы через Context7 MCP в применимой части.
- Реализует принадлежащие unit Dockerfile, Kubernetes manifests, dashboards,
  alerts, README и runbook в том же PR. Общую инфраструктуру меняет только при
  явном scope Issue.
- При изменении lifecycle или наблюдаемости учитывает инварианты раздела выше
  до передачи PR на review.
- До реализации control plane или фонового процесса прикладывает список
  защищённых видов, граф выполнения, матрицу жизненного цикла/полномочий и карту
  producer→client→consumer→readiness→deploy; исправляет все системные аналоги
  найденной причины до повторного review.

### reviewer

- Работает только в архитектурном направлении; знает о параллельных
  `product-manager` и `security` и не дублирует их без собственного
  доказанного архитектурного риска.
- Проверяет весь относящийся к направлению diff, а не несколько знакомых
  файлов.
- Замечания пишет по делу, с файлом/строкой и объяснением риска.
- Не требует расширения MVP сверх ТЗ.
- После исправления проверяет новый SHA, системные аналоги и сам закрывает
  thread только при фактическом устранении причины.
- Для lifecycle и наблюдаемости сверяет полный набор кодов, отказов и
  частичных результатов, а не выводит корректность из одного штатного
  сценария.
- Для полного unit проходит контуры полномочий, жизненного цикла и развёртывания
  целиком: универсальные и специализированные команды, межсессионное
  происхождение, все terminal и recovery paths,
  producer/client/consumer/readiness, Docker inputs,
  environment render, dashboards/alerts/runbook. Ответ автора и `resolved`
  thread доказательством не являются.

### security

- Проверяет trust boundaries, authority, tenant isolation, replay,
  idempotency, secrets, PII, TLS, network policy и supply chain.
- Проверяет точную привязку grant к session/turn/attempt/input, назначаемые
  сервером owner/delegation, PostgreSQL principal/RLS capabilities, cache
  envelope и физическое разделение identities цепочки
  pull/push/admin/promotion.
- Не требует расширять продуктовый scope без доказанного security impact.
- Блокирует fail-open path, потерю authority, утечку данных и недоказанный
  production transport.

### improver

- Периодически анализирует замечания принятых PR и причины повторных дефектов.
- Улучшает `AGENTS.md`, гайды и шаблоны отдельным документационным PR.
- Не меняет архитектурное или продуктовое решение без согласования владельца.
- Для выбранного PR с полной курсорной пагинацией получает все review threads,
  вложенные комментарии, submitted reviews и conversation comments; resolved и
  outdated не отбрасывает. Замечания дедуплицирует по root cause и переносит
  только устойчивые проверяемые правила, без статуса конкретной задачи и
  временных доказательств.
- Краткие сквозные инварианты закрепляет в `AGENT-DOC-001`, подробные матрицы
  сценариев и checklist — в профильных утверждённых гайдах с регистрацией
  по `GOV-DOC-001`; существующие источники не дублирует.

### docs-acceptance

- Готовит документацию, acceptance matrix, ручные сценарии, GitHub templates, report templates и материалы для Wiki.js.
- Не меняет смысл договора и ТЗ.
- Для неоднозначных требований фиксирует вопрос владельцу.

### QA

- Проводит ежедневные проверки staging.
- Готовит markdown-план, выполняет проверки, создает bug issues и итоговый отчет.
- Не исправляет application code без отдельной задачи.

### reporter

- Готовит короткие фактические отчеты: weekly report, повестка среды, monthly summary, список блокеров.
- Не выдает планы как сделанное.
- Указывает источники: Issues, PR, docs и подтвержденные факты.

## Структура репозитория

- `.github/ISSUE_TEMPLATE/` - шаблоны GitHub Issues. Базовые правила описаны в `TPL-DOC-001`.
- `docs/governance/` - кодификация, активный профиль проверок, открытые решения
  и происхождение адаптированных правил.
- `docs/product/` - продуктовые правила, бизнес-сценарии и ограничения MVP.
- `docs/architecture/` - архитектура, доменная карта, интеграции и ADR-ссылки.
- `docs/domains/` - документация по доменам.
- `docs/operations/` - эксплуатационные правила и границы окружений.
- `docs/runbooks/` - runbooks для повторяемых действий.
- `docs/templates/` - шаблоны отчетов, приемки и рабочих документов.
- `docs/acceptance/` - матрица приемки и ручные сценарии.
- `docs/qa/` - QA-процесс, регресс и bug triage.
- `docs/decisions/` - архитектурные решения.
- `docs/guides/` - локальные проектные гайды.
- `services/internal/` - внутренние доменные Go-сервисы.
- `services/external/` - внешние API-шлюзы.
- `services/jobs/` - самостоятельно развертываемые фоновые задачи и workers.
- `services/staff/control-center` - PWA владельца и операторов.
- `deploy/` - Kubernetes-манифесты и оверлеи.
- `infra/` - инфраструктурный код и bootstrap scripts.
- `libs/go/` - устойчивые переиспользуемые Go-примитивы с отдельным module и
  узким API.
- `tools/` - утилиты разработки и генерации.

## Базовый стек

Базовый стек: Go, Vue, TypeScript, gRPC/Protobuf, OpenAPI/REST, AsyncAPI,
WebSockets, PostgreSQL, Redis, S3-compatible storage, NATS JetStream,
Kubernetes, Prometheus/Grafana, OpenTelemetry/Jaeger, Sentry, Vault, Keycloak,
Teleport, GitHub, Mattermost и Wiki.js.

Стек может уточняться при проектировании без изменения функциональных границ MVP и по согласованию с заказчиком, если это требуется ТЗ.

## Миграции и данные

- Уже примененные миграции не редактировать.
- Изменения схемы делать новой forward-only migration через goose по `GO-DOC-002`.
- Production SQL-запросы хранить по одному в отдельных файлах с обязательным заголовком `-- name: <query> :one|:many|:exec`.
- SQL literals в production Go-коде запрещены.
- Read-through кэш реализуется декоратором repository port: PostgreSQL остается
  источником истины, Redis хранит только ограниченный по TTL protobuf-снимок.
- Ключи кэша с пользовательскими или организационными идентификаторами
  хэшируются; повреждение или недоступность Redis не разрешает доступ и не
  делает Redis источником бизнес-состояния.
- Destructive changes требуют отдельного архитектурного решения и безопасного пути `expand -> migrate -> contract`.
- Ручной SQL в staging/production без runbook и Issue запрещен.

## Безопасность и конфиденциальность

- Не коммитить секреты, токены, ключи, kubeconfig, cookies, DSN и реальные персональные данные.
- Не логировать сырые секреты и персональные данные.
- Не добавлять русскоязычные runtime-логи или тексты ошибок в исполняемый
  Go-код; повторяющиеся диагностические строки и log attribute keys оформлять
  константами.
- Не использовать production-секреты в AI-инструментах.
- В тестовых данных не использовать реальные персональные данные.
- Если агент увидел секрет в diff или логах, не повторять значение. Нужно указать только факт secret exposure и потребовать ротацию.
