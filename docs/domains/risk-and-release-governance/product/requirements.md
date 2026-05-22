---
doc_id: PRD-CK8S-RISK-GOVERNANCE-0001
type: prd
title: kodex — требования домена рисков и релизов
status: active
owner_role: PM
created_at: 2026-05-22
updated_at: 2026-05-22
related_issues: [322, 769]
related_prs: []
related_docsets:
  - docs/platform/architecture/domain_map.md
  - docs/platform/architecture/service_boundaries.md
  - docs/platform/architecture/data_model.md
  - refactoring/14-risk-and-release-governance.md
  - refactoring/15-human-gates-and-evidence.md
  - refactoring/16-release-safety-observability-and-notifications.md
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-05-22-risk-governance-kickoff"
---

# PRD: риски и релизы

## TL;DR

- Что строим: домен `governance-manager` для risk gates, role-driven review gates, policy-based approvals, release decisions и release safety-loop.
- Для кого: Owner, операторы, `agent-manager`, ролевые reviewer/QA/gatekeeper агенты, `project-catalog`, `provider-hub`, `runtime-manager`, будущие gateway/UI.
- Почему: риск, gate и релизное решение должны иметь одного владельца, иначе policy расползётся между проектным каталогом, агентной оркестрацией и уведомлениями.
- Минимум первой версии: риск-профили, оценка риска перехода, review signals, gate request/decision, release decision package и события `governance.*`.
- Критерии успеха: низкорисковые переходы не блокируются лишним человеком, а high-risk переходы и релизы не проходят без объяснимого evidence и обязательного Human gate.

## Проблема и цель

Проблема:
- `project-catalog` хранит проектную, веточную и релизную политику, но не должен становиться владельцем каждого risk decision;
- `agent-manager` управляет flow, ролями и acceptance, но не должен единолично решать, что риск принят человеком;
- `interaction-hub` доставляет запросы и callbacks, но не должен владеть бизнес-логикой gate;
- `runtime-manager` выполняет build/deploy/cleanup `job`, но не принимает release go/no-go;
- без отдельного владельца risk state невозможно объяснить, почему переход заблокирован, кто повысил риск и какое решение разрешило дальнейшее действие.

Цель:
- выделить `governance-manager` как сервис-владелец политики риска, оценки риска, review signals, gate decisions и release decisions;
- связать governance с проектной политикой без дублирования `project-catalog`;
- отделить решение риска от доставки уведомлений и внешних callbacks, которые остаются у `interaction-hub`;
- подготовить docs-first основу для контрактов и последующих сервисных срезов.

## Пользователи и роли

| Роль | Главный сценарий |
|---|---|
| Owner | Утверждает продуктовые документы, high-risk переходы и релизные решения без обязательного построчного code review. |
| Технический reviewer | Создаёт review signal о корректности реализации, контракта, данных или инфраструктуры. |
| `qa` | Создаёт signal о проверенном поведении, дефектах, smoke/e2e результате и остаточных рисках. |
| `lexical-gatekeeper` | Проверяет язык, структуру, терминологию, канонику документации и создаёт review signal. |
| Risk gatekeeper | Анализирует risk profile, diff, target environment и signals; рекомендует повышение риска или дополнительные gates. |
| Оператор / `sre` | Подтверждает release readiness, postdeploy signals, hold/rollback/follow-up. |
| `agent-manager` | Запрашивает оценку риска, передаёт результаты ролей и ждёт governance decision перед переходом flow. |
| `project-catalog` | Предоставляет проект, репозиторий, `services.yaml`, branch rules, release policy, release line и risk profile refs. |
| `provider-hub` | Предоставляет provider-native проекции, diff/PR metadata, review/comment signals и выполняет provider-команды только с нужным gate ref. |
| `runtime-manager` | Предоставляет job/deploy/postdeploy signals и выполняет runtime-действия только после разрешённого решения. |
| `interaction-hub` | Доставляет human gate запросы, уведомления и callbacks; не решает риск сам. |

## Функциональные требования

| ID | Требование | Приоритет |
|---|---|---|
| GOV-FR-1 | Домен должен иметь отдельный сервис-владелец `governance-manager` для risk policy, risk assessment, review signals, gate decisions и release decisions. | Обязательно |
| GOV-FR-2 | Домен должен хранить риск-профили и gate policy как governance-истину, но использовать `project-catalog` как источник проектов, репозиториев, `services.yaml`, branch rules, release policy и release line. | Обязательно |
| GOV-FR-3 | Домен должен поддерживать scopes риск-политики: platform, organization, project, repository, service, path/glob, API endpoint, database object, secret-bearing area, runtime operation и release line. | Обязательно |
| GOV-FR-4 | Домен должен автоматически классифицировать риск по типу файла, сервису, API/ручке, БД, секрету, auth-зоне, runtime-действию, branch/release policy usage и target environment. | Обязательно |
| GOV-FR-5 | Итоговый риск перехода должен вычисляться по правилу "сильнейший риск побеждает" и объясняться списком факторов. | Обязательно |
| GOV-FR-6 | Понижение риска ниже автоматически рассчитанного класса допускается только явным Human decision с причиной. | Обязательно |
| GOV-FR-7 | Домен должен фиксировать историю risk class: initial classification, повышения, ручные понижения, источник решения, время, обоснование и активированные gates. | Обязательно |
| GOV-FR-8 | Домен должен определять, какие изменения требуют обязательного Human gate: auth/SSO/OIDC, секреты, production write-path, destructive operations, production DB migration, cluster-impact, release deploy, rollback/recovery, изменение branch/release policy, изменение risk/gate policy, risky `services.yaml` paths и high-impact docs approvals. | Обязательно |
| GOV-FR-9 | Домен должен принимать review signals от ролевых агентов и людей: reviewer, QA, lexical gatekeeper, risk gatekeeper, SRE, security и custom roles. | Обязательно |
| GOV-FR-10 | Review signal должен иметь автора, роль, target ref, severity, outcome, evidence refs, confidence и связь с конкретным transition или release candidate. | Обязательно |
| GOV-FR-11 | Домен должен собирать evidence package для Human gate и release decision: context, risk class, factors, acceptance result, review signals, runtime/job status, provider refs, known limitations и requested decision. | Обязательно |
| GOV-FR-12 | Owner approval должен подтверждать смысл документа, high-risk переход или release decision; он не должен автоматически означать построчный code review. | Обязательно |
| GOV-FR-13 | Домен должен поддерживать safe automation для `R0`/низкого `R1`: если policy, checks и signals позволяют, переход проходит без Human gate. | Обязательно |
| GOV-FR-14 | Домен не должен блокировать автоматизацию только из-за наличия агента: блокировка возникает из risk class, policy, missing evidence или blocking signal. | Обязательно |
| GOV-FR-15 | Домен должен отделять gate decision от доставки: `governance-manager` создаёт и хранит решение, `interaction-hub` доставляет запрос и возвращает callback/result. | Обязательно |
| GOV-FR-16 | Домен должен публиковать события `governance.*` для оценки риска, review signals, gate lifecycle, release decision и safety-loop state. | Обязательно |
| GOV-FR-17 | Домен должен поддерживать идемпотентные команды, expected version и аудит для всех решений, влияющих на переход. | Обязательно |
| GOV-FR-18 | Домен не должен создавать UI/gateway, сервисный код, storage, миграции или evaluator до согласования контрактного среза; proto и AsyncAPI появляются отдельным контрактным срезом после стартового пакета документации. | Обязательно |

## Критерии приёмки

| ID | Критерий |
|---|---|
| GOV-AC-1 | Если `PR/MR` меняет только безопасную документацию, governance объяснимо классифицирует переход как `R0` и не требует лишнего release gate. |
| GOV-AC-2 | Если diff затрагивает секреты, auth, production DB migration, destructive runtime operation или release deploy, governance создаёт обязательный Human gate. |
| GOV-AC-3 | Если role reviewer, QA и lexical gatekeeper оставили signals, governance включает их в evidence package и учитывает blocking outcomes перед переходом. |
| GOV-AC-4 | Если Owner утверждает документ или релиз, decision record показывает, что именно разрешено: docs approval, merge transition, deploy, hold, rollback или follow-up. |
| GOV-AC-5 | Если `interaction-hub` не доставил callback вовремя, gate остаётся в `hold` или `awaiting_decision`, а не считается одобренным. |
| GOV-AC-6 | Если `project-catalog` меняет release policy или branch rules, governance читает новую policy/ref и пересчитывает gates без копирования проектной политики к себе. |
| GOV-AC-7 | Если low-risk automation имеет все checks и не имеет blocking signals, governance разрешает переход без человека. |

## Что не входит

- Не хранить проект, репозиторий, `services.yaml`, branch rules, release policy и release line как свою проектную истину.
- Не владеть flow, stage, role, prompt, session, `Run` и acceptance machine; это зона `agent-manager`.
- Не владеть `Issue`, `PR/MR`, комментариями, review у провайдера и webhook; это зона `provider-hub`.
- Не выполнять checkout, build, deploy, cleanup или rollback `job`; это зона `runtime-manager`.
- Не доставлять уведомления, не хранить диалоговые ветки и внешние callbacks; это зона `interaction-hub`.
- Не делать UI/gateway в стартовом доменном срезе.

## Нефункциональные требования

| ID | Категория | Требование |
|---|---|---|
| GOV-NFR-1 | Надёжность | Risk assessment, gate decision и release decision должны быть идемпотентны и воспроизводимы по сохранённым evidence refs. |
| GOV-NFR-2 | Объяснимость | Каждая классификация риска должна показывать сработавшие правила и факторы повышения. |
| GOV-NFR-3 | Безопасность | Решения не должны содержать секреты, полный diff, сырые provider payload или полные runtime logs. |
| GOV-NFR-4 | Наблюдаемость | Сервис логирует command id, target ref, risk class, gate status, decision outcome и correlation id без приватных данных. |
| GOV-NFR-5 | Расширяемость | Новые роли, signal kinds, risk rules и gate policies добавляются через версионируемую policy, а не через изменение соседних сервисов. |
| GOV-NFR-6 | Совместимость | GitHub — первая provider-связка, но refs и signals должны допускать GitLab. |

## Зависимости

| Зависимость | Зачем нужна |
|---|---|
| `project-catalog` | Проектная политика, `services.yaml`, сервисы, branch rules, release policy, release line, risk profile refs. |
| `agent-manager` | Flow/run context, acceptance results, role outputs, запросы оценки риска и ожидание governance decision. |
| `provider-hub` | Provider-native проекции, diff/PR metadata, review/comment signals, проверка gate refs при provider write operations. |
| `runtime-manager` | Job/deploy/postdeploy/cleanup status, blocking signals, target environment и runtime action refs. |
| `interaction-hub` | Доставка human gate запросов, reminders, escalation, callbacks и внешние каналы. |
| `access-manager` | Проверка прав на управление risk policy, принятие gate/release решений и high-risk actions. |
| `operations-hub` | Проекции чтения для операторского risk/release состояния. |
| будущие gateway/UI | Внешняя поверхность для чтения decision package и отправки human decision без владения бизнес-логикой. |

## Апрув

- request_id: `owner-2026-05-22-risk-governance-kickoff`
- Решение: pending
- Комментарий: требования фиксируют целевую границу отдельного `governance-manager`; контрактный срез добавляет transport/API без сервисной реализации.
