---
id: OPS-PROVIDER-MODEL-CATALOG-1068
title: Наблюдение каталога моделей через Secret Broker
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Граница поставки

Issue #1068 / PR #1069 содержит Secret Broker consumer для MVP-UI-17/18/19.
Владелец account, catalog observation, freshness, task/claim, авторизации и
публикации находится в control-plane #1046. HTTP/SDK #1045 и PWA #1022 читают
его безопасный каталог. Broker не назначает доступность account, revision или
права пользователя и не возвращает credential браузеру.

## Карта сценария и переходов

| Переход | Полномочия и запрос | Эффект broker | Ответ и владелец результата |
| --- | --- | --- | --- |
| Наблюдение | CP durable task → issuer → mTLS/bearer/JWS → `ObserveProviderModelCatalog`, exact `platform.provider-accounts.model-catalog.observe` | После полного unary SHA256 binding читает exact account/Secret UID/resourceVersion/content digest | Только models, source, observedAt, account/credential revision echo; CP принимает результат для той же живой task/claim |
| Credential rotation/revoke | CP проверяет текущие account/version/credential generation до выпуска proof | Старый или отозванный proof отклоняется до Secret read | Владелец закрывает прежнюю task; новая generation требует новой task и proof |
| Успех | Task/claim/fence/expiry назначены CP, metadata project/resource/version/idempotency/attempt запрещены | Bounded remote GET и безопасное пересечение с runtime capabilities | `NONE`, `REMOTE_API` либо `REMOTE_CODEX`; CP фиксирует durable observation, затем public version/digest |
| Подтверждённый пустой список | Та же полная проверка и свежий ответ provider | Возвращает пустое пересечение без встроенного fallback | CP определяет пустую доступность; старый список не сохраняется как свежий |
| Provider 401/403, запрос OAuth refresh | Не разрешает обновление credential | Прекращает наблюдение, не передаёт refresh token и не повторяет авторизованный запрос с новым token | `AUTHORIZATION_REJECTED`, без models/source; CP определяет дальнейший reauth |
| Ошибка связи | Ограниченный deadline без redirect/direct fallback | Не выдаёт частичный результат | `UNAVAILABLE`; CP сохраняет отказ, retry только через новую owner attempt |
| Непроверенный источник | Missing/stale/malformed cache, дубликаты, лишние страницы, несовпадение capabilities | Закрыто отклоняет snapshot | `UNVERIFIED_SOURCE`; CP не подменяет его успешной свежестью |
| Cancel/expiry/shutdown | Минимум caller deadline, proof expiry, task expiry и 15 секунд | Kill process group, join reader/process, удаление временного каталога; возврат без models | Context error; CP завершает/повторяет задачу по собственному lifecycle |
| Lost response | Наблюдение не изменяет credential и не публикует catalog само | Не делает hidden retry | CP authoritative task/observation read определяет исход; новое чтение требует разрешённой claim |

Для broker операции отдельного domain event нет: authoritative read находится
в CP task/observation/receipt. Issuer обязан проверить exact task/account и digest;
payload сам по себе не доказывает эти полномочия. Одна декларация RPC/policy не
означает, что owner worker и public consumers прошли проверку.

## Источник и ограничения

API-key путь выполняет HTTPS GET `https://api.openai.com/v1/models` через exact
egress CONNECT. Redirect, прямой fallback, partial response и дубли запрещены.
Upstream ограничен 4 MiB/4096 IDs; ответ consumer — 128 моделями, 16 усилиями
на модель и 128 KiB. OpenAI Models API не сообщает reasoning capabilities:
результат пересекается с полным paginated `model/list` закреплённого Codex.

DEVICE_CODE путь не копирует `auth.json`: после `initialize` передаёт только
`chatgptAuthTokens` access token/account ID через stdin. Managed refresh token
никогда не попадает в этот процесс. Любой server request закрыто отклоняется.
Для успешного результата требуется созданный в новом private home
`models_cache.json` с `fetched_at` текущего вызова и `client_version=0.152.0`.
Pinned Codex записывает его после успешного remote fetch; ошибка может оставить
встроенные модели в `model/list`, поэтому одного JSON-RPC успеха недостаточно.
Берутся только IDs/default/efforts из свежего remote snapshot, с точным
сравнением capabilities. Prompt metadata, transcript и прочий сырой ответ не
копируются. Symlink, hardlink, FIFO и чужой UID отклоняются до чтения cache.

Модели без reasoning имеют пустые efforts/default; строки `none` как
допустимое усилие и отсутствие reasoning различаются. Список возможностей
проверяет уникальность, границы и принадлежность default множеству efforts.

## Composition, readiness и развёртывание

Используются существующие secret-broker ServiceAccount, credential read RBAC,
private ephemeral Codex home, pinned image binary и exact egress: новый
plaintext projection или endpoint для браузера не создаётся. Рабочий RPC
включён в protected route registry и закрытый metric bucket
`provider_catalog_observe`. Local readiness проверяет credential store и
исполняемый app-server; доступность конкретного account доказывается только
свежим owner observation через тот же защищённый RPC.

Процесс получает только закрытый PATH/HOME/CODEX_HOME и exact HTTP(S)_PROXY;
пользовательский env, NO_PROXY и proxy credentials не наследуются. Вывод
ограничен, stderr отбрасывается; ошибки возвращаются как безопасные коды.
Все temporary bytes удаляются после cancel/join. Наблюдение не сохраняет
provider credentials, не вызывает inference и не обновляет OAuth.

## Документация и проверки

Проверены Context7 `/openai/codex` и официальные
[app-server model/list и external tokens](https://developers.openai.com/codex/app-server),
[pinned login schema](https://github.com/openai/codex/blob/rust-v0.152.0/codex-rs/app-server-protocol/schema/typescript/v2/LoginAccountParams.ts),
[models manager](https://github.com/openai/codex/blob/rust-v0.152.0/codex-rs/models-manager/src/manager.rs),
[remote cache](https://github.com/openai/codex/blob/rust-v0.152.0/codex-rs/models-manager/src/cache.rs).

`make test-secret-broker-drafts` включает полный broker race/vet/build и
render. Providercredential tests проверяют API intersection, пустой/повреждённый
источник, default/efforts, cache provenance, реальные child process/stdio,
отказ refresh, deadline, cleanup и reader join. Protected interceptor tests
проверяют authority до materializer; synthetic TLS context/owner verifier
не являются доказательством настоящего TLS/issuer/CP вызова.

После сборки exact broker image отдельная публичная проверка выполняет
`--version`, `initialize`, `model/list` и external token login на настоящем
pinned Codex. Disposable контейнер использует `--network none`, read-only root,
non-root UID и private tmpfs; передаётся только синтетический JWT. Команда не
проверяет provider availability и не использует account credential:

```bash
make test-provider-model-catalog-codex KODEX_CATALOG_CODEX_TEST_IMAGE="$EXACT_BROKER_IMAGE"
```

Результаты привязываются к exact SHA в PR #1069. Full protected CP producer,
real Codex/provider и browser acceptance требуют отдельного подтверждённого
контура; до фактического запуска их статус — NOT RUN. Merge/staging общий gate
и полная приёмка принадлежат #1031.
