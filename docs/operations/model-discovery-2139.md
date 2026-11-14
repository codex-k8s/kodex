---
id: OPS-MODEL-DISCOVERY-2139
title: Поисковый каталог моделей и Z.ai Coding Plan
type: operations
status: approved
owner: manager
version: 1.0.2
updated: 2026-11-14
---

# Поисковый каталог моделей и Z.ai Coding Plan

Задача [#2139](https://github.com/codex-k8s/kodex/issues/2139), родитель
[#2108](https://github.com/codex-k8s/kodex/issues/2108). Каталог для поиска
поставляется отдельно от `kodex.model-provider-catalog/v1`, который задаёт
только разрешённые маршруты установки. Импорт не создаёт accounts, credentials,
runtime profiles или grants и не меняет выбранные модели сотрудников.

## Сквозная карта

| Сценарий | Полномочия и путь | Состояние и результат |
| --- | --- | --- |
| Найти модель | Авторизованный пользователь → `GET /api/v1/model-discovery` → `PlatformQueryService.ListModelDiscovery` → server-resolved principal → прежнее `organization.view` в repository `ListProviderDefinitions` → закреплённый локальный снимок | Фильтры query/providerId/family/minimumContextTokens/inputModality/outputModality/tools/reasoning/cache, grouping PROVIDER/FAMILY и pageSize/pageToken не несут authority. Возвращаются безопасные metadata и пять отдельных измерений. Credentials и внешний model API не читаются |
| Следующая страница | Тот же путь с новым transport context; cursor связан со всеми фильтрами и grouping, source snapshot, custom/retired bytes и exact configured route digests | Изменение любой привязки закрыто отклоняет прежний cursor. Current organization eligibility проверяется заново |
| Обновить импорт | Developer в ветке Issue получает bounded public artifact и проверенный schema source, передаёт exact SHA-256 и source revision утилите обновления | Перезаписываются только snapshot, manifest и список удалённых upstream записей. Custom overrides и сохранённые selections остаются. Обновление поставляется обычным PR/immutable image |
| Удалённая upstream модель | Обновление сохраняет прежние metadata в retired-models; поиск выдаёт `REMOVED`/`UNAVAILABLE` | Новый alias или provider не подставляется. Сохранённая owner configuration не переписывается импортом. Любая смена исполняемой конфигурации проходит прежние команды |
| Пользовательская модель | Отдельный Git-owned `custom-overrides.json` либо точный действующий owner descriptor отсутствующей в upstream модели | `CUSTOM` отделён от upstream; произвольные SDK не устанавливаются. Выбор маршрута и допуск аккаунта остаются прежними |
| Подключить Z.ai | Operator создаёт preset с exact Coding Plan endpoint и точными address pins; прежние installer render/bootstrap, безопасный API-key input и broker account observation | Техлид/ревью используют `glm-5.3`, разработчик — `glm-5.3-flash` после явного выбора профиля. Подписка и реальный запуск не подтверждаются наличием preset |

Чтение не создаёт idempotency receipt, version transition, task, lease, retry,
outbox event или consumer effect. Delete/cancel/retry/terminal для самого
статического поиска неприменимы. Закрытие страницы и смена owner lifetime
отменяют чтение; поздние ответы не меняют новый owner state. Исполняемые
Session/Turn/Run, credential generations и их lifecycle остаются прежними.

Новый RPC зарегистрирован как `platform.query.model-discovery.list` для exact
`control-api-gateway` → `control-plane` с `USER_CREDENTIAL_REQUIRED` и
`UNARY_PROTO_SHA256`. Producer/client operation registry, service classification,
Proto/OpenAPI и generated clients содержат тот же метод. Service readiness
проверяет тем же loader, что и рабочий поиск, hash распакованных assets.
Внешний listener, job, secret delivery и сетевое разрешение не добавляются.
Assets входят в прежний Docker input `services/internal/control-plane`.

## Пять независимых измерений

| Измерение | Значение и предел доказательства |
| --- | --- |
| Известна | `KNOWN` — есть в pinned artifact; `CUSTOM` — отдельно сопровождаемая запись; `REMOVED` — сохранена после удаления upstream |
| Адаптер | `BUNDLED` означает наличие одного из трёх закреплённых SDK/протоколов. Это не квалификация конкретной модели. Другие SDK — `UNSUPPORTED`; npm из каталога не исполняется |
| Аккаунт | `AVAILABLE` означает наличие хотя бы одного текущего eligible account этого exact endpoint/protocol/model route, а не проверку выбранного пользователем account или успешного turn. Отсутствие наблюдения — `UNKNOWN`. Сам import не подтверждает подписку |
| Разрешено | Без включённого exact route либо для retired записи — `UNAVAILABLE`; при наличии маршрута — `UNKNOWN`, поскольку поиск не имеет контекста agent/grant/Run |
| Проверено | `NOT_RUN`: эта read projection не извлекает model execution attestation. Synthetic adapter tests не превращаются в подтверждение живого аккаунта |

Сохранённый exact descriptor задаёт `routeConfigured=true` независимо от
включения provider и доступности account. Отключённый provider либо наблюдаемая
модель без eligible account дают account `UNAVAILABLE`; отсутствие наблюдения —
`UNKNOWN`. Ни одно из этих состояний не подтверждает выбранный account или turn.

Для каждого запроса полностью перечитываются до4096 owner definitions страницами
не более100 через прежний авторитетный repository path. Переполнение бюджета,
повтор definition/cursor, пустая нетерминальная страница либо отказ authority
отклоняют весь ответ без частичных route/account evidence. Каждый repository read
заново проверяет organization eligibility; observation account не кешируется.

RPC зарегистрирован атомарно в policy revision106: gateway operation profile,
OIDC proof producer permission, exact method/request profile и target-owned
service-identity policy. Генераторы `policygen` и `service-identity-policy.mjs`
материализуют обе стороны; тест `TestModelDiscoveryGeneratedAuthorityMapping`
использует фактическую generated policy, настоящий admission/middleware/resolver
и синтетический verified TLS peer. Это локальная проверка mapping, не live TLS/SSO.

Каталог не создаёт линейную «лестницу готовности». Даже account `AVAILABLE`
не разрешает Run. Реальные `eligibleForSelection`/`allowedToSubmit`, текущие
grants, immutable runtime revision, budgets и очередь проверяются прежним
контекстным путем запуска. Поиск не имеет кнопки, которая обходит этот путь.

## Источник и обновление

Поставляемый import содержит 236 providers и9726 моделей из
[`models.dev` experimental v2](https://models.dev/_experimental_v2.0_api.json),
MIT. Проверенная source revision
`a5458bdc8326d995619413b54425db97e911f4d4` закрепляет
[`schema-v2.ts`](https://github.com/anomalyco/models.dev/blob/a5458bdc8326d995619413b54425db97e911f4d4/packages/core/src/schema-v2.ts).
`source_revision` означает проверенный исходник; сборка upstream artifact из
этого Git tree локально не воспроизводилась. Exact artifact и schema имеют
собственные hashes; так связь не выдаётся за воспроизведённую сборку.

Семейство, входные/выходные MIME modalities, known flags для размеров контекста и ответа, три состояния tools/reasoning, отдельные implicit/explicit cache states и summary cache kind импортируются явно. Отсутствующие сведения остаются `UNKNOWN`, а отсутствующий limit — `contextKnown=false` либо `outputKnown=false`. Фильтр UNSUPPORTED не включает UNKNOWN; cache metadata не включает кеш Kodex.

Группировка FAMILY упорядочивает весь результат сервером, включая переходы страниц; интерфейс объединяет загруженные модели по семействам без смешения provider/model identities. Форма хранит отдельно применённые фильтры, поэтому редактирование полей не меняет scope уже выданного cursor.

`data/manifest.json` хранит `schema_sha256`, `upstream_sha256` и
`snapshot_sha256`; последний проверяется перед использованием metadata. `retrieved_at` фиксирует время получения exact artifact; текущий import получен `2026-11-14T04:03:01.859067Z`. Время не является live account observation.
API также выдаёт `overlaySha256` от custom/retired bytes и `catalogSha256`
от пары exact manifest/overlay hashes (manifest закрепляет snapshot, schema и provenance); интерфейс показывает этот итоговый digest.
Проекция намеренно не исполняет upstream SDK/body/headers/experimental options.
Её закрытые схемы — `contracts/model-provider-discovery/v1/snapshot.schema.json` и `manifest.schema.json`. Их точные копии поставляются в assets; projection schema digest проверяется при загрузке. Все manifest/snapshot/custom/retired документы проходят duplicate-key rejection и полную local schema validation до typed decode, затем проверяются уникальность provider/model и protocol identities, semantic capability consistency и общий count. Один явный custom override может заменить upstream запись; повтор identity внутри одного overlay запрещён. Сетевой загрузчик схем отключён.

`schema_sha256` относится к проверенному TypeScript исходнику upstream. Он закреплён в реестре поддержанных schema sources, но полный upstream JSON не валидируется через Zod: manifest/API явно содержат `upstream_schema_validation=NOT_PERFORMED`. Машинно проверяется только полная закрытая проекция Kodex, обозначенная `projection_schema` и `projection_schema_sha256`; менять проверенную upstream схему без отдельной квалификации нельзя.
Текущая схема upstream не заменяет OpenCode1.19.5 ABI5/input10: SDK остаются
`@ai-sdk/openai@3.0.88`, `@ai-sdk/anthropic@3.0.111`,
`@ai-sdk/openai-compatible@2.0.41`.

Обновление выполняется offline после отдельного bounded получения artifact
и закреплённой схемы, без загрузки credentials:

```bash
python3 tools/dev/model-provider-catalog-refresh.py \
  --input /absolute/path/models-dev-v2.json \
  --revision <проверенная-40-hex-ревизия> \
  --schema-input /absolute/path/schema-v2.ts \
  --expected-input-sha256 <64-hex> \
  --expected-schema-sha256 <64-hex> \
  --retrieved-at <UTC-RFC3339-время-получения-artifact>
```

Размер каждого входа ограничен16MiB; duplicate JSON keys, неверные identity,
повреждение прежнего снимка и неподходящий hash закрыто отклоняются. Новая
схема upstream требует review проекции; обновление не загружает пакет по имени
из metadata. Partial write snapshot/manifest закрывает readiness, а не
возвращает смешанный каталог. Rollback — предыдущий согласованный immutable
image/snapshot; сохранённые owner selections и custom overrides не удаляются.

## Z.ai Personal Coding Plan

International Coding Plan использует только
`https://api.z.ai/api/coding/paas/v4` и `CHAT_COMPLETIONS` в данном preset.
`https://api.z.ai/api/paas/v4` является отдельным API с иной тарификацией и не
становится fallback. OAuth/подписка другого runtime не конвертируются в key.

```bash
python3 tools/install/model-provider.py preset-zai-coding-plan \
  --source-revision <проверенная-40-hex-ревизия> \
  --address-cidr <проверенный-точный-IP/32-или-IPv6/128> \
  --output /absolute/path/provider.json
```

Output создаётся через exclusive/no-follow0600; существующий файл не
перезаписывается. Затем этот файл передаётся прежнему installer flag
`--model-provider-configuration`. Секрет вводится только прежним безопасным
API-key path из private owned file либо штатным UI авторизации аккаунта.
В preset нет ключей, npm override, cache hints или managed caching.
Default `glm-5.3`; `--default-model glm-5.3-flash` разрешает явный другой default.
Установка не меняет модели уже существующих сотрудников.

Conservative owner ceilings: context1000000, output32768. Это operator limits,
а не remote quota attestation. Доступные reasoning efforts в справочнике не
расширяют runtime preference allowlist. Preset сохраняет provider default,
не отправляет `thinking.disabled` либо неподтверждённый `max` через общий overlay.

Настоящий закреплённый OpenCode прошёл synthetic wire-проверку обоих aliases:
точный Coding Plan path, два model requests и один managed tool effect на alias,
полный `reasoning_content` на продолжении, отсутствие cache hints, fallback и
посторонних model calls. Штатные поля upstream —
`thinking={type:enabled,clear_thinking:false}`; это documented preserved thinking,
не дополнительный кеш Kodex. Такой тест не доказывает доступность живого account.

Через Context7 проверены `/anomalyco/models.dev` (experimental v2 API и per-API schema), `/websites/z_ai` (Coding Plan endpoint, key/billing
граница, GLM5.3), `/vuejs/core` (scope cleanup и stale async result). Официальные
источники: [GLM5.3](https://docs.z.ai/guides/llm/glm-5.3),
[подключение инструментов](https://docs.z.ai/devpack/tool/others),
[управление keys](https://docs.z.ai/devpack/notice/api-key-management).
Latest документация не является доказательством ABI установленного бинарника.

## Ручная проверка

1. В авторизованной PWA открыть страницу аккаунтов моделей и каталог поиска.
   Найти `glm-5.3`, убедиться, что провайдеры разделены, оба Coding Plan aliases
   видны, все пять измерений показаны отдельно, source revision/hash доступны.
2. Выбрать family `glm`, минимум контекста1000000, input/output `text`, tools/reasoning `SUPPORTED`, cache `IMPLICIT`; проверить точный результат. Переключить grouping FAMILY и пройти следующую страницу. Отдельно найти cache `UNKNOWN`, убедиться, что UNKNOWN не выдан за UNSUPPORTED. Изменить поля фильтра до загрузки следующей страницы: пагинация использует прежний применённый набор; новый поиск применяет новые значения.
3. Без настроенного маршрута проверить отсутствие готовности запуска.
   Поиск не вызывает `/models` или inference у upstream.
4. Создать preset с тестовыми exact pins, выполнить безопасный render,
   сверить endpoint в CP/broker/egress; не применять его к неподтверждённому target.
5. На отдельно разрешённой установке подключить собственный API key, дождаться
   credential-bound catalog, явно назначить модели сотрудникам и выполнить
   ограниченный обычный tool turn. Живой результат записать отдельно по exact
   SHA/image/account generation; import и synthetic wire не заменяют эту проверку.
6. Проверить поиск после обновления pinned source, сохранность custom override
   и прежнего model selection; удалённая upstream модель остаётся видимой как
   недоступная, другая модель автоматически не выбирается.

Cluster apply, live account observation, paid inference и browser E2E данного
среза не выполнены. Значения секретов в коде, документации и evidence отсутствуют.
