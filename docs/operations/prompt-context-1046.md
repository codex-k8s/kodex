---
id: OPS-PROMPT-CONTEXT-1046
title: Контекст preview и материализация инструкций
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-05
---

# Контекст preview и материализация инструкций

Источник: Issue #1046, Epic #1018, MVP-UI-30/34/35/36. Этот документ
фиксирует контракт; сам по себе он не подтверждает исполнимость owner paths.

## Цепочка и граница полномочий

Authenticated actor → existing HTTP prompt preview →
`PlatformQueryService.PreviewPromptTemplate` → owner-resolved Agent/Workflow
stage либо immutable Run/Session snapshot → общий renderer → безопасные
ordered sections и provenance → PWA editor. Полный текст требует прежнего
`prompt.full.view` и свежей интерактивной аутентификации. Query не создаёт
RuntimeRevision, Session, turn или событие; authoritative read path — тот же RPC.

`PromptPreviewContext` содержит только ссылки, выбранный этап, входные данные
и ожидаемые версии. Agent/Workflow принадлежность, template/runtime/environment
и attachment pins, effective capabilities, locale разрешает владелец состояния.
Поля запроса не выдают authority. `expected_context_digest` закрыто отклоняет
изменившийся контекст после preview. `PromptContextPin` не содержит credential,
Secret values, provider locator либо полномочия на исполнение.

## Служебный renderer

`prompt-service-v2` использует canonical JSON envelope. Пользовательский текст
и служебные блоки представлены отдельными escaped string values: пользователь
не может закрыть структуру envelope. PWA показывает человеку sections и их
provenance, а не имена служебных JSON-полей. Runtime и preview используют одну
материализацию. Старые snapshots без service revision продолжают исполняться
прежним renderer и не переписываются.

Явная вставка имеет форму `{{ slot "PURPOSE" }}`. Закрытый реестр содержит
WORKFLOW, STAGE, PURPOSE, EXPECTED_RESULT, INPUT, CONSTRAINTS,
EFFECTIVE_CAPABILITIES, FILES, TOOLS, INTEGRATIONS и RUNTIME_CHANGES.
Workflow/STAGE/EXPECTED_RESULT применимы к этапу; RUNTIME_CHANGES — к продолжению.
Остальные обязательны для каждого вида запуска. Синтаксис slot допускается
только отдельным действием с literal argument, без assignment, pipeline или
использования в condition. Вызов в невыполненной ветке не считается вставкой;
повтор выполненного slot не дублирует его. Пропущенные блоки добавляются после
пользовательской части в порядке реестра. Locale закрыт значениями en/ru.

Отдельные digests связывают пользовательский шаблон, service template,
variable snapshot и итоговый prompt. Фактические insertion points сохраняют
typed slot, источник USER_TEMPLATE/PLATFORM и монотонную position.

## Продолжение Session

Согласован отдельный published PROMPT_TEMPLATE через существующие
create/save/validate/publish/history и RebindPromptTemplate. Специализированный
consumer AGENT_CONTINUATION принадлежит точному Agent; system assistant требует
organization.manage. Binding не заменяет основной шаблон Agent. Migration639
зарезервирована для durable notice и связанной схемы. Owner implementation,
exact retry, notice previous/current RuntimeRevision pins и end-to-end checks
должны быть завершены до заявления готовности этого сценария.

## Проверки

Context7: Go standard library `/websites/pkg_go_dev_go1_25_3`,
text/template FuncMap и encoding/json Marshal: функция с error закрывает
execution; JSON Marshal экранирует значения. Renderer tests проверяют false
branch, повторную вставку, недопустимый pipeline/assignment, неизвестный scope,
подмену effective capabilities, escaping, digest/locale и legacy snapshots.
Producer/RPC/HTTP/PWA lifecycle и staging отмечаются отдельно в exact SHA ledger.
