---
doc_id: DLV-CK8S-WAVE-006-4
type: delivery-plan
title: kodex — волна 6.4, пакет домена доступа
status: active
owner_role: EM
created_at: 2026-04-26
updated_at: 2026-04-26
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

# Волна 6.4 — пакет домена доступа

## TL;DR

Волна создаёт минимальный доменный пакет `access-and-accounts`, который нужен перед реализацией первого кодового домена. Результат — согласованные требования, дизайн, модель данных, API-контракт и план реализации.

## Цель

Подготовить домен доступа так, чтобы реализация `access-manager` не начиналась с неявных решений по организациям, группам, пользователям, allowlist, внешним аккаунтам и вычислению прав.

## Объём

| Артефакт | Путь | Назначение |
|---|---|---|
| Требования домена | `docs/domains/access-and-accounts/product/requirements.md` | Продуктовый объём и критерии успеха. |
| Дизайн домена | `docs/domains/access-and-accounts/architecture/design.md` | Граница сервиса, основные потоки и события. |
| Модель данных | `docs/domains/access-and-accounts/architecture/data_model.md` | Агрегаты, связи, инварианты и хранение данных. |
| API-контракт | `docs/domains/access-and-accounts/architecture/api_contract.md` | Команды, чтения, ошибки и события. |
| План реализации | `docs/domains/access-and-accounts/delivery/wave7_access_and_accounts.md` | Порядок кодовой реализации и критерии готовности. |
| План волны 7 | `docs/delivery/waves/wave-007-access-and-accounts.md` | Компактный верхнеуровневый план реализации. |

## Критерии готовности

- Домен `access-and-accounts` имеет явные документы в `docs/domains/access-and-accounts/**`.
- Граница между `access-manager` и `provider-hub` согласована: внешний аккаунт и политика его применения принадлежат `access-manager`, операции провайдера и лимиты — `provider-hub`.
- Карты Issue обновлены и не требуют единого растущего файла.
- В продуктовых и архитектурных документах нет ссылок на рабочие ветки, PR, review threads и временные задачи в теле документа.
- Кодовая реализация может стартовать с нового сервиса, без переноса старого кода из `deprecated/**`.

## Что не входит

- Реализация сервиса.
- Создание proto, миграций и Go-кода.
- Доработка пользовательского интерфейса.
- Полная модель биллинга, релизной политики и серверов.

## Апрув

- request_id: `owner-2026-04-26-wave6-4-access-domain`
- Решение: approved
- Комментарий: пакет домена доступа считается согласованным после merge PR.
