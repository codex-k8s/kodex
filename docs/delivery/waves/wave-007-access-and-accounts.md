---
doc_id: DLV-CK8S-WAVE-007
type: delivery-plan
title: kodex — волна 7, доступ и аккаунты
status: active
owner_role: EM
created_at: 2026-04-25
updated_at: 2026-04-27
related_issues: [599, 600, 601, 602]
related_prs: []
related_docsets:
  - docs/domains/access-and-accounts/product/requirements.md
  - docs/domains/access-and-accounts/architecture/design.md
  - docs/domains/access-and-accounts/architecture/data_model.md
  - docs/domains/access-and-accounts/architecture/api_contract.md
  - docs/domains/access-and-accounts/delivery/wave7_access_and_accounts.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-26-wave6-4-access-domain"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-26
---

# Волна 7 — доступ, организации, группы и внешние аккаунты

## TL;DR

Волна 7 реализует первый новый сервис-владелец `access-manager`: организации, пользователи, группы, членство, allowlist, внешние аккаунты, вычисление доступа и аудит решений.

## Входные документы

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/access-and-accounts/product/requirements.md` |
| Дизайн домена | `docs/domains/access-and-accounts/architecture/design.md` |
| Модель данных | `docs/domains/access-and-accounts/architecture/data_model.md` |
| API-контракт | `docs/domains/access-and-accounts/architecture/api_contract.md` |
| Детальный план реализации | `docs/domains/access-and-accounts/delivery/wave7_access_and_accounts.md` |

## Структура работ

| Направление | Issue | Результат |
|---|---|---|
| Организации и членство | #599 | Модель организаций, групп, пользователей и членства с версиями агрегатов. |
| Вход и allowlist | #600 | Создание или связывание пользователя через SSO/OIDC, allowlist, статусы пользователя и операторский путь чтения. |
| Внешние аккаунты | #601 | Реестр поставщиков и внешних аккаунтов, ссылки на секреты, область применения и разрешённые операции. |
| Вычисление доступа | #602 | Каталог действий, правила доступа, явный запрет, объяснение решения и след аудита. |

## Разбиение на PR-срезы

Волна 7 ведётся не одним крупным PR, а серией малых PR-срезов. Каждый срез должен иметь узкую область изменения, собственные проверки и минимальную документационную синхронизацию.

| Срез | Область | Результат |
|---|---|---|
| 7.1 | PostgreSQL repository | Полный repository-слой для уже согласованных доменных операций без подключения транспорта и новых пользовательских сценариев. |
| 7.2 | gRPC transport | Регистрация `AccessManagerService`, обработчики для реализованных доменных сценариев, cast `proto <-> domain`, единая граница ошибок и interceptors. |
| 7.3 | Access checks | Полные `CheckAccess`, `ExplainAccess`, аудит решения доступа и тесты пограничных случаев. |
| 7.4 | Identity bootstrap | `BootstrapUserFromIdentity`, allowlist-path, статусы пользователя и тесты сценариев входа. |
| 7.5 | External accounts | Жизненный цикл поставщиков и внешних аккаунтов, проверка ссылок на секреты, `ResolveExternalAccountUsage`. |
| 7.6 | Outbox/events | Доставка outbox-событий, idempotency skeleton для будущих потребителей и сверка AsyncAPI. |
| 7.7 | Operational hardening | Health/readyz/metrics, tracing, config validation, Docker/deploy manifest и smoke-путь сервиса. |
| 7.8 | Delivery cleanup | Обновление delivery-карт, статусов #599-#602 и явное закрытие или перенос оставшегося бэклога. |

## Критерии начала

- Принят пакет доменной документации `access-and-accounts`.
- Создаётся новый сервис и новые каталоги реализации, старый код из `deprecated/**` не переносится как база.
- Все команды изменения проектируются с идемпотентностью и ожидаемой версией агрегата.
- Сырые секреты не проектируются как данные PostgreSQL.

## Критерии завершения

- `access-manager` владеет своим контуром данных, миграций, контрактов и событий.
- Другие сервисы могут проверять доступ только через контракт `access-manager`.
- Поставщики внешних аккаунтов, внешние аккаунты и действия доступа имеют отдельные модели, версии и события.
- `provider-hub` использует внешний аккаунт по разрешению `access-manager`, но не владеет его политикой.
- Все административные изменения и критичные решения доступа аудируются.

## Карты Issue

- Доменная карта: `docs/delivery/issue-map/domains/access-and-accounts.md`.
- Волновая карта: `docs/delivery/issue-map/waves/wave-007-access-and-accounts.md`.
