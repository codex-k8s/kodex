---
id: OPS-PROVIDER-RENEWAL-001
title: Управляемое обновление авторизации провайдера
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-14
---

# Область

Issue #1797, bootstrap PR #1807. Владелец 14.10.2026 поручил автономно
выполнить семь оставшихся этапов, включая рекомендуемую отдельную управляемую
задачу обновления credential. Прежний вопрос о выборе этого пути больше не
является ожиданием решения. Само разрешение не расширяет READ observer.
Этот документ фиксирует карту реализации; доставка полного пути пока OPEN.

Account `AUTHORIZED` означает подтверждённое подключение, не свежесть OAuth
или доступность каталога. Недоступность каталога не означает отозванную учётку.
Необратимый refresh исполняет только secret-broker по отдельной задаче CP.
Нельзя использовать фиктивную execution lease, PENDING device-code endpoint,
bootstrap reconciliation, повтор старого access token или экспериментальный
режим вместо этой задачи.

# Сквозная карта

| Этап | Actor, authority, pins и результат |
| --- | --- |
| Инициатор | CP maintenance по текущему DEVICE_CODE account и подтверждённому `AUTHORIZATION_REJECTED` каталогу. Browser endpoint отсутствует; payload не назначает actor/org |
| Создание задачи | Owner-транзакция CP блокирует account; назначает task/attempt/claimant/fence/generation/expiry, account version и exact current credential ref/revision/descriptor. Активные executions этого аккаунта и другая renewal исключают внешний эффект |
| Авторизация записи | Отдельный `ProviderCredentialMaterializerService.RenewManagedProviderCredential`, операция `platform.provider-accounts.credential.renew`, caller `control-plane`, target `secret-broker`, SERVICE actor, без project override. Registered exact method, request digest и свежий CP task proof обязательны |
| Fresh owner consume | До credential read broker вызывает `ProviderCredentialRenewalWorkService.AuthorizeProviderCredentialRenewal`: caller `secret-broker`, target `control-plane`, операция `platform.provider-accounts.credential.renewal.authorize`. Owner выводит actor/org из transport, проверяет полный sealed task и переводит CLAIMED → AUTHORIZED одной транзакцией; browser endpoint отсутствует |
| Materializer | Перед native process durable receipt exact task/input в Kubernetes. Только создатель receipt может начать managed flow; повтор читает результат, не запускает процесс. Seed — exact immutable credential, private managed CODEX_HOME и штатный file store |
| Native protocol | Закреплённый Codex0.160.0, `initialize` с `experimentalApi=false`, `account/read` с `refreshToken=true`. Нет inference, нового login/device-code, token callback или сетевого bypass |
| Сохранение | После bounded stop/join writer exact regular600/single-link/own-UID auth file читается через nofollow descriptor. Account identity прежняя, access token действительно изменён, native last_refresh свежий. Bytes не выходят из broker; новая immutable Secret сохраняется до очистки home |
| Rejoin | Отдельный `ProviderCredentialMaterializerService.ObserveManagedProviderCredentialRenewal`, READ операция `platform.provider-accounts.credential.renewal.observe`. Exact сохранённые task/input pins; возвращает только закрытое состояние и descriptor. Чтение не повторяет OAuth |
| Fresh owner observation | Broker использует `ProviderCredentialRenewalWorkService.ValidateProviderCredentialRenewalObservation`, операция `platform.provider-accounts.credential.renewal.validate`, exact `secret-broker`/SERVICE. Старый expires_at — immutable input, не новый срок: READ получает отдельную свежую authority, owner проверяет current account/task и допустимость recovery, но не возобновляет WRITE |
| Owner completion | CP заново проверяет task/claim/fence/generation, current account/version/credential и immutable materializer receipt. Одна транзакция создаёт новую credential revision, CAS активирует её, закрывает claim, назначает cleanup прежней и фиксирует audit/receipt/`PROVIDER_ACCOUNT_CHANGED` |
| Consumers | Catalog worker создаёт новую READ task на новых pins; runtime admission получает только свежую revision; UI/cache получает прежнее событие и авторитетный account read. Старый catalog response не принимается за новую revision |
| Registry/delivery | Proto/generated clients, exact machine policy/proof resolver, broker route/readiness, CP worker startup/join, migrations, render и serving readback доставляются вместе. Пока они не реализованы, kernel сам по себе не является рабочим путём |

# Жизненный цикл

| Переход | Cardinality, событие и восстановление |
| --- | --- |
| Eligible → CLAIMED | Одна текущая task на credential, server-owned attempt/fence, полное seal атомарно при создании. До внешнего эффекта durable owner authorization; события нет, authoritative read — owner task ledger |
| CLAIMED → AUTHORIZED | Одноразовое право запуска managed flow. Runtime claim того же аккаунта не допускается до завершения; никакой глобальной блокировки других аккаунтов |
| AUTHORIZED → SUCCEEDED | Не более одного materializer process и одной новой active revision; owner completion/audit/event атомарны. Повтор exact completion возвращает тот же receipt. Внутреннее число HTTP refresh запросов native Codex не объявляется равным одному |
| Повтор broker call / lost ACK | Exact durable receipt либо recovery произведённой immutable Secret. OAuth не повторяется. Неизвестное состояние не объявляется успехом |
| Истечение до авторизации эффекта | FAILED, прежний claim закрыт; новая попытка получает новые pins/fence. События нет, ledger authoritative |
| Истечение/crash после авторизации эффекта | UNKNOWN_OUTCOME; старый refresh token не используется повторно. READ rejoin может восстановить доказанный материализованный результат; отсутствие результата требует штатной повторной авторизации владельцем |
| Account disable/delete/reauthorize/version drift | Незавершённая task закрывается owner-транзакцией, прежние grants/claims отзываются. Late result не активируется; произведённый orphan обрабатывается exact cleanup. Событие самой owner account-команды сохраняется |
| Отказ persistence/stop/join/file identity | Не выдаётся свежий catalog или active revision; UNKNOWN_OUTCOME после возможного эффекта, закрытая диагностика без native stderr/token values |
| Current credential сменился другим writer | CAS conflict; нет rollback, второй активации или перезаписи текущего credential. Runtime writer и renewal используют один account lock и current revision predicate |
| Receipt/source mismatch, foreign actor/tenant/workload, expired proof | Отказ до credential read и процесса; никаких эффектов и domain event |

# Доказательства и остатки

- [x] Неавторизованный CONNECT/TLS probe из текущего broker Pod: оба exact
  домена CONNECT200/TLS verified; корневой HTTP403 не является отказом OAuth API.
- [x] Адресные синтетические process tests managed kernel: стабильный режим,
  изменённый token, сохранение ровно один раз, чужой аккаунт/неизменённый token/
  отказ хранилища закрыто отклоняются. Это CODE_ONLY, не live refresh.
- [ ] Durable materializer receipt, rejoin и межрепличный single-winner.
- [ ] Полный CP task/claim/authorization/completion lifecycle и runtime exclusion.
- [ ] Proto/authority/generated clients/worker/readiness/render.
- [ ] Disposable component negative/restart/lost-ACK/concurrent-writer проверки.
- [ ] Trusted-dev delivery, новая credential revision и REMOTE_CODEX catalog.
- [ ] Native Developer/ONE Reviewer/Manager и остальные шесть этапов цели.

Проверены Context7 `/openai/codex`, официальная документация app-server и
[исходник Codex0.160.0](https://github.com/openai/codex/tree/a956835d020762cb2b570053af06f643a11c0ecc).
Managed `account/read` вызывает native refresh; external auth этот вызов
не заменяет. Source `persist_tokens` сохраняет новые tokens и last_refresh.
Приватные credentials, тела ответов и native логи в доказательства не включаются.
