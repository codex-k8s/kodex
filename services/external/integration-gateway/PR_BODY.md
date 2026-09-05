---
id: EXT-INT-1028-HANDOFF
title: Передача полного каталога integration-gateway
type: delivery-report
status: approved
owner: backend
version: 1.0.0
updated: 2026-09-05
---

# Полный каталог MVP-UI-42

Связано с #1028, эпиком #1018. Дополняет foundation PR #1049 до принятого
набора MVP-UI-42. База включает main `516f330e4c6b0a07aab58aa798ff9988cad485a2`.

Добавлены 84 операции: GitHub 41, GitLab 37, Jira 22, Confluence 16.
Вместе с двумя Synthetic это 118 операций области #1028; общий исполняемый
каталог содержит 121 с тремя неизменёнными email operations. Mattermost,
email, CP Proto/policy, HTTP и PWA не изменены.

Полный закрытый набор и путь authority приведены в [матрице](OPERATION_MATRIX.md).
Версии definitions: GitHub 2.2.0, GitLab/Jira/Confluence 1.2.0.
Schema и shipped generated обновлены; явное `allowEmpty` разрешает пустое
содержимое файла/описание, но не ослабляет configuration или scope fields.

## Критерии и доказательства

Все результаты ниже локальные, не GitHub CI. Проверены исходники перед
коммитом; точный итоговый SHA сообщается отдельно после фиксации дерева.

| Критерий | Конкретное доказательство | Результат / NOT RUN |
| --- | --- | --- |
| Каждая объявленная операция исполнима | `TestEveryAdvertisedOperation`, отдельные fake responses и проверка typed output/receipt всех 121 capabilities | PASS |
| Exact authority до credential | `TestScopeDeniedBeforeCredentialRead`, `TestCatalogPinnedAuthorityBeforeCredentials`, `TestCatalogResourceIdentifiersBeforeCredentialRead` | PASS |
| Scope связанных ресурсов и поискового запроса | `TestRelatedResourcesNeverEscapeParentScope`, `TestJiraQueryScopePreservesSortingAndRejectsEscapes`, `TestConfluenceReplyBindsVerifiedParent` | PASS |
| Pagination и rate limit | Положительные fixtures страниц, `TestGitLabPaginationUsesExactProviderCursor`, `TestReadOperationsHandleRateLimits` | PASS |
| Ограниченные файлы и рабочие пустые файлы | `TestProviderFilesAreBoundedBeforeDecoding`, `TestGitHubFileNamesAreEscapedAndEmptyFilesRemainUsable` | PASS |
| Нет повторного эффекта при неизвестном результате | `TestEveryMutationPreservesUnknownOutcome`, `TestCatalogMutationProviderFailuresNeverReplay` | PASS |
| Durable receipt, Human Gate, отзыв и tenant boundary | `make test-integration-gateway-postgres`: существующий CP owner lifecycle с disposable PostgreSQL | PASS |
| Synthetic сквозной путь | `make test-integration-synthetic`: race, локальный E2E, render | PASS |
| Go и race | `go test ./...`, `go test -race ./...`, `go vet ./...` в gateway; `go test -race ./...` в integrationpackage | PASS |
| Машинный каталог и deploy-профили | `make gen-integration-packages check-integration-package-codegen test-integration-gateway-render`, web-only и web-with-mattermost | PASS |
| Живые vendor API, установленный MCP/PWA, staging/production | Требуют отдельного окружения и разрешения владельца | NOT RUN |
| Общий baseline и product/security/architecture review | Выполняет root на итоговом интегрированном SHA | NOT RUN |

Первоначальный запуск PG с самостоятельным узким regex `integration` дал
FAIL из-за отсутствующей подготовки provider capacity. Поддерживаемая публичная
цель `test-integration-gateway-postgres`, включающая эту подготовку, прошла.
Изменений CP для обхода ошибки не вносилось.

## Ручная проверка владельца

1. После отдельного разрешённого развёртывания привязать новую version-pinned
   definition к тестовому connection и ограниченному repository/project/space.
2. В MCP проверить список capabilities и чтение страницы/файла, PR/MR diff,
   Jira transitions и Confluence comments в разрешённом scope.
3. Запросить изменение: до Human Gate внешнего эффекта нет; после допуска
   появляется один результат и durable receipt. Повтор invocation не создаёт
   второй эффект. Отозванный grant не выдаёт новый claim.
4. На fake provider оборвать ответ мутации: получить unknown outcome;
   повторное выполнение допустимо только через существующее решение владельца
   или reconciliation, не через автоматический сетевой retry.
5. Проверить чужой parent/resource и незарегистрированную operation:
   отказ без materialization credential и без внешней мутации.

## Ограничения и откат

Новых миграций, RPC, workload, сетевых разрешений, RBAC или обходов egress нет.
Используются существующие composition, probes, метрики и deploy ownership;
render обоих профилей проверен. Обновление закреплённых connection revisions
проходит существующий управляемый lifecycle, не подменяется новым каталогом.

Тела ограничены 64 KiB, внешние redirects не разрешены. Vendor JSON не является
универсальным proxy: выдаётся закрытая типизированная проекция. Большие файлы,
CDN redirects, GitHub Actions artifacts/log archives не объявляются как
поддерживаемые операции. Остальные ограничения перечислены в матрице и README.

Откат к прежним definitions требует согласованной перепривязки revision и
запрета новых claims. Откат кода не отменяет внешние эффекты и не является
разрешением повторить unknown mutation; durable receipts остаются у CP.

Проверены Context7: go-github, GitLab API, Jira REST v3, Confluence REST v2;
дополнительно официальные GitHub Actions, GitLab MR diffs и Confluence comments
docs, ссылки приведены в README. Версии зависимостей не менялись.

Секреты не раскрыты. Push, создание PR, remote merge и deploy не выполнялись.
