---
id: OPS-EMAIL-1371
title: Приёмка почтовой операции настоящим Agent через MCP
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-09
---

# Область и контракт

Issues #1371, #1031/#1223; MVP-UI-42, OPS-EMAIL-1037. Публичный entrypoint
`tools/dev/email-agent-acceptance.mjs` выполняет одну конкретную операцию через
настоящий Agent. Он не вызывает SMTP/IMAP/POP3 или adapter напрямую.
Поддерживаются 21 capability текущего email package; наличие capability не
означает, что операция разрешена конкретной mailbox policy или прошла live.

| Переход | Actor, boundary и команда | Idempotency, состояние и результат |
| --- | --- | --- |
| agent | Легитимная cookie/CSRF session → BFF POST `/api/v1/projects/{ref}/agents` → специализированный CP CreateAgent; owner/tenant назначает CP | Durable fsync INTENT до POST, прежний ACK читается без повторного создания; UNKNOWN блокирует повтор |
| plan | Те же защищённые GET project/Agent/runtime configuration/account-specific catalog/account LAUNCH usage/connection/mailbox/effective capabilities | Exact versions/digests; без business mutation, содержимое input только в private profile |
| launch | BFF POST `/api/v1/runs` → CP CreateRun → immutable RuntimeRevision/turn/attempt → runtime-controller → настоящий runner/provider | Один зафиксированный Run intent; owner idempotency key из журнала. Lost ACK/409 не запускают второй POST. План сверяется заново до INTENT; owner остаётся окончательным authority |
| MCP | Runner lease token и execution headers → exact pinned grant → CP ResolveIntegrationInvocation/GetIntegrationInvocation | CP владеет invocation, Gate, lease/replay и effect; исходный call ID сохраняет прежний idempotency scope |
| terminal | Runtime-controller RecordRunToolCall с lease/fence/generation и grant → CP проверяет worker/lease/grant и атомарно сохраняет ToolCall/audit/RunEvent | Существующий TOOL_CALL_RECORDED.safeResult содержит закрытую JSON-проекцию ниже; event/read path остаются прежними |
| capture | BFF GET Run/events/runtime-revision-diff/graph → CP owner reads | Exact run/session/attempt/node/turn, config/policy/model, grant/connection/capability и digest реально переданного MCP input; более одного вызова закрыто отклоняется |
| receipt | BFF GET `/api/v1/integration-invocations/{ref}/email-effect-receipt` → CP owner receipt | Exact invocation/project/connection/mailbox/configuration revision. Повторяется только чтение; reconcile/new effect отсутствуют |

MCP terminal `structuredContent` и сериализованный text content сохраняют
прежние `ok`, `result` либо `error_code`, `owner_decision_required`; добавляется
`invocationRef`, полученный исключительно от CP. Поле не выдаёт полномочий.
Ссылка из ответа модели не используется как evidence.

`TOOL_CALL_RECORDED.safeResult` для terminal integration результата:

```json
{"version":1,"invocationRef":"inv_example","state":"SUCCEEDED","inputSHA256":"<64 lowercase hex>"}
```

Закрытые состояния: SUCCEEDED, FAILED, REJECTED, CANCELLED, UNKNOWN_OUTCOME.
Projection назначается из внутреннего типизированного CP результата, а не
произвольной map инструмента. В ней нет provider result, input, адресов,
subject/body или credential. Digest соответствует Go encoding/json плоского
input; это не email semantic digest receipt. Tool error без terminal readback
оставляет прежний `TOOL_UNAVAILABLE`, а не выдуманный invocationRef.
Proto/OpenAPI/AsyncAPI ABI и owner SQL не меняются.

# Подготовка оператора

Нужны fresh legitimate limited storageState, актуальный CAPTURED component
manifest с валидным semantic digest и подтверждённой совместимостью, clean
exact checkout. Все private inputs 0600, directory 0700, без symlink/hardlink.
Root отдельно выполняет штатные component-manifest capture/verify; driver
проверяет формат и digest сохранённого capture, а не Kubernetes live state.
Manifest, source и bytes profile закрепляются immutable HEADER. Смена
session разрешена в рамках штатного client; смена source/profile/manifest
того же журнала закрыто отклоняется, старые evidence не переписываются.

Private profile (только обезличенный пример):

```json
{
  "version": 1,
  "prefix": "mvp1371-email1",
  "projectRef": "prj_example",
  "connectionRef": "int_example",
  "configurationRef": "mailcfg_example",
  "mailboxRevisionRef": "mailrev_example",
  "accountRef": "pacc_example",
  "model": "gpt-5.6-sol",
  "defaultReasoningEffort": "low",
  "capabilityKey": "email.message.send",
  "input": {"to":"qa@example.invalid","subject":"Приёмка","body_text":"Проверочный текст"}
}
```

Модель/effort выбираются по реальному каталогу выданного account, не по примеру.
Agent создаётся отдельной фазой. Затем оператор обычными UI-командами назначает
этому Agent опубликованное окружение/RoleImage, FIXED account/model/effort и
grant **одной** выбранной capability через существующие candidate selectors.
Mailbox создаётся/публикуется прежним `email-mailbox-acceptance.mjs`; grant scope
и HUMAN_EACH_EFFECT не расширяются. Driver не создаёт account defaults,
не меняет credentials/mailbox policy и не настраивает окружение автоматически.
Для уже выделенного собственного Agent допустим `agentRef` в исходном profile;
тогда фаза agent запрещена. Не добавлять ref задним числом в HEADER profile.

Общий argv сохраняется для всех фаз:

```bash
EMAIL_QA_ARGS=(--origin https://control.kodex.works
  --storage-state "$EMAIL_QA_SESSION" --profile "$EMAIL_QA_PROFILE"
  --serving-manifest "$EMAIL_QA_MANIFEST" --state "$EMAIL_QA_JOURNAL"
  --timeout-ms 60000)
node tools/dev/email-agent-acceptance.mjs agent "${EMAIL_QA_ARGS[@]}" \
  --confirm CREATE-STAGING-EMAIL-AGENT
# Обычные UI binding/grant/configuration для полученного собственного Agent.
node tools/dev/email-agent-acceptance.mjs plan "${EMAIL_QA_ARGS[@]}"
# Только после явного owner GO на provider и конкретную почтовую операцию:
node tools/dev/email-agent-acceptance.mjs launch "${EMAIL_QA_ARGS[@]}" \
  --confirm START-ONE-STAGING-EMAIL-RUN
node tools/dev/email-agent-acceptance.mjs capture "${EMAIL_QA_ARGS[@]}"
node tools/dev/email-agent-acceptance.mjs receipt "${EMAIL_QA_ARGS[@]}"
```

Plan читает `GET /api/v1/integration-grant-candidates/capabilities` с exact
connectionRef/projectRef/recipientKind=AGENT/recipientRef. Schema берётся из
единственного выбранного `candidate.capability`, а не optional полей grant.
Проверяются page/item contextDigest, connectionVersion, definitionVersion/digest,
projectVersion, recipientVersion и currentGrantRef/version. До 10 страниц по 100,
без duplicate keys/cursors или смены snapshot; target должен быть READY/grantable.
Candidate schema digest сверяется по bytes, затем проверяется private input.
Локальный shipped каталог ограничивает поддержанный профиль оснастки, но не
заменяет live schema/authority. Отсутствие live schema закрыто отклоняется.
Plan закрепляет projectVersion и candidate context/pins digests; перед Run INTENT
launch повторяет все GET и сравнивает полный план. Этот read не выдаёт grant и
не выполняет provider/HEALTH/mail mutation.

Plan проверяет pinned package/schema, primitive input constraints и narrowing;
семантику mailbox/UID/recipient, capacity и актуальные полномочия окончательно
проверяет CP/bridge. Plan не резервирует account capacity и не обещает отсутствие
гонки между GET и owner mutation. Published overlay закрепляется digest; поле
`defaultReasoningEffort` означает default кандидата, не подменяет effective overlay.

# Gate, неизвестный результат и завершение

- WAITING_HUMAN/PENDING — только bounded GET checkpoint, не PASS. Оператор
  отдельно решает существующий Gate через UI; capture можно повторить без Run POST.
- UNKNOWN без Run ACK запрещает повтор launch и новый intent. Нужен owner
  authoritative readback; generic recovery/new-prefix retry отсутствует.
- Прежний ACK при повторном launch возвращается без HTTP POST; это не повтор
  выполнения и не доказательство успеха. Новый Run/attempt не создаётся автоматически.
- FAILED/REJECTED/CANCELLED/UNKNOWN_OUTCOME сохраняются (exit 2). Capture на
  старой safeResult строке, malformed ref, чужом scope, изменённом input,
  повторном вызове или новом attempt закрыто падает (exit 1).
- READ_ONLY не создаёт effect receipt: READ_COMPLETED отдельно отмечает
  contentVerification=NOT_RUN. Это не проверка найденных писем/attachments.
- PASS требует terminal SUCCEEDED Run, SUCCEEDED invocation и owner
  EFFECT_CONFIRMED receipt. SMTP accepted не означает delivered; delivery
  всегда NOT_PROVEN. Проверка получения письма отдельной разрешённой read
  операцией и сравнение private content остаются самостоятельными вариантами.
- Task просит ровно один MCP вызов, но это не механизм принудительного ограничения
  модели. Capture обнаруживает повторные вызовы; HUMAN_EACH_EFFECT/owner scopes
  продолжают защищать write effects. Не выдавать обнаружение за их предотвращение.
- Cancel/revoke/restart не обходятся: journal сохраняется, capture читает тот же
  attempt; при смене attempt — отказ. Отмена через штатный UI требует отдельного
  решения оператора. Cleanup — отдельная разрешённая операция над точным fixture
  UID, не автоматический EXPUNGE/удаление чужих писем. Не отправлять повторно при
  неизвестном SMTP outcome и не очищать fixture до доказанного receipt/readback.

# Совместимость и проверка

Меняется только application runtime-controller. Новый writer совместим с
прежними CP/API/PWA/runner: JSON MCP additive, SafeResult уже opaque string,
обычный UI отображает безопасную строку. Новая оснастка принимает новый закрытый
результат; старый runtime не даёт ложный PASS. Перед launch раскатывается именно
runtime-controller и фиксируется новый serving capture; runner image, schema,
security sidecars и signer generation этим изменением не обновляются.
Rollback application допустим с прекращением новых QA launches: прежний
runtime выполняет прежний протокол, но новая QA-проекция для новых вызовов
недоступна. Исторические events/receipts не изменяются.

`make test-email-agent-acceptance` запускает настоящий public CLI на isolated
clean fixture checkout с bounded synthetic HTTP transport. Go callback tests
проходят настоящий callTool/MCP serialization/RecordRunToolCall projection,
проверяют пять terminal states, exact lease/grant и запрет sensitive projection.
Это локальные protocol fixtures, не live CP/PG/provider acceptance.

Проверены актуальные MCP 2025-06-18 tools/schema через Context7:
https://modelcontextprotocol.io/specification/2025-06-18/server/tools .
Live provider/mail effects, реальная Gate/revoke/restart/cleanup матрица и
результат доставки остаются NOT RUN до отдельного staging GO и actual evidence.
