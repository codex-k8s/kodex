---
id: OPS-DOC-1262
title: Пользовательская API-приёмка RoleImage и forward restore
type: acceptance-runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-08
---

# Приёмка RoleImage

Источники: #1262, #1031, #1223, CFG-01 и матрица PR #1245;
[переход runner policy](runner-policy-release.md), `GUIDE-DOC-006`,
`GOV-DOC-003`, OpenAPI `control-api-gateway/v1` и существующие команды PWA
`managed-configurations/api.ts`. Этот документ описывает процедуру,
а не утверждает, что новые образы уже собраны или проверены на staging.

`tools/dev/role-image-acceptance.mjs` использует существующий
`owner-session-client.mjs`: легитимные cookie/CSRF, естественный refresh и
принятие обновлённых cookies. QA не получает SSH, kubeconfig, PAT владельца
или provider credentials. Нужна отдельная private API storageState с доступом
к созданию проекта и управлению его agent, image source/build/promotion и
runtime environment. Одну session family не используют параллельные drivers.

Перед GO root завершает новую trusted base/policy и передаёт URL, fresh session
и безопасный manifest фактически обслуживаемых компонентов. Manifest является
отдельным SRE evidence: driver закрепляет SHA256 его точных bytes, но не выдаёт
чтение файла за проверку Kubernetes. State/session/manifest находятся вне source,
в private каталоге 0700, файлы 0600. Checkout должен быть чистым, exact source SHA
записывается в первый journal event и сохраняется между фазами.

## Последовательность и границы

| Фаза | Команда пользователя и владелец состояния | Доказательство и граница |
| --- | --- | --- |
| `prepare` | Новые Project/Agent; GET role-environments; UI ROLE_IMAGE draft → VALIDATE → PUBLISH | Actor из session; проект/роль разрешаются CP. PUBLISH сам атомарно создаёт build; дополнительный REQUEST_BUILD не отправляется |
| Build/admission | Worker claim → trusted base/build/scan/SBOM/provenance → admission | API candidate обязан соответствовать recipe/generation/configurationRevisionRef и COMPLETED build; обязательны provenance/SBOM/vulnerability digests |
| Promotion | POST recipe/promotions с exact artifact/provenance и If-Match | Readback требует ACCEPTED, promoted roles@digest, exact active artifact и promotion receipt SHA256 |
| Первое назначение | Новое runtime environment с admitted artifact; PUT agent/runtime-environment-binding | Проверяются exact artifact/reference и binding.versionRef текущей environment version. Прежние fixture ресурсы не меняются |
| `advance` | Новая forward UI revision того же recipe; к синтетическому Dockerfile добавляется фиксированный комментарий | Отдельные validate/publish/build/promotion. До publication published pointer сохраняется; до rebind прежняя agent binding остаётся точной |
| Выборочный rebind | PREPARE impact → все страницы → POST consumer-bindings | План обязан содержать минимум environment и agent; выбираются только новый fixture project/environment. При наличии чужих items закрытый отказ. Требуются APPLIED у всех выбранных items и итоговый exact artifact |
| `restore` | Чтение самой первой исторической revision → новый draft с тем же исходником | Никакой искусственной правки исторического source. Новый ref/монотонная revision/parent, прежний published pointer и pins; отдельный новый build/promotion и выборочный rebind |
| `inspect` | Только GET доступных owner projections | Показывает unresolved intent, matching projects/configurations/recipe/build states и историю без source. Не повторяет команду и не объявляет UNKNOWN успешным |

Источники authority, специализированные RPC, audit и domain events не меняются.
Owner transaction остаётся единственным владельцем revision/build/receipt/grant.
Для terminal/cancel/expiry driver читает состояние и останавливается; cancel,
retry, archive, delete или repair автоматически не вызываются. Существующие
immutable artifacts, history и pins не очищаются. `advance` и `restore`
каждый создают отдельную сборку: запускать их после принятого результата
предыдущей фазы, с собственным bounded бюджетом.

## Команды

После явного GO root; shell variables ниже содержат только пути, URL,
проверенный digest и новый безопасный prefix:

```bash
node tools/dev/role-image-acceptance.mjs prepare \
  --origin "$QA_ORIGIN" --storage-state "$QA_STORAGE_STATE" \
  --state "$QA_ROLE_IMAGE_JOURNAL" --prefix "$QA_FIXTURE_PREFIX" \
  --runner-digest "$EXPECTED_RUNNER_DIGEST" \
  --serving-manifest "$QA_SERVING_MANIFEST" --timeout-ms 1200000 \
  --confirm APPLY-STAGING-ROLE-IMAGE-FIXTURE
```

Для следующих фаз заменить только `prepare` на `advance`, затем `restore`.
Budget 60–1800 секунд на фазу, отдельный HTTP timeout не более 30 секунд;
GET polling ограничен общим сроком. Все операции выполняются последовательно.
Новый prefix нужен для нового fixture; он не разрешает повтор неизвестного
эффекта прежней попытки. Повтор уже завершённой фазы возвращает сохранённый
результат без новой проверки и без новых mutations.

При неизвестном исходе выполнить ту же команду с `inspect`, без `--confirm`:

```bash
node tools/dev/role-image-acceptance.mjs inspect \
  --origin "$QA_ORIGIN" --storage-state "$QA_STORAGE_STATE" \
  --state "$QA_ROLE_IMAGE_JOURNAL" --prefix "$QA_FIXTURE_PREFIX" \
  --runner-digest "$EXPECTED_RUNNER_DIGEST" \
  --serving-manifest "$QA_SERVING_MANIFEST" --timeout-ms 60000
```

Журнал append-only: fsync intent **до** отправки, ACK с whitelist metadata после
проверки ответа. Request body, Dockerfile, raw response/error и credentials
не записываются. HTTP 400/401/403/404/409/412/422 классифицируются REJECTED,
остальные неоднозначные ответы/ошибки — UNKNOWN. В обоих случаях новые mutations
блокируются до отдельного решения по owner readback; скрытого повторения нет.
Старый helper повторял тот же idempotency key внутри request; само это не
объявляется дефектом owner receipt. Новый driver дополнительно сохраняет intent
между процессами и проходит UI-managed draft/validation/publication.

Одновременно писать journal нельзя: exclusive lock. После аварии оставшийся
lock не удаляется автоматически; root сначала доказывает отсутствие живого
процесса. `inspect` открывает существующий journal read-only. Повреждение,
обрезанный хвост, symlink, доступ группы/других или другая source/origin/manifest
закрыто отклоняются. Не редактировать журнал для превращения UNKNOWN в PASS.

## Что ещё проверяется отдельно

API-проекция с digests доказывает owner readback соответствующих records.
Root отдельно связывает их с реальными scanner/SBOM/provenance/signature,
promotion и node-pull Jobs, UID и фактическим registry digest. Отдельный
разрешённый пользовательский Run должен доказать RuntimeRevision и imageID
нового runner Pod. Driver не запускает платный provider и не подменяет Run
canary/fixture; его output явно сохраняет `runtimeJob: NOT_RUN`.

Браузерный Dockerfile editor/history/restore, права source, SHIPPED/GIT copy,
archive, NOT_SELECTED/CONFLICT/FORBIDDEN варианты impact, revoke, no-secret
negatives и другие CFG-01/02/03 сценарии остаются в #1031/#1223. Поле
`browserUI: NOT_RUN` не позволяет выдать API-путь за UI-приёмку.

Локальные проверки:

```bash
node --check tools/dev/role-image-acceptance.mjs
node --test tools/dev/role-image-acceptance.test.mjs tools/dev/owner-session-client.test.mjs
git diff --check
```

Тесты проверяют lost response/restart без нового effect, exact ACK, typed412 и
UNKNOWN503, private journal/corruption/concurrency, read-only inspect, полную
managed последовательность и отказ без exact completed build/SBOM/scan/promotion.
Fixture tests не заменяют staging. Актуальные Node.js fs exclusive open/write/fsync
проверены через Context7 `/websites/nodejs_latest-v24_x_api`.
