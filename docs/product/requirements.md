---
id: PRD-MC-005
title: Требования web-first платформы
type: product-requirements
status: approved
owner: product
version: 1.0.0
updated: 2026-08-22
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
- `FR-021`: `Помощник MatterCodex` автоматически создаётся при bootstrap, имеет
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

## Безопасность

- `NFR-SEC-001`: actor, organization, project ownership и root lineage выводятся
  только из проверенного transport/signed context и server-owned state.
- `NFR-SEC-002`: секреты не попадают в prompt, log, trace, metric, audit, event,
  frontend JSON, manifest или generated artifact.
- `NFR-SEC-003`: provider credentials и опасные integration credentials не
  передаются в role Pod; managed MCP выполняет эффект внутри integration gateway.
- `NFR-SEC-004`: role image запускается только по immutable digest из promoted
  repository и совместимому runtime ABI.
- `NFR-SEC-005`: foreign organization/project/run/session/gate/artifact access,
  opaque ref enumeration, CSRF, Origin/replay и stale session закрыто отклоняются.
- `NFR-SEC-006`: mTLS не заменяет bearer application context, exact permission и
  durable replay protection.

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
