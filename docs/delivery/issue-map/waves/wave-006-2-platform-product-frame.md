---
doc_id: MAP-CK8S-WAVE-006-2
type: issue-map
title: kodex — карта Issue wave 6.2
status: active
owner_role: KM
created_at: 2026-04-26
updated_at: 2026-04-26
---

# Карта Issue — wave 6.2

## TL;DR

Волновая карта документационной wave 6.2. После merge остаётся историческим снимком сквозного продуктового каркаса.

## Матрица

| Issue/PR | Документы | Домен | Статус | Примечание |
|---|---|---|---|---|
| PR текущей волны | `docs/platform/product/brief.md`, `docs/platform/product/constraints.md`, `docs/platform/product/product_model.md`, `docs/platform/product/glossary.md`, `docs/platform/product/requirements.md` | platform product | ready | Перенос сквозной продуктовой рамки из `refactoring/**` в активную структуру `docs/**`. |
| #599 | `docs/platform/product/product_model.md`, `docs/platform/product/requirements.md` | access-and-accounts | planned | Организации, группы и граф членства должны соответствовать сквозной продуктовой модели. |
| #600 | `docs/platform/product/constraints.md`, `docs/platform/product/requirements.md` | access-and-accounts | planned | Вход пользователя и allowlist должны соблюдать ограничения входа и регистрации. |
| #601 | `docs/platform/product/product_model.md`, `docs/platform/product/requirements.md` | access-and-accounts | planned | Внешние аккаунты являются частью сквозной модели, а детали будут в доменном пакете. |
| #602 | `docs/platform/product/requirements.md` | access-and-accounts | planned | Вычисление доступа должно учитывать организации, группы, explicit deny и audit. |

## Правила

- Эта карта не заменяет доменную карту `docs/delivery/issue-map/domains/access-and-accounts.md`.
- Следующие волны должны ссылаться на документы `docs/platform/product/**`, а не копировать требования целиком.
