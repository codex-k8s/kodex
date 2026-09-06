---
id: OPS-HTTP-PROMPT-CONTEXT-1045
title: HTTP каталог и просмотр точного контекста Prompt
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

Источник: #1045/#1046, MVP-UI-30/34/35/36 и
`prompt-context-1046.md`. Владелец materialization — control-plane; gateway
не строит prompt и не выводит grants из выбранного Agent или payload.

Prerequisite: полный CP89 плюс точный scoped enum/caster/catalog correction
из CP70 `60af9e6f4b925932977cf5c55cf3f3c528cebf17`. Причины
PERMISSION_REQUIRED/CAPABILITY_REQUIRED больше не превращаются в UNSPECIFIED,
authoritative unavailable всегда даёт available=false. Остальные изменения
70, включая обязательные Instructions/Prompt impact plans, потребляются
следующим отдельным checkpoint в том же HTTP unit.

| Сценарий и проверенный actor | HTTP → существующий RPC | Owner и consumer |
| --- | --- | --- |
| Выбор контекста Agent/Workflow stage/continuation | POST prompt-templates/catalog/query → ListTemplateVariables | Exact target/context/input/OCC; authoritative variable availability и contextPin, cursor/total после eligibility |
| Каталог без пользовательских input/task | Существующие GET catalog/template-variables → ListTemplateVariables | Прежний безопасный metadata path сохранён |
| Синтаксис и выбранный контекст | POST prompt-templates/validation → ValidatePromptTemplate | Typed diagnostics и contextPin; validation не выдаёт authority публикации |
| Просмотр до запуска | POST prompt-templates/preview → PreviewPromptTemplate | AGENT/WORKFLOW_STAGE без fake Run/Turn refs; ordered sections, actual executed slots, version/digest/locale |
| Продолжение Session | Тот же Preview с SESSION_CONTINUATION | Owner-derived prior runtime и prospective diff; current revision/turn/attempt ещё отсутствуют |
| Сохранение declared scope | Create/SavePromptTemplateDraft с promptScope | Owner присваивает scope snapshot без task/input values; Validate/Publish повторяют текущую зависимость и права |
| Привязка continuation template | Existing managed consumer AGENT_CONTINUATION | Специализированная owner validation; не generic capability grant |

POST catalog/query использует существующий read RPC и CSRF как Preview;
новая policy/RPC не вводится. Пользовательские task/input находятся только в
body, не в URL. Signed actor/tenant и RPC proof остаются единственным
источником transport authority. Полный materialized prompt требует отдельной
проверки prompt.full.view и fresh authentication у владельца.

Gateway проверяет context tuple, safe53 OCC, UTF-8 и bounded input. Output
проверяет enum, refs/digests, уникальность и последовательность executed slots,
раздельное происхождение user section и platform block. Явно использованный
slot может стоять между user sections; только неиспользованные platform
blocks образуют завершающую часть. Повтор slot не создаёт дубликат блока.
Safe preview сохраняет producer redaction; HTTP не заменяет его полной
материализацией и не нормализует пользовательский текст как enum.

Declared scope принимает AGENT/WORKFLOW_STAGE и templateKind
INSTRUCTIONS/CONTINUATION. Первый capture может не иметь expectedContextDigest;
в этом случае exact snapshot назначает owner. Task/input-dependent preview pin
не подменяется scope pin. Response scope связывает фактически сохранённую
revision; request не может назначить owner snapshot или будущие runtime refs.
Scope запрещён у других managed kinds.

Runtime diff имеет закрытый component/action registry. Prospective diff не
содержит currentRevisionRef/turnRef/attempt; уже материализованный diff требует
полный tuple. Descriptor не содержит произвольных metadata, credential или
locator. Response используется для чтения notice, не как authority команды.

Prepare/read не создают события; повторное чтение идёт через те же protected
RPC. Draft/Validate/Publish/Bind и continuation сохраняют собственные atomic
owner audit/receipts/events по producer matrix. Idempotency и If-Match не
заменяют tenant/actor eligibility. Existing immutable RuntimeRevision не
переписываются.

Проверки HTTP/SDK и exact ledger фиксируются в PR1066. Browser, integrated
protected path и deployment проверяются отдельно. Секреты не раскрыты.
