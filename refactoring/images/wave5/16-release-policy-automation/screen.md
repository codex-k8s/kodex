# Релизы и автоматизация

Статус: `approved`

## Назначение
Экран управления релизными линиями, правилами веток, автоматическими расписаниями/триггерами и risk gates.

Экран закрепляет, что релизная политика принадлежит платформе: провайдеры репозиториев синхронизируются с ней, но не являются единственным источником процесса. Автоматизация создаёт `Issue` или запускает `flow` с аудитом, а рискованные операции проходят через `Risk gate`.

## Главные сущности
- release line
- release candidate
- branch rule
- rollout policy
- automation rule
- trigger event
- schedule
- Risk gate
- release Issue

## Основные блоки
- верхняя лента summary-card по release lines, release candidates, автоматизациям и risk gates
- вкладочная поверхность релизных линий, правил веток, расписаний/триггеров и risk gates
- таблица релизных линий, branch rules, automation rules или risk gates
- инспектор выбранной сущности
- блоки timeline, policy summary, sync jobs, recent trigger events и pending decisions

## Семантика вкладок
- `Релизные линии` — release lines, release candidates, rollout policy, связанный `Issue`, ожидающие `PR/MR`, QA и postdeploy;
- `Правила веток` — branch rules платформы, синхронизация с GitHub/GitLab, required checks и нарушения;
- `Расписания и триггеры` — cron, webhook, provider events и alerts, которые создают `Issue` или запускают `flow`;
- `Risk gates` — риск-решения перед merge, release, deploy, миграциями, секретами, биллингом и установкой непроверенных пакетов.

## Ключевые действия
- создать release line
- запустить rollout
- поставить rollout на паузу
- запустить rollback
- синхронизировать branch rules
- создать automation rule
- отправить тестовый trigger payload
- одобрить, запросить изменения или эскалировать `Risk gate`

## Источники данных
- платформа: release lines, branch policies, automation rules, trigger deliveries, risk gates, audit log
- провайдеры репозиториев: branch protection state, tags, `PR/MR`, provider events
- Kubernetes/platform jobs: результаты проверок, rollout jobs, postdeploy checks
- внешние системы мониторинга: alerts, которые могут создавать `Issue` или запускать diagnostic flow

## Что намеренно не показывается
- GitHub Actions workflow как основной механизм build/deploy
- raw CI YAML и shell deploy commands
- секреты и provider tokens
- скрытые запуски без `Issue`, `flow` или audit event
- общий inbox approvals вместо профильного risk-контекста

## Макеты
- [Основной макет](screen.png) — полноэкранный экран релизов с активной вкладкой `Релизные линии`.
- [Вкладка `Релизные линии`](tab-release-lines.png) — release lines, release candidate, связанный `Issue`, `PR/MR`, проверки и rollout actions.
- [Вкладка `Правила веток`](tab-branch-rules.png) — branch rules, provider sync, required checks, required reviews и нарушения.
- [Вкладка `Расписания и триггеры`](tab-schedules-triggers.png) — cron, webhook, provider events, alerts, действия create Issue/start flow и защита от петель.
- [Вкладка `Risk gates`](tab-risk-gates.png) — ожидающие риск-решения, профильные роли, evidence checklist и действия approve/request changes/escalate.

## Открытые вопросы
- нет
