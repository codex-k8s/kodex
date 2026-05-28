# Риски и релизы

## Назначение

Домен описывает `governance-manager`: классификацию риска, risk gates, role-driven review gates, policy-based approvals, пакет данных для релизного решения, правила Human gate, правила утверждения документов, переходов повышенного риска и релизов, безопасную автоматизацию без участия человека, релизные ветки, релизные линии и политики выкладки.

## Что входит

- risk profiles, risk rules и gate policy;
- автоматическая и ручная классификация риска;
- история факторов риска и объяснение effective risk class;
- review signals от reviewer, QA, lexical gatekeeper, risk gatekeeper, SRE, security и других ролей;
- gate requests и gate decisions;
- release decision package, release decision и release safety-loop state;
- события `governance.*`;
- связь risk/release decisions с `project-catalog`, `agent-manager`, `provider-hub`, `runtime-manager`, `interaction-hub` и будущими gateway/UI.

## Что не входит

- Проекты, репозитории, `services.yaml`, branch rules, release policy и release line как проектная истина — зона `project-catalog`.
- Flow, stage, role, prompt, agent session, `Run` и acceptance machine — зона `agent-manager`.
- Provider-native `Issue`, `PR/MR`, комментарии, reviews, webhook и reconciliation — зона `provider-hub`.
- Slot, workspace, build/deploy/cleanup `job` и runtime state — зона `runtime-manager`.
- Доставка уведомлений, внешние каналы, callbacks и retry доставки — зона `interaction-hub`.
- UI/gateway и операторские экраны.

## Документы

| Документ | Путь |
|---|---|
| Требования | `product/requirements.md` |
| Дизайн | `architecture/design.md` |
| Модель данных | `architecture/data_model.md` |
| API-обзор | `architecture/api_contract.md` |
| Runbook | `ops/governance_manager_runbook.md` |
| Наблюдаемость | `ops/governance_manager_monitoring.md` |
| План поставки | `delivery/risk_governance_delivery.md` |

## Ключевые решения

- Домен не является опциональным и закладывается с самого начала.
- Сервис-владелец домена — отдельный `governance-manager`.
- `project-catalog` остаётся владельцем проектной политики, branch rules, release policy и release line; governance использует их через refs и авторитетные чтения.
- `interaction-hub` доставляет запросы Human gate, уведомления и callbacks, но не принимает risk/release decision.
- Владелец утверждает документы, переходы повышенного риска и релизы там, где это требуется риском, а не обязательно смотрит код построчно.
- Автоматизация релизов допустима только в рамках заданных правил риска и контрольных точек.

## Реализация

- Сервис-владелец: `services/internal/governance-manager`.
- Сервисный каркас: процесс, конфигурация, health/readiness/metrics, gRPC registration и безопасные backlog-handlers.
- Review signal refs intake: provider/agent/interaction evidence refs принимаются как safe refs owner-доменов, проходят access check и дедуплицируются по source fingerprint без копирования чужого state.
- Эксплуатационный контур: Dockerfile, Kubernetes manifests, migration Job, env/secret inventory, проверка готовности, runbook и monitoring готовы для первого backend deploy.
- gRPC-контракт: `proto/kodex/governance/v1/governance_manager.proto`.
- Сгенерированный Go-контракт: `proto/gen/go/kodex/governance/v1/**`.
- AsyncAPI: `specs/asyncapi/governance-manager.v1.yaml`.
- Сгенерированные Go-контракты событий: `libs/go/platformevents/governance/events.gen.go`.
- Миграции и постоянное хранилище: PostgreSQL-модель MVP-сущностей, repository и service-local outbox готовы.
- Статус контрактов и бэклог реализации: `delivery/risk_governance_delivery.md`.

## Карта Issue

- Доменная карта: `docs/delivery/issue-map/domains/risk-and-release-governance.md`.
