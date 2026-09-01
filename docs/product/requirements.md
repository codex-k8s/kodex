---
id: PRD-MC-005
title: Требования web-first платформы
type: product-requirements
status: approved
owner: product
version: 1.1.0
updated: 2026-08-29
---

# Требования web-first платформы

## Функциональные требования

- `FR-001`: платформа поддерживает `Organization`, пользователей, platform roles,
  членство в организации и отдельные project permissions.
- `FR-002`: `Project` является единственным пользовательским контейнером работы
  и не требует repository, Mattermost Team, Kubernetes namespace или иной
  внешней сущности.
- `FR-003`: пользователь создаёт ИИ-сотрудника с назначением, аватаром,
  инструкциями, моделью, образом роли, capabilities, знаниями и integration
  grants без ввода внутренних идентификаторов.
- `FR-004`: инструкции имеют draft, validation, immutable published version и
  rollback с сохранением provenance.
- `FR-005`: каждый обычный turn выполняется в отдельном Kubernetes Pod из exact
  promoted role image. Образ содержит собственное окружение, инструменты и ПО
  роли, а также защищённый `agent-runner` runtime ABI.
- `FR-006`: `RoleImageRecipe` имеет канонический hash; сборка выполняется
  `role-image-builder` через изолированный BuildKit и допускается к запуску
  только после provenance, SBOM, vulnerability, signature и promotion checks.
- `FR-007`: `Workflow` описывает координатора, разрешённых агентов, bounded input,
  инструкции, concurrency, timeout, completion criteria, Human Gates и result
  schema и не предполагает software delivery.
- `FR-008`: пользователь может напрямую запустить агента или Workflow из Control
  Center; schedule не используется как техническая обёртка ручного запуска.
- `FR-009`: сессия поддерживает долговечную историю, последовательные FIFO turns,
  продолжение и дополнительное задание без внешнего чата.
- `FR-010`: агент делегирует работу дочернему агенту через типизированный
  платформенный MCP-инструмент. Control-plane создаёт server-owned child Run,
  RunNode, RunEdge и callback route; display name и prompt не дают authority.
- `FR-011`: `Run` поддерживает root/child hierarchy, attempts, cancel, retry,
  terminal result, usage, errors, artifacts и полный аудит.
- `FR-012`: Control Center отображает server-owned execution graph, выбранный
  node, timeline, progress, Human Gates, incidents, artifacts и доступные
  следующие действия.
- `FR-013`: `RunEvent` имеет монотонный sequence в пределах root Run; браузер
  получает snapshot и resumable ordered deltas, восстанавливает gap и игнорирует
  duplicate.
- `FR-014`: Human Gate является долговечным one-winner состоянием с OCC,
  идемпотентным exact replay и stale/conflict readback.
- `FR-015`: файлы загружаются, сканируются, versioned, связываются с input/result
  и скачиваются через ограниченный grant; Mattermost binding не требуется.
- `FR-016`: schedule запускает агента или Workflow и содержит timezone, input,
  session policy, notification policy и owner-friendly preset.
- `FR-017`: integration model состоит из definition, connection, typed
  capability и grant. Пустой каталог и ноль подключений являются штатным Ready
  состоянием.
- `FR-018`: MCP остаётся runtime-протоколом инструментов. Каждый инструмент имеет
  типизированную schema, специализированный adapter, capability/grant boundary,
  audit и bounded result; небезопасный универсальный proxy запрещён.
- `FR-019`: Mattermost является одной необязательной интеграцией с независимо
  включаемыми inbound messages, notifications, result mirror и Human Gate
  decisions.
- `FR-020`: core startup, readiness, execution, Human Gates и artifacts работают
  при полностью отключённых Mattermost, GitHub, GitLab и Kubernetes-интеграциях.
- `FR-021`: `Помощник Kodex` автоматически создаётся при bootstrap, имеет
  stable key, protected versioned core prompt, owner supplement, durable history
  и не может быть удалён, архивирован или отключён.
- `FR-022`: системный помощник использует тот же закрытый registry
  специализированных MCP-инструментов и те же полномочия проверенного
  пользователя, что и Control Center; прямой доступ к БД, Kubernetes и secret
  storage запрещён.
- `FR-023`: системный помощник обслуживается реальным warm runtime с resource
  limits, heartbeat, controlled revision и восстановлением после restart.
- `FR-024`: Control Center полностью поддерживает RU и EN, доступен с клавиатуры,
  адаптивен и имеет loading, empty, error, forbidden, offline и conflict states.
- `FR-025`: пользовательские ошибки передаются стабильными message keys и
  локализуются по проверенной локали пользователя; raw provider/runtime errors и
  secret values не выдаются.
- `FR-026`: доменные команды используют semantic idempotency, OCC, audit и
  transactional outbox в одной транзакции владельца состояния.
- `FR-027`: пользователь с соответствующим полномочием создаёт и редактирует
  полный Dockerfile `RoleImage`. Каждое изменение создаёт immutable revision;
  платформа валидирует Dockerfile, добавляет неизменяемый runtime contract Kodex
  и не допускает к запуску образ без успешной promotion точного digest.
- `FR-028`: `RuntimeEnvironment` pin-ит точную promoted revision образа и одной
  immutable revision объединяет обычные env values, ссылки на версии секретов,
  выбранные инструменты, resource policy, разрешённые volumes, typed network
  policy и workload RBAC profile. Изменение окружения не меняет уже созданную
  `RuntimeRevision` активной Session.
- `FR-029`: сборка образа обнаруживает и проверяет executable, окружение
  разрешает их поднабор и задаёт для каждого инструмента имя, команду, описание
  и подсказку по использованию. Readiness закрыто отклоняет окружение, если
  заявленный executable отсутствует в выбранном image digest.
- `FR-030`: редактор инструкций поддерживает ограниченный Go `text/template`,
  подсветку синтаксиса, validate и preview. Каталог переменных типизирован по
  scopes `system`, `user`, `organization`, `project`, `agent`, `environment`,
  `runtime`, `tools`, `input`, `session`, `run` и `workflow`; доступны только
  allowlisted функции, а secret values в каталог и результат рендера не входят.
- `FR-031`: пользователь создаёт и ротирует секрет через Control Center.
  Отдельный `secret-broker` с минимальным namespace-scoped доступом сохраняет
  versioned immutable Kubernetes Secret, а PostgreSQL хранит только descriptor,
  Kubernetes reference, version, ownership, lifecycle и `display_hint`.
- `FR-032`: просмотр значения секрета требует отдельного permission и свежей
  OIDC re-auth. `secret-broker` возвращает значение однократным короткоживущим
  `no-store` ответом; обычные read-модели возвращают только `display_hint`, в
  котором суммарно видно не более 15% и не более 12 символов, а короткие и
  binary secrets не раскрываются частично.
- `FR-033`: одна node server-owned execution graph представляет одну логическую
  agent Session. Turns, attempts, retries, continuations и tool calls находятся
  в timeline этой node; новая Session того же ИИ-сотрудника создаёт новую node,
  а продолжение существующей Session сохраняет её identity и FIFO-порядок.
- `FR-034`: источником подробного timeline служит нормализованный JSONL stream
  `codex exec --json`; hooks могут только обогащать события. События начала,
  прогресса и завершения tool call имеют bounded payload, привязаны к точным
  Session, Turn и attempt и не подменяются свободным текстом агента.
- `FR-035`: Live Run открывается как full-bleed canvas с pan, zoom, fit и
  различимыми состояниями nodes и edges. Summary, legend и inspector являются
  detached-панелями, activity timeline открывается в drawer, а детальный вид
  Session показывает параметры запуска, сообщения, attempts, tool calls и
  полностью материализованные prompts в пределах отдельного permission.
- `FR-036`: Kodex работает в контекстном adaptive drawer шириной 520-640 px на
  desktop и как bottom sheet на mobile. Новый диалог и история доступны явными
  controls, composer остаётся видимым, а каждое изменение сначала показывается
  полным редактируемым plan preview без скрытых операций.
- `FR-037`: рабочие страницы используют всю доступную ширину shell, а dialog
  выбирает semantic size `sm`, `md`, `lg`, `xl` или `full` по содержимому.
  Неограниченные списки и selectors имеют server-side search, cursor pagination,
  автоподгрузку и popup стабильной ширины; dropdown закрывается по выбору,
  `Escape` и клику вне него.
- `FR-038`: Human Gates представлены decision inbox с группировкой по срочности
  и Проекту. Для каждого решения до действия видны источник запроса, Run и
  Session, инициатор, причина, ожидаемый эффект, deadline, допустимые варианты и
  одна однозначная primary action; полные сведения открываются в detail panel.
- `FR-039`: аватар ИИ-сотрудника загружается в S3, обрезается, удаляется и имеет
  immutable revision. При отсутствии пользовательского изображения платформа
  использует generated fallback; ввод внешнего URL не требуется.
- `FR-040`: один общий WebSocket transport обслуживает realtime-экраны. При
  reconnect клиент продолжает stream с cursor или получает новый snapshot без
  route reload, потери несохранённой формы и сброса выбранной graph node.
- `FR-041`: каждый composer Kodex, Run chat, продолжения Session, входа
  Process/Workflow и комментария Human Gate принимает `AttachmentSet` с любым
  числом новых или ранее загруженных Project files. Продукт не задаёт лимит
  количества, а transport использует bounded chunks и batches в пределах quota
  установки.
- `FR-042`: `AttachmentSet` содержит упорядоченные immutable
  `ArtifactRevision` refs, actor, Project, purpose, binding и manifest digest.
  После scan со статусом `CLEAN` platform materialize-ит разрешённый snapshot
  read-only в отдельный workspace directory и создаёт manifest с безопасными
  именами, digest и локальными путями.
- `FR-043`: шаблонизатор предоставляет `.input.files`, `.input.files_dir`,
  `.input.files_count`, `.input.manifest_path`, `.session.files`, `.run.files`,
  `.workflow.files` и только явно выбранные `.project.files`. При continuation
  platform-controlled block всегда сообщает о новых файлах, их количестве,
  каталоге и manifest, даже если пользовательский template не использует
  файловые переменные.
- `FR-044`: все file pickers и composers поддерживают drag-and-drop, очередь,
  progress, retry и отсоединение до отправки. Файловый workspace поддерживает
  list/grid, server-side search, cursor pagination, preview, download и delete;
  действия зависят от capability и effective permission.
- `FR-045`: удаление файла является soft delete с `deleted_at` и
  `purge_after = deleted_at + 30 days`. Корзина поддерживает restore, bulk
  restore, purge selected и явную необратимую очистку с удалением точной версии
  объекта из S3 и сохранением минимального audit tombstone.
- `FR-046`: удаление отзывает будущие bindings и materialization, но не меняет
  immutable snapshot уже работающего Pod. До подтверждения UI показывает
  затронутые Runs и для немедленного отзыва предлагает отменить их; новые Turns
  и Sessions удалённый Artifact не получают.
- `FR-047`: enterprise RBAC объединяет OIDC identity и groups с platform,
  Organization и Project roles, typed permissions, resource scopes и
  instance-level bindings. Пользователь видит effective access и его источник;
  делегирование не может превысить полномочия actor или admission policy.
- `FR-048`: RBAC позволяет отдельно управлять просмотром и изменением Проекта,
  ИИ-сотрудников, Sessions, Processes, files, integrations, environments,
  secrets, secret reveal, full prompts, Human Gates и audit. В частности можно
  разрешить пользователю только запуск определённого ИИ-сотрудника в выбранных
  Проектах без доступа к его настройкам и остальным сотрудникам.

## Надёжность

- `NFR-REL-001`: принятый turn, gate, callback, artifact и schedule occurrence не
  теряются после перезапуска stateless process или Pod.
- `NFR-REL-002`: каждый background claim связан с workload, method, aggregate,
  attempt, immutable input digest и fence; callback и Human Gate continuation
  выполняются ровно один раз на уровне доменного эффекта.
- `NFR-REL-003`: retry создаёт новую attempt и RuntimeRevision, сохраняя прежнюю
  попытку и `RETRY_OF` lineage.
- `NFR-REL-004`: `/healthz` проверяет только жизнь процесса, `/readyz` читает
  локальный рассчитанный snapshot; соседний бизнес-сервис не входит в Pod
  readiness.
- `NFR-REL-005`: межсервисный граф проверяется отдельным smoke/diagnostic
  контуром. Рабочий вызов при недоступном соседе получает типизированный
  `Unavailable` или HTTP `502/503/504`.
- `NFR-REL-006`: JWKS и control-plane authorization metadata используют bounded
  last-known-good не дольше двух минут без продления на повторной ошибке;
  integrity, rollback, revision conflict и expiry закрывают доступ немедленно.
- `NFR-REL-007`: upload, scan, binding, materialization, soft delete, restore и
  purge являются идемпотентными переходами; повтор операции не создаёт вторую
  object version и не возвращает доступ к удалённому Artifact.
- `NFR-REL-008`: realtime reducer применяет только монотонные события и после
  reconnect сохраняет выбранные Session, graph viewport и несохранённые данные
  формы, если сервер не сообщил проверяемый conflict.

## Удобство и доступность

- `NFR-UX-001`: upload и drag-and-drop одинаково работают с клавиатурой и
  указателем во всех file surfaces; destructive action имеет явное
  подтверждение, а необратимый purge визуально отличается от soft delete.
- `NFR-UX-002`: dropdown, drawer, dialog, canvas controls и context inspector
  имеют предсказуемый focus management, закрываются стандартными действиями и не
  создают горизонтальный overflow на поддерживаемых desktop и mobile viewport.
- `NFR-UX-003`: unbounded selector не загружает полный список на клиент;
  server-side filter и cursor сохраняют стабильный порядок без duplicate и
  пропусков при автоподгрузке.
- `NFR-UX-004`: состояния graph nodes и edges различимы не только цветом;
  легенда объясняет семантику, а активная Session имеет доступную анимацию,
  отключаемую при `prefers-reduced-motion`.

## Безопасность

- `NFR-SEC-001`: actor, organization, project ownership и root lineage выводятся
  только из проверенного transport/signed context и server-owned state.
- `NFR-SEC-002`: секреты не попадают в prompt, log, trace, metric, audit, event,
  manifest, generated artifact, обычный frontend JSON, browser persistence или
  client cache. Единственное исключение - явно инициированный одноразовый
  `no-store` reveal response по `FR-032`, который UI не сохраняет в state.
- `NFR-SEC-003`: provider credentials и опасные integration credentials не
  передаются в role Pod; managed MCP выполняет эффект внутри integration gateway.
- `NFR-SEC-004`: role image запускается только по immutable digest из promoted
  repository и совместимому runtime ABI.
- `NFR-SEC-005`: foreign organization/project/run/session/gate/artifact access,
  opaque ref enumeration, CSRF, Origin/replay и stale session закрыто отклоняются.
- `NFR-SEC-006`: mTLS не заменяет bearer application context, exact permission и
  durable replay protection.
- `NFR-SEC-007`: каждый доступ к secret metadata, reveal, full prompt,
  Artifact, environment policy и integration grant проверяет Organization,
  Project, resource scope, exact permission и актуальную OIDC Session; отказ не
  раскрывает существование foreign resource.
- `NFR-SEC-008`: имя загруженного файла не используется как filesystem path.
  Scan, media policy, digest verification, path normalization и read-only
  materialization выполняются до передачи Artifact агенту.

## Эксплуатация и поставка

- `NFR-OPS-001`: активный `web-only` профиль включает только прямые
  инфраструктурные зависимости и не материализует optional interaction adapter.
- `NFR-OPS-002`: release lock содержит immutable digest каждого внутреннего и
  разрешённого внешнего image; zero digest, placeholder и mutable tag запрещены.
- `NFR-OPS-003`: fresh database использует одну новую baseline без legacy
  aliases, backfill, dual-read/write и cutover jobs.
- `NFR-OPS-004`: bootstrap идемпотентно создаёт Organization, initial owner
  membership contract, system assistant, capabilities, integration definitions,
  runtime defaults и system policies.
- `NFR-OPS-005`: merge и deployment выполняет только владелец после отдельного
  решения; reset живой среды не входит в implementation PR.
- `NFR-OPS-006`: до готовности MVP новая модель не содержит legacy DTO,
  aliases, dual-read/write и migration compatibility для тестовых данных
  прототипа; несовместимая замена выполняется одной канонической реализацией.
