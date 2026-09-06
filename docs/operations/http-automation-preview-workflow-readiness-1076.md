---
id: OPS-HTTP-AUTOMATION-PREVIEW-1076
title: HTTP предпросмотр Автоматизации и готовность Процесса
type: contract-map
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# MVP33 и MVP43

Refs #1045, #1046, #1076, #1078, #1022. Source checkpoints CP:
`dc55a043a9e96b15a7a3a2325eef22e51830f3ff` и
`0bf95a94689f171294378389e978c936b51209e9`. Это расширение существующих
PreviewSchedule и Workflow, без нового endpoint, RPC или permission.
Итоговый исполняемый producer:
`bebb3e04cfd3633b2edfdf8f0f4d490dba3a8b5d`; он включён целиком, включая
MVP19, 647/648 и повторную проверку аккаунта после ожидания блокировки.

| Сценарий | Путь и authority | Ответ и потребитель |
| --- | --- | --- |
| Проверка запуска Workflow | Protected Get/List и mutation/replay → owner current Workflow → shared launch predicate и batch provider admission | launchReadiness с exact Workflow version/current published ref, closed reason и отдельным operationalState; event consumer повторяет GetWorkflow |
| Новый Automation draft | POST schedules/preview с materialization → existing full Proto proof → project.manage и target guards | owner renderer без создания Schedule/Session/Run; DRAFT, revision unavailable |
| Новый draft сохранённой Automation | Тот же POST, scheduleRef/expectedScheduleVersion → schedule.manage до OCC | base revision pin, будущий execution actor; текущая immutable revision не выдаётся за результат draft |
| CURRENT_REVISION | Точный saved spec/version → independent viewer guards и persisted execution actor владельца | current revision совпадает с base, revisionAvailable=true; actor отсутствует в input |
| CONTINUE_ONE | Owner-selected Session и текущая authority → существующий continuation renderer | Session и previous RuntimeRevision совпадают в context pin/runtimeDiff; gateway не создаёт продолжение |
| Полный текст | IncludeFullMaterialization → existing prompt.full.view и fresh authentication владельца | Полный текст только в запрошенном разрешённом ответе; обычный preview отвергает неожиданный full payload |

Материализация принимает projectRef, targetType/targetRef, name/task, input,
promptInputs, sessionPolicy, notificationPolicy и необязательные owner OCC pins.
Подпись охватывает весь Proto request. Ни поля формы, ни mode не назначают
execution actor или capability. Время materializationPin совпадает с первым
occurrence ответа; первый запуск не содержит runtimeDiff.

Workflow readiness сохраняет allowedToSubmit=false явно. UNKNOWN health и
исчерпанная capacity не заменяют owner submission admission. Неизвестная
reason/operationalState, stale version либо несовпадающий published revision
закрыто отклоняются. Owner гидратирует актуальный Workflow также для старого
receipt; HTTP не соединяет старое body с новой readiness.
После UpdateWorkflowDraft состояние DRAFT блокирует запуск с UNPUBLISHED,
даже если основной Published-first body продолжает показывать прежнюю
публикацию. Наличие publishedRevisionRef само по себе не разрешает запуск.

Automation preview повторно использует строгий promptPreviewView, поэтому
пользовательские task, sections и examples сохраняются буквально. Шесть
automationVariables преобразуются тем же typed catalog mapper; переводятся
только server description tokens. Workflow coordinator использует точный
server stage key `workflow.coordinator.initial`; пустой stage key не допускается.
REVISION_NOT_SAVED отличается от deferred runtime context и не превращается в
выдуманную revision.

Preview/read не создают события, leases или receipts. Обычные mutation/OCC и
scheduler lifecycle остаются у владельца. Authoritative rejoin — повторное
protected чтение/preview; HTTP response не является разрешением будущего effect.

Локальные проверки HTTP: полный gateway race PASS (HTTP 6.004 s), vet/build
PASS; после уточнения coordinator pin и DRAFT readback — targeted race PASS
2.038 s. Canonical Go/TS generation и побайтный replay, строгий generated SDK
TypeScript — PASS. После объединения producer HTTP/OpenAPI/SDK и Proto
не изменились относительно проверенного HTTP commit
`5bf26db397ed529e4e3e51be68e3c38f17c0ee57`.
На объединённом дереве Proto lint/build/canonical replay и authority policy72
replay — PASS; неизменный полный HTTP набор повторно не запускался.
Отдельные owner проверки: полный Bootstrap PASS 31.429 s, Avatar PASS 0.392 s;
после узкой проверки блокировки repository race PASS 1.894 s и SQL PASS.
Первый lock fixture PG был FAIL 0.650 s: enabled=false требовал также
state=DISABLED. После исправления только testdata повтор PASS 0.292 s.
Browser/live/provider/staging для этого HTTP вклада: NOT RUN до отдельной
сквозной проверки. Ручной сценарий: сравнить новый draft, изменённый saved draft,
CURRENT_REVISION и CONTINUE_ONE; проверить точные pins и недоступность full
preview без соответствующего права. Секреты и private provider данные не
раскрываются.
