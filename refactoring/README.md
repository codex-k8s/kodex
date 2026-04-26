---
doc_id: REF-CK8S-0000
type: refactoring-index
title: "kodex — индекс программы рефакторинга"
status: active
owner_role: EM
created_at: 2026-04-21
updated_at: 2026-04-25
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

## Кратко
- Главный документ программы: `refactoring/task.md`.
- Документы в `refactoring/*.md`, кроме исторических справочных пакетов, являются рабочим источником правды для новой версии платформы.
- `refactoring/control-plane-refactor-agent-pack/**` используется только как справочный пакет по дисциплине декомпозиции, а не как целевая модель платформы.
- Старые документы, delivery-пакеты, сервисы, deploy, контракты и инструменты перенесены в `deprecated/**`.
- Новая реализация волн `7+` пишется только с нуля в новых сервисах и новых каталогах целевой архитектуры; материалы из `deprecated/**` остаются только справочным материалом до удаления.

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
   - `refactoring/14-risk-and-release-governance.md`
   - `refactoring/15-human-gates-and-evidence.md`
   - `refactoring/16-release-safety-observability-and-notifications.md`
   - `refactoring/17-console-and-ux-model.md`
   - `refactoring/18-workspaces-onboarding-and-operator-surfaces.md`
   - `refactoring/19-flow-role-prompt-and-settings-ux.md`
   - `refactoring/20-foundation-expansion-wave5-1.md`
   - `refactoring/21-runtime-deploy-and-bootstrap.md`
   - `refactoring/22-first-deployment-and-wave7-gate.md`
   - `refactoring/23-backlog-checkpoint-before-wave7.md`
   - `refactoring/24-pre-wave7-documentation-rebuild-plan.md`
3. `docs/design-guidelines/**` как инженерные ограничения реализации
4. Исторические материалы из `deprecated/**` и `refactoring/control-plane-refactor-agent-pack/**` как справочный материал

## Цели программы
- Переосмыслить платформу сверху вниз: от бизнес-модели и пользовательских сценариев до сервисных границ и реализации.
- Уйти от старой control-plane-центричной и label-центричной модели к платформе, управляемой `agent-manager` и работающей с provider-native сущностями.
- Не плодить искусственные рабочие сущности, которыми нельзя нормально управлять через GitHub/GitLab.
- Переписывать систему доменами и компактными PR, а не одним большим переносом всего репозитория.
- Вычищать устаревший код и документацию сразу после завершения соответствующего vertical slice.

## Канонический первый набор доменов
1. Access and accounts (доступ, организации, группы и внешние аккаунты)
2. Projects and repositories (проекты, репозитории, проектная документация и release policies)
3. Provider-native work items (рабочие сущности провайдера: `Issue`, `PR/MR`, комментарии, mentions, relationships, branches, tags)
4. Package platform (пакетная платформа: плагины, пакеты руководящей документации и каталоги)
5. Agent orchestration (агент-менеджер, flow, роли, шаблоны промптов и automation rules)
6. Runtime and fleet (runtime-платформа, контур серверов и кластеров, слоты)
7. Interaction hub (пользовательские взаимодействия, внешние каналы и уведомления)
8. Console and operations UX (консоль и операционные интерфейсы)
9. Billing and cost accounting (биллинг, учёт затрат и коммерческий контур)
10. Risk and release governance (управление рисками и релизами)
11. Knowledge lifecycle (руководящая и проектная документация, жизненный цикл знаний)

## Порядок волн
1. Волна 0: правила программы, doc governance, аудит backlog, compact PR policy
2. Волна 1: каноническая продуктовая модель
3. Волна 2: целевая архитектура и доменные границы
4. Волна 3: модель данных, provider integration, watermark и acceptance contract
5. Волна 4: risk/release governance
6. Волна 5: UX и frontend-консоль
7. Волна 5.1: расширение платформенного основания перед runtime-волной
8. Волна 6: runtime/deploy/bootstrap
9. Волна 6.1: информационная архитектура новой `docs/**`, доменные пакеты и раздельная карта Issue
10. Волна 6.2: сквозной продуктовый каркас платформы
11. Волна 6.3: сквозной архитектурный каркас платформы
12. Волна 6.4: пакет домена доступа перед кодовой wave 7
13. Волна 6.5: delivery, трассируемость и acceptance
14. Волна 6.6: ops и первый deploy gate
15. Волна 7: Access and accounts — доступ, организации, группы и внешние аккаунты
16. Волна 8: Projects and repositories — проекты, репозитории, релизные политики и источники проектной документации
17. Волна 9: Package platform — пакеты, каталоги, установка, версии и пакеты руководящей документации
18. Волна 10: Provider-native work items — `Issue`, `PR/MR`, комментарии, relationships, ветки и теги
19. Волна 11: Agent orchestration — `agent-manager`, flow, stage, role, шаблоны промптов и automation rules
20. Волна 12: Runtime and fleet — слоты, `run`, `job`, runtime manager, fleet manager, серверы и кластеры
21. Волна 13: Interaction hub — платформенный MCP, согласования, уведомления, внешняя обратная связь и каналы взаимодействия
22. Волна 14: Console and operations UX — реализация утверждённых операторских экранов и рабочих пространств
23. Волна 15: Risk and release governance — risk gates, release lines, branch rules и автоматизация по триггерам
24. Волна 16: Billing and cost accounting — затраты, распределение расходов, счета и коммерческий контур
25. Волна 17: Knowledge lifecycle — руководящая и проектная документация, самоулучшение и жизненный цикл знаний

## Состояние исторических справочных материалов
- `refactoring/control-plane-refactor-agent-pack/**` оставляем в репозитории как исторический пакет:
  - он полезен по темам `ownership`, `service split`, `database-per-service`, удаление устаревшего кода;
  - его список сервисов, приоритеты и старая целевая модель не считаются источником правды для новой программы.

## Следующие артефакты
- После backlog checkpoint обязательный порядок такой:
  - wave 6.1: структура `docs/platform/**`, `docs/domains/**`, `docs/catalogs/**`, `docs/delivery/**` и раздельная карта Issue;
  - wave 6.2-6.6: наполнение новой документации от сквозного уровня к домену доступа и первому deploy gate;
  - затем первый кодовый пакет wave 7 `Access and accounts`;
  - реализационные документы и PR по очереди `#599`-`#602`;
  - следующий backlog pass уже по итогам wave 7, а не вместо неё.
