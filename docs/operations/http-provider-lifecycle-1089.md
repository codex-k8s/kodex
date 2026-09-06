---
id: OPS-HTTP-PROVIDER-LIFECYCLE-1089
title: Удаление и проверка provider account в HTTP
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# HTTP lifecycle provider account

Refs #1045, #1046, #1089, #1091, #1094, #1031. Исходный prerequisite
`8dd6f0e5701765f52e3ef9eda38db9f7e91f13f4` заменён consolidated CP
`838ede35d9e04b8e714a9e3f47ac7c9fc95ed3e4`; новая общая acceptance и live
не объявляются выполненными этим HTTP вкладом.
Включён также финальный owner checkpoint
`4e1339f373ca2739ff5c33d88578e45ab377eb87`: SQL name comments и owner evidence,
без изменения HTTP DTO. Весь CP subtree и Proto совпадают с этим checkpoint.

| Сценарий | Authority и owner | HTTP / consumer |
| --- | --- | --- |
| Delete | Verified actor, exact account tenant/revoke; OCC и idempotency после resolve | Existing DELETE назначает DELETING, не обещает завершённую очистку |
| Blockers | Owner применяет тот же eligibility к items/count; query не ищет скрытые имена | GET blockers возвращает visible filtered total и отдельный hiddenCount; kind/query/page передаются без локального поиска |
| Queued cancellation | Exact account + selected Run, fresh run.cancel, blockersDigest и mutation pins; активный turn не отменяется | POST queued-work/cancellation возвращает ровно выбранные Run в прежнем порядке; частичный отказ не превращается в общий успех |
| Cleanup | CP durable task и broker exact descriptor/fence; no hidden retries | DELETED только при шести нулевых counts и pendingCleanup=0; authoritative Get/replay сохраняют tombstone |
| Verify AUTHORIZED | Existing Verify создаёт новую attempt текущей credential, не переиспользует прежний catalog success | verification отделяет PENDING/VERIFIED/FAILED/STALE, credential/account pins, scope и времена |
| Reauthorize | Existing отдельная device authorization attempt той же account identity | HTTP сохраняет новый authorization descriptor; Verify не подменяется reauth |

Новых HTTP background jobs/events нет. Защищённые Get/List и прежние owner
events/replay используют один validator lifecycle projection. Входные refs
не выдают authority. Все account/codegen enums закрыты. Unknown state/reason,
future verification accountVersion, false terminal, duplicate blocker kind,
foreign cancellation result и malformed pins закрываются до выдачи body.
Непустой исторический verification.accountVersion может быть меньше текущего
account version; это provenance проверки, а не право использовать старую
credential. CompletedAt обязателен у terminal verification и DELETED intent.

Blockers query ограничен 200 Unicode code points, page 1–100, token 2048;
selectedRunRefs — 1–64 уникальных refs. hiddenCount зависит от kind, но не от
текстового query. Он не содержит скрытых имён и не исключается из owner
решения об удалении. canCancel возможен только для QUEUED_TURN с Run.ref и
Run.version; прочие blockers не превращаются в произвольный cancel endpoint.
ContextDigest связывает весь authoritative blocker snapshot/account/actor и
одинаков для фильтров одного снимка; cursor отдельно связывает kind/query.
Это позволяет Cancel проверить digest без неизвестного ему поискового фильтра.
AGENT и PROVIDER_POOL используют Agent ref/version текущей policy, AUTOMATION —
Schedule, ACTIVE_TURN/QUEUED_TURN — Run, WARM_RUNTIME — SystemAssistant.

Ручная проверка после согласованного owner deployment: запросить удаление
используемого account, увидеть blockers и отсутствие немедленного DELETED;
отменить явно выбранный queued Run, проверить отдельные outcomes; убрать
зависимости и дождаться cleanup/tombstone. Для AUTHORIZED account запустить
Verify, дождаться новой exact credential-bound observation; смена credential
делает прежнюю attempt STALE. Ручной и live запуск пока NOT RUN.

Локально PASS: canonical Go/TS/AsyncAPI generation и replay, strict generated
SDK, provider HTTP race 1.188 с. Первый compile запуск завершился FAIL на
различии int64/uint64 и optional ExpectedVersion; casts и fixture исправлены,
повтор прошёл. Полный gateway baseline после объединения owner73 пока NOT RUN.
Секреты, pending object UID/RV,
credential material и broker cleanup descriptors в публичные DTO не попадают.
Rollback согласуется для всего CP/HTTP/PWA lifecycle; возврат немедленного
«успешного удаления» при незавершённой cleanup недопустим.

## Независимая полнота ответа до финального owner checkpoint

| Источник | RPC / mapper | HTTP / потребитель |
| --- | --- | --- |
| #1091: новая verification attempt | VerifyProviderAccountDeviceAuthorization → проверка непустого Account с точным requested ref → общий lifecycle validator | 200 account, версия может остаться прежней; локализуется PROVIDER_ACCOUNT_VERIFICATION_REQUESTED |
| #1091: отдельная повторная авторизация | ReauthorizeProviderAccountDeviceCode → тот же exact account guard | 202 account той же identity, новый authorization descriptor назначает CP |
| #1089: intent удаления | DeleteProviderAccount → тот же exact account guard | 200 account; пустой либо чужой ответ закрывается 502 до выдачи данных |
| #1090/#1092: существующая operation12 | Proto TYPE_UPDATE_PROJECT → AssistantPlanOperation.type и AssistantContextDescriptor.allowedOperations | Закрытые OpenAPI enum/SDK сохраняют UPDATE_PROJECT; actor eligibility остаётся у CP |

Request ref служит только проверкой согласованности ответа, не источником
authority. Verified transport context, tenant resolve, OCC и idempotency остаются
в существующей цепочке CP. Никаких новых grants, событий или endpoints нет.
Положительные тесты сохраняют прежние HTTP статусы и mutation pins; отрицательные
проверяют nil/foreign account и отсутствие чужого текста. Operation12 проверяется
на входе и в обоих readback, неизвестная operation отвергается закрытым enum.

Этот предыдущий checkpoint был подготовлен независимо от dirty CP73/651.
Локально на окончательном коде дополнения: canonical Go/TS/AsyncAPI generation
и повтор без изменения generated файлов PASS, strict SDK PASS; scoped race
Provider/AssistantUpdateProject/Catalog PASS (HTTP 1.249 s, usertext 1.071 s).
Первый scoped запуск FAIL: guards ошибочно попали в соседние обработчики,
и negative reauthorize получил 202. Привязка исправлена точно для трёх
заявленных команд, повтор PASS. Проверки выполнялись до evidence/commit;
полный owner integration и live acceptance остаются NOT RUN.

## Consolidated owner и окончательный HTTP readback

CP73 source объединяется семантически; generated Proto восстанавливается
каноническим генератором из итогового source. Recovery origin broker остаётся
внутренним и не добавляется в HTTP/SDK.

`DELETING` с `deletion.state=FAILED` сохраняет ненулевой pendingCleanup и не
имеет completedAt. Явный новый Delete использует новый key и текущий OCC;
неизвестный исход прежнего Delete повторяется с прежним key/OCC. HTTP не
назначает DELETE по состоянию: доступный action приходит от CP. Все шесть
закрытых cancellation outcomes сохраняются отдельно и в выбранном порядке;
nil, foreign, missing, reordered и unknown outcomes закрываются 502.

Verify readback содержит отдельную PENDING attempt даже при неизменной account
version; Get/List/command используют общий validator. Историческая attempt
может иметь меньшую accountVersion. Семь context kinds PROJECT/AGENT/WORKFLOW/
RUN/FILE/ENVIRONMENT/INTEGRATION_CONNECTION сохраняют owner metadata и allowed
operations без вывода permission из kind. ROLE_IMAGE full revision требует
presence sourceAvailable; sourceEditable относится к ROLE_IMAGE configuration,
а metadata summary не притворяется полной revision.

Для #1094 каждое RETRY, включая точный replay, вызывает owner RetryRun. Fresh
target launch denial остаётся HTTP403 без raw error; run.view не добавляет
RETRY в nextActions. HTTP не заменяет CP permission/OCC/receipt проверки.
Локализован PROVIDER_ACCOUNT_QUEUED_WORK_PROCESSED без обещания, что все
выбранные задания отменены.

Актуальные правила обработки gRPC ошибок проверены через Context7
`/grpc/grpc-go`: [RPC errors](https://github.com/grpc/grpc-go/blob/master/Documentation/rpc-errors.md).
Локальный полный gateway race на итоговом production/test коде после owner
merge прошёл PASS: HTTP 6.324 s, boundary 2.050 s, session 1.015 s,
usertext 1.083 s; `go vet -mod=readonly -p 1 ./...` и аналогичная сборка PASS.
Логи: `/home/s/.k1045/http-cp4e133-{race1,vet,build}.log`.
Proto lint/build/clean replay, canonical OpenAPI Go/TS и AsyncAPI replay,
policy73 и strict generated SDK — PASS; повтор не изменил generated файлы.
Логи `http-cp4e133-replay.log` и `http-cp4e133-sdk.log` в той же private
директории. AsyncAPI validator сохранил рекомендацию перейти на 3.1.0,
validation завершилась exit 0. Новой ошибки suite в этом объединении не было.
Проверки выполняются до изменения этого evidence-документа и итогового commit;
они не объявляются отдельным запуском после commit. Исторические FAIL выше
сохраняются. Общий review, deploy и live acceptance NOT RUN.
