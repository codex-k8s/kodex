---
id: OPS-DOC-SELFDEV-001
title: Самонастройка и разработка Kodex средствами платформы
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-04
---

# Цель и источники

Полностью выполнить согласованное владельцем задание
`/home/s/projects/kodex/.agents/full-qa-task.md` (65 разделов), а не заменять
его обходом экранов. После самонастройки системного и проектного помощников
внутренняя ИИ-команда разрабатывает сам Kodex по реальной GitHub Issue.
Результат — отдельный PR, `READY_FOR_HUMAN_REVIEW`, без merge.

Исходный `main`: `d43bd605ec7b41335ec038a84a896b1ab5b0d189`, PR #1790 уже слит.
Связанное Issue: https://github.com/codex-k8s/kodex/issues/1797.
Ветка: `kodex-agent/issue-1797-self-development-bootstrap`.
Bootstrap PR: https://github.com/codex-k8s/kodex/pull/1798 (Draft).
Все новые платформенные изменения — в одном сквозном bootstrap PR как явно
разрешённое владельцем исключение из правила одного deployable unit.
Данный документ фиксирует дополнения владельца; полный исходный сценарий
сохраняется в `.agents/full-qa-task.md` и выполняется целиком.

## Решения владельца и режим

- Bootstrap PR разрешено сливать автономно после фактических проверок без
  нового owner gate; запрещено обходить GitHub protection/checks.
- Доработки внешнего host-агента не отправлять на отдельный review: быстрые
  адресные unit-тесты, необходимые сборка/codegen, затем живой сквозной QA с
  исправлением найденного. Незапущенные suites — NOT RUN, не PASS.
- Внутренние Documentation/Security/Lexical reviews — обязательная часть
  проверяемого Workflow и не отменены этим исключением.
- До живого QA разрешены основной агент и до пяти субагентов
  `gpt-6.1-sol`, reasoning `high`, в пределах фактического лимита инструментов.
  Разделять владение файлами; общий контракт интегрирует основной агент.
  В финальном Workflow host не подменяет сотрудников платформы.
- Рабочий клон `/home/s/projects/kodex` примонтирован в разрешённый локальный
  кластер; изменения проверять на hot reload. KUBECONFIG
  `/home/s/.kube/config`, context `k3d-kodex`; staging/production не затрагивать.
- Каждые 10–15 вызовов инструментов или 5–10 минут получать список вкладок
  Chrome MCP; дополнительно обновлять рабочую вкладку раз в 5 минут,
  предварительно сохраняя ввод. Чужие вкладки не закрывать.
- При согласованном bootstrap/dogfooding разрешены реальные ИИ-запуски и
  предусмотренные сценарием GitHub effects. STT/device-code не тестировать.
- Никакого legacy, двойных источников состояния и ручных обходов платформы.
  Applied migrations не менять, новые изменения forward-only.
- Секреты не показывать в prompts, аргументах, URL, логах, screenshots,
  документации и Git. Единый runtime источник host-секретов —
  `/home/s/.codex/agent-secrets.env`, regular file текущего владельца, 0600.
  Владелец явно разрешил разово получить из `/home/s/projects/kodex/.env`
  только `CODEX_GITHUB_AGENT_INTEGRATION_TOKEN` и
  `CODEX_GITHUB_AGENT_GIT_TOKEN`; не исполнять `.env` и не загружать остальные
  project variables. GIT_BOT_PAT — разработка/PR; GIT_OWNER_PAT — Issue и явно
  разрешённые owner операции; owner token агентам платформы не выдаётся.

## План с доказательствами

- [x] 1. Создать связанное Issue, ветку от свежего main и один Draft bootstrap
  PR; фиксировать результаты PASS/FAIL/NOT RUN/BLOCKED на точном SHA.
- [ ] 2. Полные управляемые MCP/tool profiles системного помощника,
  проектного помощника и каждого сотрудника; host Context7 reference,
  immutable RuntimeRevision, scoped Secret binding, exact network/readiness.
  Ключ Context7 доступен только доверенному MCP adapter/server, не shell агента.
- [ ] 3. Настраиваемая ApprovalPolicy grant: package default/allowed policies,
  durable/versioned/audited selected policy, CP/gateway/adapter/runtime pins.
  Collaborative GitHub writes допускают NONE только в разрешённом реестре;
  destructive операции не становятся автономными.
- [ ] 4. Сессия для её владельца отображается как переписка: пользовательские
  сообщения, публикуемые промежуточные сообщения и итоговые ответы агента.
  В общей хронологии показываются вызовы инструментов, название действия,
  статус и раскрываемые безопасные детали/результат, как в интерфейсе Codex.
  Работает для помощников, сотрудников, процессов и дочерних сессий; автора,
  session/turn/attempt нельзя перепутать. Realtime/rejoin/reload сохраняют
  порядок, сообщения и дедупликацию; длинный вывод сворачивается, прокрутка
  не прыгает. Секреты, сырые bearer headers и скрытые рассуждения не выводятся.
- [ ] 5. Безопасный observability/read path фактически materialized prompt:
  instructions, template variables, integrations, identity, tools/MCP,
  files, user/task input с harmless marker, model/reasoning и exact pins.
- [ ] 6. Общий admitted/promoted образ kodex-selfdev со всем требуемым
  инструментарием; отдельные execution workspaces, без общего mutable PVC.
- [ ] 7. System Assistant сам настраивает себя typed plan; подтверждение,
  публикация, Context7/web/GitHub read и prompt proof реальных ходов.
- [ ] 8. System Assistant создаёт Kodex | Dev и отдельного Project Assistant;
  authoritative ownership/version/audit readback; project isolation,
  Context7/repository/network/runtime/prompt proof.
- [ ] 9. Project Assistant создаёт шесть сотрудников (Manager, Architect,
  Developer, Documentation Reviewer, Security Reviewer, Lexical Guardian),
  selfdev-write/selfdev-review, Project Files/Secrets, GitHub connection и
  least-privilege grants. Raw git push token только Developer.
- [ ] 10. Проверить реальные тестовые ходы каждой роли, template validate/
  preview/publish/materialization, scoped grants, NONE writes, оба Human Gate
  режима, delegation и handoff через файлы/артефакты.
- [ ] 11. SOFTWARE_CHANGE: Manager → Architect → Developer → параллельные
  Documentation/Security/Lexical reviews → fixes/re-review → final Manager.
  Проверить небольшой disposable delegated run до настоящей Issue.
- [ ] 12. При bootstrap acceptance зафиксировать и автономно слить bootstrap
  PR, обновить стенд на свежий main и повторно сверить созданные ресурсы,
  migrations/source/Pod/image/runtime/realtime и prompt pins.
- [ ] 13. Manager выбирает #1796, если актуальна и имеет поддерживаемый
  upstream API; иначе следующую подходящую реальную Issue. Не scraping,
  не private undocumented endpoint и не выдуманные usage/credits.
- [ ] 14. Выполнить полный реальный Workflow силами команды Kodex; host
  проверяет каждый значимый transition и исправляет дефекты платформы,
  но не пишет финальную задачу вместо Developer и не подменяет reviewers.
- [ ] 15. Internal reviews/fixes/responses на exact SHA, final-readiness.md,
  финальный PR READY_FOR_HUMAN_REVIEW и отчёт по разделу 64 исходного задания.
  Этот PR не merge, не auto-merge, не approve от имени владельца.

## Карта новых пользовательских сценариев

| Сценарий | Authority и владелец состояния | Consumer / проверка |
| --- | --- | --- |
| MCP profile publish → turn | Проверенный actor/scope, CP immutable revision и secret metadata; trusted adapter получает только exact binding | Runner startup и штатный MCP call, Console/Network/runtime proof |
| Grant policy select → GitHub effect | Package allowed set и exact selected grant snapshot; CP owner transaction | Gateway/adapter membership check, grant pin, NONE/Human Gate negative cases |
| Runtime message/tool → transcript | Callback workload/session/turn/attempt, CP persisted event sequence; session eligibility из серверного read path | Scoped WebSocket и history/rejoin, owner transcript без secret leakage |
| Typed plan self-config → следующий ход | Owner confirmation, OCC/idempotency, immutable опубликованные pins | Runtime readback, actual prompt/tool/network proof; stale plan закрыто отклоняется |

Lifecycle cancel/delete/retry/terminal, deduplication и возможные частичные
переходы детализируются перед изменением соответствующих контрактов. Нельзя
считать зелёный Pod, скриншот или unit-тест доказательством живого Workflow.

## Журнал

04.10.2026: задания прочитаны, уточнения владельца внесены; код ещё не изменён,
новый живой QA не запускался. Два явно разрешённых ключа в проектном `.env`
присутствуют и перенесены в единый private source с readback 0600; значения
не выводились. Рабочая вкладка Chrome MCP доступна.

04.10.2026, bootstrap checkpoint `2c103867cf9b14bbcfd8ead0555f1f1dfcde691a`:
PASS — Issue #1797, ветка от подтверждённого main, Draft PR #1798 и совпадающий
GitHub head SHA. Реализация и новый живой QA пока NOT RUN.

Подтверждён разрыв переписки: runner принимает `commentary`, но не передаёт
его владельцу состояния; native tool calls сохраняются пакетом после хода,
а MCP — после завершения вызова. Исправление должно сохранять исходную
хронологию, точные session/turn/attempt и состояния вызова. Служебный summary
ограничен 2000 символами и не заменяет полный bounded текст сообщения.
