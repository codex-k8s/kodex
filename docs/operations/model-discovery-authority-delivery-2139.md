---
id: OPS-DOC-MODEL-DISCOVERY-DELIVERY-2139
title: Поставка authority105→106 для каталога моделей
type: operation-guide
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-14
---

# Граница

Refs [#2139](https://github.com/codex-k8s/kodex/issues/2139),
[#1830](https://github.com/codex-k8s/kodex/issues/1830).
Путь использует существующие publisher, registry, durable rotation и sidecars.
Он не создаёт другую систему authority и не разрешает понижать high-watermarks.
Это поставка с техническим обслуживанием; отсутствие перерыва не обещается.

Прежний переход76→77 из `OPS-DOC-1390`/`OPS-DOC-1434` сохраняет свои точные
проверки и staging guards. Новый режим включается явно:
`--policy-transition MODEL_DISCOVERY_105_106`.
Он принимает только105→106, ABI2 и exact source bytes двух публичных fixtures.
Отредактированная policy, другая revision, неизвестное поле, duplicate key либо
произвольная «последняя policy» не становятся разрешённым переходом.

| Материал | Revision | SHA-256 исходных bytes |
| --- | --- | --- |
| Predecessor | 105 | `c63153741d7ed867cba59ac87fcfda3e2ca50e9c5d82bb1bc064ed62926332cf` |
| Каталог моделей | 106 | `bd5ca7ecd2bdaa9a0d7cc9d097e11437c52be6fb3de53181551cb35b3ae315be` |

Policy106 отличается от `source_revision` delivery registry и snapshot revision.
Следующая delivery revision выводится из фактического predecessor как `N+1`.
Номер106 не подставляется вместо delivery revision.

# Authority и карта исполнения

| Инициатор / источник полномочий | Вход и проверка | Авторитетный владелец / результат |
| --- | --- | --- |
| Оператор выбранной установки; Kubernetes RBAC | Явные `--kubeconfig`, `--context`, private portable target | `portableRunnerTarget`: cluster/ns UID, installation ConfigMap UID/hash, exact CP/publisher/registry UID |
| Тот же оператор | Чтение CP public OIDC config и его ConfigMap versions | `readAuthorityPolicyEnvironment`: issuer/audience принадлежат выбранному CP |
| Тот же оператор | Exact105 source → environment render → live predecessor bytes | Новый immutable plan связывает source/render hashes; несовпадение закрывает переход |
| Тот же оператор | Durable admission proof и fresh API/Service/EndpointSlices readback | Закрытый API admission, пригодный для миграции и запуска sidecars |
| Transition CLI | UID/RV/data CAS registry и UID/RV/full-spec CAS publisher | Одна атомарная замена registry+policy106; restart только publisher |
| Действующий publisher | Та же owner operation и immutable input digest | PostgreSQL intent/history; три публикации; все16 обязательных reader receipts |
| Projection CLI | Registry106, exact CPUID/spec/RV, predecessor immutable CM UID/hash | Новая immutable106 CM; изменяются только CP policy pointer arrays |
| Наблюдатель | Exact operation UUID и readback Job | Авторитетный rotation status; `UNKNOWN` требует resume/readback того же plan |

Operator CLI не превращает request-поля в identity. Целевой ресурс разрешается
через установку до OCC. Ни private plan, ни idempotency/UUID не заменяют target
authority. В public DTO, evidence и stdout нет kubeconfig, Secret values или ключей.
Ключи подчинённой интеграции Z.ai этим переходом не меняются.

Новые domain events не публикуются: lifecycle принадлежит существующим durable
rotation tables, snapshot history/readbacks и operator journal. Терминальный
read path — штатные `rotation-status`/`rotation-watch` exact owner operation.

# Environment render

Runtime policy должна содержать фактический OIDC issuer, а не
`__KODEX_OIDC_ISSUER__`. Разрешена только существующая подстановка issuer у трёх
producer profiles `control-api-gateway`; permissions, methods и остальные
поля остаются исходными. Issuer/audience читаются из выбранного CP и закрепляются
вместе с UID/resourceVersion public ConfigMap dependencies.

Read-only сверка `k3d-kodex` от14.11.2026 доказала точное равенство bytes:
source105 → environment render → live105 SHA-256
`c75051478e1cf011b60a718435d717dee4da6ab4ad69ad8a9505ffa9c419bfa1`.
Это объясняет отличие от source hash; не является доказательством поставки106.
Значение OIDC issuer не копируется из чужой установки или свободного CLI аргумента.

# Порядок поставки

1. Закрыть API через repo-owned maintenance plan и подтвердить positive join
   всех прежних writer Pods. Для043/044 закончить их собственные journals.
   Новый CP image каталога может пока читать проверенный policy105 file.
2. Подтвердить отдельную owner-bound изоляцию exact API Service и пустые
   EndpointSlices. Она позволяет запустить API Pods для issuer readbacks при
   закрытом внешнем admission. Нельзя удалять reader из required set или
   менять labels установки ради staging guards.
3. Подготовить exact clean source и private portable target. Прочитать current
   rotation/status, registry/snapshot pins и отсутствие незавершённого predecessor.
   Наличие production label у portable установки не изменяется.
4. Построить105→106 rotation plan. Одним CAS опубликовать следующую delivery
   revision и environment-rendered106. Выполнить bounded publisher-only restart
   и наблюдать тот же owner UUID. При неизвестном ответе читать состояние и
   продолжать `resume`, не создавать другое намерение.
5. Пока API admission закрыт, подготовить новую immutable106 projection и
   переключить CP policy pointer. Существующая immutable105 CM остаётся
   неизменной. Сохраняются application image, sidecars, replica count и остальные
   поля template. CP заново читает policy при запуске; он не hot-reload resolver.
6. Вернуть API Pods с уже допущенным application template, сохраняя изоляцию
   Service. Дождаться issuer/readiness и всех16 exact readbacks каждой phase.
   Штатные `DISTRIBUTING → WAITING_SWITCH → SWITCHING → WAITING_RETIRE → RETIRING
   → RETIRED` и оба40-секундных окна не сокращаются.
7. После `RETIRED` и проверки рабочего пути открыть только исходный Service
   selector. Открытие не восстанавливает прежний CP/API template, policy105 или
   прежние ключи. Ручной owner login и обычный tool turn фиксируются отдельно.

Required readers включают `control-api-gateway/AUTHORIZATION_ISSUER`.
Ожидать16 новых receipts при API replicas0 нельзя: такой порядок сам блокирует
публикацию. Закрытие admission и остановка Pods — разные проверяемые состояния.
Наблюдение без effects остаётся доступно и при работающих API Pods.

Новый CP binary не требует106 на startup: настоящий constructor/readiness и
прежняя worker operation проверяются с полной105 policy. Legacy resolver105
отклоняет discovery до owner resolution. Отдельный `service-v1` reader использует
встроенную policy106; поэтому до поставки внешнее закрытие API обязательно,
и105 file не выдаётся за общий запрет нового endpoint.

# Operator CLI

Private target использует существующий
`kodex.portable-runner-target/v1`; allowlist включает exact существующие
`control-plane`, `internal-rpc-authority-publisher`,
`internal-rpc-authority-publisher-target-registry`, Deployment и Service
`control-api-gateway`. Общий fingerprint всех5 ресурсов совпадает с maintenance
и traffic isolation proof. Файлы plan/evidence/profile
принадлежат оператору, regular, без hardlink/symlink и group/other permissions.
Все kubectl calls получают явные kubeconfig/context и request timeout.
Проверки Secret prerequisites читают только имена объектов.

```bash
node tools/release/authority-rotation-transition.mjs plan \
  --policy-transition MODEL_DISCOVERY_105_106 \
  --kubeconfig /private/operator-kubeconfig --context EXACT_CONTEXT \
  --portable-target /private/target.json \
  --source /exact/clean/source --revision EXACT_SHA \
  --action status --output /private/authority-status-plan.json

node tools/release/authority-rotation-transition.mjs plan \
  --policy-transition MODEL_DISCOVERY_105_106 \
  --kubeconfig /private/operator-kubeconfig --context EXACT_CONTEXT \
  --portable-target /private/target.json \
  --maintenance-plan /private/api-admission-plan.json \
  --maintenance-evidence /private/api-admission.jsonl \
  --traffic-plan /private/api-isolation-plan.json \
  --traffic-evidence /private/api-isolation.jsonl \
  --source /exact/clean/source --revision EXACT_SHA \
  --action rotate --output /private/authority106-plan.json

node tools/release/authority-rotation-transition.mjs apply \
  --kubeconfig /private/operator-kubeconfig --context EXACT_CONTEXT \
  --plan /private/authority106-plan.json --evidence /private/authority106.jsonl \
  --confirm APPLY-PORTABLE-CATALOG-AUTHORITY

node tools/release/control-plane-policy-projection.mjs plan \
  --policy-transition MODEL_DISCOVERY_105_106 \
  --kubeconfig /private/operator-kubeconfig --context EXACT_CONTEXT \
  --portable-target /private/target.json \
  --maintenance-plan /private/api-admission-plan.json \
  --maintenance-evidence /private/api-admission.jsonl \
  --traffic-plan /private/api-isolation-plan.json \
  --traffic-evidence /private/api-isolation.jsonl \
  --output /private/cp106-plan.json

node tools/release/control-plane-policy-projection.mjs apply \
  --kubeconfig /private/operator-kubeconfig --context EXACT_CONTEXT \
  --plan /private/cp106-plan.json --evidence /private/cp106.jsonl \
  --confirm APPLY-PORTABLE-CATALOG-PROJECTION
```

Оба helper поддерживают `resume` и `observe` того же plan/evidence; `observe`
получает также `--evidence`. Projection
после потерянного ACK сначала проверяет immutable CM UID и exact CP spec;
второй неизвестный ACK не разрешает новую CM с другим UID или иной template.
`APPLIED` projection означает pointer CAS/readback, а не завершённый Pod rollout,
успешный OIDC login или завершённую authority rotation.

Перед возвратом исходного Service selector выполняется отдельный fresh
`--action status` plan/apply с тем же source/context/kubeconfig. Его canonical
read-only CLI Job должен завершиться, а owner `observedAt` быть не старше30секунд.
`readCatalogAuthorityTerminal` проверяет exact operation UUID, registry/policy
hashes, все16 required readbacks, `RETIRED`, phase deadlines и source snapshot.
Job/Pod связываются по UID, полным container/init/ephemeral arrays и CRI digests.
Старый успешный watch Job не считается свежим DB query. Getter сам только
читает; создание status Job остаётся явной операторской операцией.

# Обновление immutable publisher image

Старый CLI ошибочно требовал `registryRevision = baseRevision + 1` для каждой
operation. Это верно при первой normal rotation, но последующие registry и
snapshot sequences растут независимо. Исправленный CLI оставляет durable
predecessor checks SQL owner и собственные bounded identity/state checks.
Первая105→106 может использовать квалифицированный старый publisher только при
подтверждённом owner state; следующая107 требует image с исправленным CLI.

`tools/release/authority-publisher-image.mjs` меняет только main publisher image
через UID/RV/full-spec CAS. Он сохраняет sidecars, volumes, policy/credential
pins и replica count. `READY` требует полного owned Pod inventory, approved
template/status/CRI digest и scheduling на квалифицированный Node.

Принимаются фактические `stable-image-publication/v2` plan и ровно
`INTENT/PASS` evidence вместе с canonical `build.json`. Перед plan/apply заново
проверяются public source archive и actual NodeUID/containerUID/CRI repoDigest.
Readonly `nodes` не выполняет pull; root/operator отдельно квалифицирует image.
CLI image не выбирается свободным override: последующие authority Jobs
используют exact image обслуживаемого publisher.

Build source и executor tools — отдельные pins. Для этого исправления build
source `5a516d6e1b15f560288605538e6e1f976e2545d3` сохранён в публичном
`work/authority-catalog-build-2139`. Это исторический build candidate с известной
лишней пустой строкой в synthetic fixture, без заявления deployment qualification.
Image plan дополнительно сохраняет actual clean executor path/SHA и отклоняет
apply/status другой версией tools. Смена executor не переписывает build provenance.

```bash
node tools/release/authority-publisher-image.mjs nodes \
  --source /exact/clean/build-source --revision BUILD_SHA \
  --publication-plan /private/authority-publication/plan.json \
  --publication-evidence /private/authority-publication/apply.jsonl \
  --build /private/authority-build/build.json --target /private/target.json \
  --kubeconfig /private/operator-kubeconfig --context EXACT_CONTEXT \
  --output /private/authority-node-proof.json

node tools/release/authority-publisher-image.mjs plan \
  --source /exact/clean/build-source --revision BUILD_SHA \
  --publication-plan /private/authority-publication/plan.json \
  --publication-evidence /private/authority-publication/apply.jsonl \
  --build /private/authority-build/build.json --target /private/target.json \
  --node-proof /private/authority-node-proof.json \
  --maintenance-plan /private/api-admission-plan.json \
  --maintenance-evidence /private/api-admission.jsonl \
  --traffic-plan /private/api-isolation-plan.json \
  --traffic-evidence /private/api-isolation.jsonl \
  --kubeconfig /private/operator-kubeconfig --context EXACT_CONTEXT \
  --plan /private/authority-image-plan.json

node tools/release/authority-publisher-image.mjs apply \
  --kubeconfig /private/operator-kubeconfig --context EXACT_CONTEXT \
  --plan /private/authority-image-plan.json --evidence /private/authority-image.jsonl \
  --approved-plan-sha EXACT_IMAGE_PLAN_SHA256
```

После `UNKNOWN` сначала выполнить `status` того же image plan/evidence.
Повтор apply допускается по подтверждённому BEFORE readback либо уже
подтверждённому after state; чужой UID/spec не принимается. Deployment revision
annotation принадлежит Kubernetes controller; только этот числовой annotation
не превращается в ложный drift после смены image. Остальные annotations точные.

# Проверка и восстановление

Публичная локальная точка входа — `make test-model-catalog-authority-delivery`.
Её synthetic fixtures не обращаются к кластеру или внешнему аккаунту.
Проверяются105 constructor/Ready/old signed proof, закрытый discovery legacy path,
exact source/render mapping, portable scope, registry CAS, сохранение immutable
predecessor и восстановление журналов после двух потерянных ACK.
Результаты Go module unit/race/vet/build и live readback указываются отдельно
с точным SHA. Отсутствие CI не является CI PASS.

После начала доставки не возвращать105 или старые keys под прежней revision.
Сохранить API admission закрытым и продолжать exact operation по readback.
Только действительно `PREPARED` без history/delivery допускает штатный abort;
`DELIVERING`, partial fan-out, `UNKNOWN`, `PROMOTED` и `RETIRED` не откатываются
обычным application rollback. Миграции043/044 и их journals остаются forward-only.

Дополнительные images для первой105→106 не требуются, если уже
обслуживаемый stable publisher/sidecars совместимы с ABI2 и обычным rotation
protocol. Publisher image/digest берутся из exact selected Deployment, а не
из свободного аргумента. Для продукта каталога обязательны новые application
images `control-plane`, `control-api-gateway`, `control-center`; прочие images
большего cohort принадлежат своим изменениям. Исправление CLI для последующих
rotations требует отдельно описанного нового `internal-rpc-authority` image;
sidecar ABI2 и DB schema от этого не меняются. Пересборка authority без причины
не подменяет source publication,16 receipts или фактическую готовность RPC.

Portable migration owner reads используют exact контейнер `postgresql`, очищенное
`env -i`, `psql -X`, локальный socket `/var/run/postgresql:5432` и явные database/user
без password prompt. `PGHOST`, `PGSERVICE`, startup environment и `.psqlrc` не
могут перенаправить precondition, drain или postcondition на другой backend.
Новый portable plan хранит строковые PostgreSQL system identifier/database OID;
каждый owner SQL query сверяет их и Pod UID до и после исполнения. План без этого
pin нужно подготовить заново; прежние исторические staging-планы сохраняют свой
формат, но также используют закрытый local-socket transport.
