---
id: OPS-HTTP-CONFIGURATION-WRITEBACK-1045
title: HTTP подтверждение Git writeback
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

Источник #1045/#1046, Epic #1018, CFG-01. Producer640/policy65 находятся
в объединённом CP455/policy68. Все семь HTTP consumers и SDK принадлежат
существующему gateway PR #1066.

| Actor и сценарий | HTTP → RPC | Owner переход, ответ и consumer |
| --- | --- | --- |
| Подписанный actor, current configuration/source manage | POST role-image-configurations/{configurationRef}/git-write-backs → PrepareRoleImageGitWriteBack; аналог IntegrationDefinition | Exact If-Match configuration, expectedSourceVersion и content; owner WAITING_APPROVAL, immutable approval digest, audit/receipt; PWA читает план |
| Тот же owner source-read | GET managed-configuration-git-write-backs/{proposalRef} → GetManagedConfigurationGitWriteBack | Summary и exact base/proposed documents с проверенными SHA256; без события |
| Current owner | GET managed-configurations/{configurationRef}/git-write-backs → ListManagedConfigurationGitWriteBacks | Только summary, authoritative count/cursor, без content и private descriptors |
| Явное решение пользователя | POST proposal/approve или reject → соответствующая specialized command | Proposal If-Match и exact approvalDigest, CSRF/idempotency; QUEUED либо REJECTED, audit/receipt |
| Отмена | POST proposal/cancel → CancelManagedConfigurationGitWriteBack | If-Match/CSRF/idempotency; CANCELLED до эффекта либо UNKNOWN_OUTCOME при уже начатом действии, без ложного подтверждения отмены |

Payload не назначает authority. Канонический unary issuer связывает полный
protobuf digest; owner извлекает точные refs и версии из request и повторяет
текущие права. Дополнительные resource/project hints gateway не придумывает.
Idempotency replay возвращает exact receipt, HTTP не заменяет его latest GET.

Внешний worker создаёт отдельную ветку и PR/MR. Source branch и активный runtime
этими HTTP командами не меняются: их обновление следует только после внешнего
merge и штатного source sync. Ни Prepare, ни GET не подразумевают Approve.
Событий для пользовательских переходов нет; PWA использует protected Get/List
и bounded polling nonterminal outcome согласно owner matrix.

HTTP проверяет owner/source/proposal pins, версии safe53, закрытые состояния,
ошибки и три action availability; enabled допустим только при reason NONE.
Success требует branch и PR receipts; наличие только branch receipt допустимо
для UNKNOWN_OUTCOME. URI не содержит credentials/query/fragment и использует
HTTPS. Оба документа ограничены 256 KiB UTF-8, проверяются exact SHA256 до
выдачи. Summary не содержит документов, leases, grants или credential values.

Проверки: семь реальных handwritten routes с exact RPC/OCC/intent mappings,
invalid caller fields до owner, unknown enums/false authority/unsafe versions,
ветка без PR, confirmed PR и повреждённое содержимое без echo. Full gateway
race, vet/build, OpenAPI/SDK/codegen ledger фиксируется на exact checkpoint.
Browser, рабочий external Git path, integrated review и deploy — NOT RUN.
Scope unit не сокращён, отдельный PR не создан. Секреты не раскрыты.
