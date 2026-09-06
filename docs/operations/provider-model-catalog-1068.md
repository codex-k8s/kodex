---
id: OPS-PROVIDER-MODEL-CATALOG-1068
title: Наблюдение каталога моделей через Secret Broker
type: operations
status: approved
owner: developer
version: 1.1.0
updated: 2026-09-06
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
результат пересекается с отдельным embedded
`internal/providercredential/api_model_capabilities.json`. Codex picker не
является источником API reasoning/default. Неизвестные IDs, aliases, snapshots
и Spark отсутствуют в API результате, даже если account их перечисляет.
API key не передаётся app-server и не записывается в его `auth.json`.

Источник `openai-api-2026-09-06.1` имеет schemaVersion, exact runtimeVersion,
verifiedAt/validUntil, официальные model URLs, apiDefault и defaultOrigin.
SHA256 `bfb2bda2354b563968f77c4176032c5728ee450fdc2839cc6e591829a7c2cd63`
закреплён в коде и проверяется перед каждым account запросом и перед выдачей
результата. Unknown schema/revision/runtime, повреждение, будущая проверка
или истечение срока дают `UNVERIFIED_SOURCE`, без models. Срок источника —
30 дней, до `2026-10-06T00:00:00Z` исключительно; обновление требует новой
проверки official docs, revision/digest, regression и нового image. Наличие
этого источника само по себе не доказывает account eligibility: всегда нужен
свежий защищённый `/v1/models`; его отказ не заменяется встроенным списком.

| Exact API ID | Efforts по API | Default Kodex | Происхождение default |
| --- | --- | --- | --- |
| `gpt-6-astra` | low, medium, high, xhigh, max | low | `KODEX_RUNTIME_POLICY`; API default не объявлен, `apiDefault=null` |
| `gpt-5.6-sol` | none, low, medium, high, xhigh, max | medium | `OPENAI_API_DOCUMENTATION` |
| `gpt-5.6-terra` | none, low, medium, high, xhigh, max | medium | `OPENAI_API_DOCUMENTATION` |
| `gpt-5.6-luna` | none, low, medium, high, xhigh, max | medium | `OPENAI_API_DOCUMENTATION` |
| `gpt-5.5` | none, low, medium, high, xhigh | medium | `OPENAI_API_DOCUMENTATION` |
| `gpt-5.4` | none, low, medium, high, xhigh | none | `OPENAI_API_DOCUMENTATION` |
| `gpt-5.4-mini` | none, low, medium, high, xhigh | none | `OPENAI_API_DOCUMENTATION` |

Default Astra — явный выбор Kodex из поддерживаемого API набора, необходимый
действующему обязательному runtime default. Это не утверждение о default
OpenAI. Broker передаёт его как `DefaultReasoningEffort`; CP включает exact
models/default/efforts в свой catalog digest и immutable runtime overlay.
Дополнительный `ultra` из Codex picker не попадает в API capabilities.

DEVICE_CODE путь не копирует `auth.json`: после `initialize` передаёт только
`chatgptAuthTokens` access token/account ID через stdin. Managed refresh token
никогда не попадает в этот процесс. Любой server request закрыто отклоняется.
Для успешного результата требуется созданный в новом private home
`models_cache.json` с `fetched_at` текущего вызова и `client_version=0.153.4`.
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
исполняемый app-server с exact `codex-cli 0.153.4` через bounded `--version`;
старый binary закрыто отклоняется до account запроса. Доступность account доказывается только
свежим owner observation через тот же защищённый RPC.

Процесс получает только закрытый PATH/HOME/CODEX_HOME и exact HTTP(S)_PROXY;
пользовательский env, NO_PROXY и proxy credentials не наследуются. Вывод
ограничен, stderr отбрасывается; ошибки возвращаются как безопасные коды.
Все temporary bytes удаляются после cancel/join. Наблюдение не сохраняет
provider credentials, не вызывает inference и не обновляет OAuth.

## Документация и проверки

Проверены Context7 `/openai/codex` и официальные
[app-server model/list и external tokens](https://developers.openai.com/codex/app-server),
[pinned login schema](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/app-server-protocol/schema/typescript/v2/LoginAccountParams.ts),
[models manager](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/models-manager/src/manager.rs),
[remote cache](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/models-manager/src/cache.rs).

Для #1100 применён skill openai-docs и Context7 `/websites/developers_openai_api`
и `/openai/codex`; проверены официальные model pages
[Astra](https://developers.openai.com/api/docs/models/gpt-6-astra),
[Sol](https://developers.openai.com/api/docs/models/gpt-5.6-sol),
[Terra](https://developers.openai.com/api/docs/models/gpt-5.6-terra),
[Luna](https://developers.openai.com/api/docs/models/gpt-5.6-luna),
[5.5](https://developers.openai.com/api/docs/models/gpt-5.5),
[5.4](https://developers.openai.com/api/docs/models/gpt-5.4),
[mini](https://developers.openai.com/api/docs/models/gpt-5.4-mini),
[reasoning guide](https://developers.openai.com/api/docs/guides/reasoning),
[Astra guide](https://developers.openai.com/api/docs/guides/latest-model)
и [release0.153.4](https://github.com/openai/codex/releases/tag/rust-v0.153.4).
Bundled source0.153.4 подтверждает поддержку Astra, но содержит другие
Codex defaults/efforts; поэтому он не используется как API source.
Pinned `core/src/client.rs` (`reasoning_effort_for_request` и сборка `Reasoning`)
передаёт explicit API efforts без преобразования; отдельное преобразование
`Ultra`/`Persistent` не применяется к этому API набору. Offline `thread/start`
проверяет actual model/effort readback, но не доказывает inference.

## Карта исправления #1100 по существующим unit

Broker #1068/PR1069 владеет embedded API source, parser/digest/freshness,
API/device разделением, exact binary check, providercredential tests,
broker Dockerfile, этой документацией и offline catalog fixture.
Согласованный toolchain bump включает `tools/install/components.lock.json`,
`tools/install/prepare-host.sh`, `scripts/tests/install-contract-test.sh`,
`scripts/tests/host-hardening-contract-test.sh` и
`docs/design-guidelines/common/external_dependencies_catalog.md`.
Runner unit получает оба pin в своём Dockerfile, поддержку API `none` для5.5,
regression всех семи моделей и актуальную version в parser fixture.
Manager переносит shared/runner paths в соответствующие существующие unit
при сборке; CP, HTTP и PWA этим исправлением не редактируются.

Source → exact binary0.153.4 → authenticated account list → broker observation
→ CP task/credential pins/freshness → catalog digest → immutable runtime
selection/overlay → runner validation → explicit app-server model/effort.
Каждое новое account observation проходит прежнюю owner authority; встроенный
source не выдаёт account/project grants и не расширяет device/Spark доступ.

`make test-secret-broker-drafts` включает полный broker race/vet/build и
render. Providercredential tests проверяют API intersection, пустой/повреждённый
источник, default/efforts, cache provenance, реальные child process/stdio,
отказ refresh, deadline, cleanup и reader join. Protected interceptor tests
проверяют authority до materializer; synthetic TLS context/owner verifier
не являются доказательством настоящего TLS/issuer/CP вызова.

После сборки exact broker image отдельная публичная проверка выполняет
`--version`, `initialize`, весь paginated `model/list`, explicit `thread/start`
для каждого effort семи API моделей и external token login на настоящем
pinned Codex. Disposable контейнер использует `--network none`, read-only root,
non-root UID и private tmpfs; передаётся только синтетический JWT. Команда не
вызывает `turn/start`, не проверяет inference/provider availability и не использует account credential:

```bash
make test-provider-model-catalog-codex KODEX_CATALOG_CODEX_TEST_IMAGE="$EXACT_BROKER_IMAGE"
```

Результаты привязываются к exact SHA в PR #1069. Full protected CP producer,
real Codex/provider и browser acceptance требуют отдельного подтверждённого
контура; до фактического запуска их статус — NOT RUN. Merge/staging общий gate
и полная приёмка принадлежат #1031.
