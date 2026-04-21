---
doc_id: REF-CK8S-0000
type: refactoring-index
title: "kodex — индекс программы рефакторинга"
status: active
owner_role: EM
created_at: 2026-04-21
updated_at: 2026-04-21
related_issues: [470, 488]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-21-refactoring-wave0"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-21
---

# Индекс программы рефакторинга

## TL;DR
- Главный документ программы: `refactoring/task.md`.
- Документы в `refactoring/*.md`, кроме historical reference-пакетов, являются рабочим source of truth для новой версии платформы.
- `refactoring/control-plane-refactor-agent-pack/**` используется только как справочный пакет по дисциплине декомпозиции, а не как целевая модель платформы.
- Старые документы из `docs/**` считаются действующими только до момента, когда соответствующий домен будет переписан и явно заменён новой каноникой.

## Порядок приоритета внутри программы
1. `refactoring/task.md`
2. Канонические документы этой программы:
   - `refactoring/01-program-charter.md`
   - `refactoring/02-doc-governance.md`
   - `refactoring/03-domain-map.md`
   - `refactoring/04-backlog-audit.md`
   - `refactoring/05-delivery-and-risk-principles.md`
   - `refactoring/06-product-model.md`
   - `refactoring/07-glossary.md`
   - `refactoring/08-provider-native-work-model.md`
   - `refactoring/09-target-architecture.md`
   - `refactoring/10-service-boundaries.md`
   - `refactoring/11-data-and-state-model.md`
   - `refactoring/12-provider-integration-model.md`
   - `refactoring/13-artifact-contract-and-acceptance.md`
3. `docs/design-guidelines/**` как инженерные ограничения реализации
4. Исторические документы из `docs/**` и `refactoring/control-plane-refactor-agent-pack/**` как reference material

## Цели программы
- Переосмыслить платформу сверху вниз: от бизнес-модели и пользовательских сценариев до сервисных границ и реализации.
- Уйти от старой control-plane-центричной и label-центричной модели к manager-driven платформе, которая работает с provider-native сущностями.
- Не плодить искусственные рабочие сущности, которыми нельзя нормально управлять через GitHub/GitLab.
- Переписывать систему доменами и компактными PR, а не одним большим переносом всего репозитория.
- Вычищать устаревший код и документацию сразу после завершения соответствующего vertical slice.

## Канонический первый набор доменов
1. Доступ, администрирование и внешние аккаунты
2. Проекты и репозитории
3. Provider-native рабочие сущности (`Issue`, `PR/MR`, комментарии, mentions, relationships)
4. Агент-менеджер и оркестрация выполнения
5. Runtime-платформа и слоты
6. Контур пользовательских взаимодействий и внешних каналов
7. Консоль и операционные интерфейсы
8. Risk/release governance
9. Документация и knowledge lifecycle

## Порядок волн
1. Волна 0: правила программы, doc governance, аудит backlog, compact PR policy
2. Волна 1: каноническая продуктовая модель
3. Волна 2: целевая архитектура и доменные границы
4. Волна 3: модель данных, provider integration, watermark и acceptance contract
5. Волна 4: risk/release governance
6. Волна 5: UX и frontend-консоль
7. Волна 6: runtime/deploy/bootstrap
8. Волна 7+: implementation waves по одному домену за раз

## Состояние historical reference
- `refactoring/control-plane-refactor-agent-pack/**` оставляем в репозитории как исторический пакет:
  - он полезен по темам `ownership`, `service split`, `database-per-service`, `legacy removal`;
  - его список сервисов, приоритеты и старая целевая модель не считаются source of truth для новой программы.

## Следующие артефакты
- После выполнения backlog alignment pass следующими каноническими документами должны стать:
  - risk/release governance с матрицей обязательных human gates;
  - новая карта frontend и operator UX;
  - отдельный design-срез по внешним каналам взаимодействия и уведомлениям.
