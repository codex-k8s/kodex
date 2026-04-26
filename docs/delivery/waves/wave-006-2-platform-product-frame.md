---
doc_id: DLV-CK8S-WAVE-006-2
type: delivery-plan
title: kodex — wave 6.2, сквозной продуктовый каркас платформы
status: active
owner_role: PM
created_at: 2026-04-26
updated_at: 2026-04-26
related_issues: [599, 600, 601, 602]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-26-wave6-2-platform-product-frame"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-26
---

# Wave 6.2 — сквозной продуктовый каркас платформы

## Кратко

- Что поставляем: активный продуктовый пакет `docs/platform/product/**`.
- Когда: перед wave 6.3, 6.4 и кодовой wave 7.
- Главный риск: пропустить enterprise-заделы и снова сузить платформу до одного проекта, одного репозитория и одного ручного контура.
- Что нужно от Owner: принять PR как согласование продуктового каркаса.

## Входные артефакты

| Артефакт | Путь |
|---|---|
| Главный мандат | `refactoring/task.md` |
| Индекс программы | `refactoring/README.md` |
| Продуктовая модель | `refactoring/06-product-model.md` |
| Provider-first модель | `refactoring/08-provider-native-work-model.md` |
| Расширение основания | `refactoring/20-foundation-expansion-wave5-1.md` |
| План пересборки документации | `refactoring/24-pre-wave7-documentation-rebuild-plan.md` |

## Объём

Создать и связать:
- `docs/platform/product/brief.md`;
- `docs/platform/product/constraints.md`;
- `docs/platform/product/product_model.md`;
- `docs/platform/product/glossary.md`;
- `docs/platform/product/requirements.md`;
- волновую карту `docs/delivery/issue-map/waves/wave-006-2-platform-product-frame.md`.

## Критерии готовности

- Provider-first модель зафиксирована в активной продуктовой документации.
- Инициатива описана как `Issue` типа `initiative`.
- Модель `flow`, этапов, ролей, привязок ролей к этапам и шаблонов промптов отражена на продуктовом уровне.
- Организации, группы, внешние аккаунты, пакетная платформа, руководящая документация, runtime/fleet, биллинг, релизная политика и автоматизация указаны как обязательные контуры.
- Новые документы добавлены в индексы.
- Package submodule обновлены после merge пакетных PR.

## Не входит

- Детальная архитектура сервисов и данных.
- Детализация домена доступа.
- Кодовая реализация.
- Перенос старых документов из `deprecated/**`.

## Апрув

- request_id: `owner-2026-04-26-wave6-2-platform-product-frame`
- Решение: approved
- Комментарий: merge PR считается фактом согласования wave 6.2.
