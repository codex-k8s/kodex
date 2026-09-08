---
id: OPS-DOC-1258
title: Ограниченная выкладка runner и admission policy
type: runbook
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-08
---

# Ограниченная выкладка runner и admission policy

Источники: #1031, #1223, #1258, `GUIDE-DOC-003`, `GUIDE-DOC-006`,
`ARCH-MC-010` и решение владельца от 2026-09-08 об ускоренной выкладке
неиспользуемого disposable приложения с допустимым простоем. Этот документ
описывает процедуру, а не результат выполненной приёмки.

Новый application source не заменяет runner внутри Kubernetes Job или
закреплённого RoleImage. В данном профиле новый runner сначала становится
точной trusted base для следующей сборки. Полный пользовательский build,
scan, SBOM, provenance, admission, promotion, публикация и выбранный rebind
остаются обязательными. Импорт OCI archive сам по себе не доказывает эту цепочку.

## Границы и жизненный цикл

| Сценарий | Actor и authority | Владелец, эффект и отказ | Авторитетный readback |
| --- | --- | --- | --- |
| Подготовка | root SRE, явно выданный staging context | Чтение exact UID, spec, policy и owner DB; изменения отсутствуют | Private bundle, cluster/namespace UID, digests |
| Окно остановки | Тот же SRE, разрешение владельца | API временно получает replicas=0; прежнее число связано с digest bundle; Pod template и browser family не переписываются | Deployment UID/RV, отсутствие API replicas |
| Reader и пауза | Тот же SRE | Совместимый image-admission-controller принимает старое и versioned имя; `IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS=true` запрещает новые claim/promote cycles, но завершает существующие фазы | Ready controller, terminal Jobs, отсутствие рабочих PVC и pending owner states |
| Публикация базы | Отдельная существующая promotion identity | Только точный runner OCI digest; TLS с CA, точным SNI, client/application credentials; durable intent до import; UNKNOWN не повторяется автоматически | Registry digest readback; старые digest остаются доступны |
| Новая policy | SRE, Kubernetes RBAC/CAS | Новые immutable ConfigMap и Parameters, новая revision; старые ресурсы сохраняются. Catalog меняет только standard base | Полный canonical SHA256 payload и отдельные names/UID |
| Consumer switch | SRE, UID/resourceVersion CAS | VAP binding остаётся Deny; CP получает policy/base/catalog, builder base/catalog, controller точное имя. При новой работе или drift следующая фаза закрыто отказывается | Spec digest каждого изменённого ресурса, ready rollout |
| Resume | SRE после переключения consumers | Удаляется только прежнее точное read permission, новые циклы возобновляются; API получает исходное число replicas | Новые Job policy pins, прежние promoted artifact count/digest |
| Пользовательский RoleImage | Обычная проверенная owner session, существующие специализированные команды и grants | UI create/validate/build/admission/promotion/publish/rebind; версия, idempotency и effects принадлежат CP и существующим workers | Artifact/receipt/provenance/node pull и exact image новых runner Jobs |

У инфраструктурных фаз нет domain event: авторитетны Kubernetes resource
readback и owner DB. Существующие доменные commands, audit/outbox/inbox,
lease/claim/retry и terminal events не меняются. Неизвестный исход внешней
операции сохраняет intent; новый prefix не разрешает повтор эффекта.

Переход разрешён только при отсутствии открытых builds, pending admissions и
promotions, активных runs и claimed runtime leases. SQL выполняется read-only
с bounded timeout. После паузы не должно быть незавершённых admission Jobs и
рабочих PVC; terminal history удаляется только обычным lifecycle controller.
Новая работа блокирует дальнейший переход. Это профиль с простоем приложения,
он не доказывает конкурентную смену policy под build-нагрузкой.

`nodeReadbackImage`, runtime contract revision/digest, signer/trust,
credential generation и прежний runtime-controller default сохраняются.
Иначе старые bootstrap pins перестали бы проходить существующий exact image
rule. Новая пользовательская сборка получает обычный promoted `roles@digest`;
этот путь уже поддерживается runtime-controller. Существующие окружения и
attempts не перепривязываются автоматически. Default новых проектов можно
менять отдельной конфигурационной поставкой только после фактической
публикации нового artifact; данный инструмент default не меняет.

## Подготовка образов и публикация базы

Работа выполняется из точного доставленного checkout после PR/merge.
Следующая команда собирает и импортирует только новый reader image:

```bash
tools/dev/build-local-image-supply-chain.sh \
  --source-root "$SOURCE" --state-directory "$READER_STATE" \
  --component image-admission
```

В `READER_STATE/image-admission-image` находится exact reference. Остальные
authority, builder, tools и registry images эта команда не заменяет.
Новый runner собирается штатным runner image builder; его state содержит
`agent-runner-image` и exact OCI archive.

```bash
tools/dev/seed-local-image-supply-chain.sh --context "$CONTEXT" \
  --state-directory "$RUNNER_STATE" --tool-state-directory "$TOOLS_STATE" \
  --component runner --evidence "$NEW_PUBLICATION_EVIDENCE"
```

`TOOLS_STATE` содержит ранее подготовленное имя tools image; перед запуском
оно разрешается в immutable Docker image ID. Runner-профиль не публикует
control-plane helper, frontend или role-input и не требует нового общего render.
При UNKNOWN сначала выполняется точный readback без import:

```bash
tools/dev/seed-local-image-supply-chain.sh --context "$CONTEXT" \
  --state-directory "$RUNNER_STATE" --tool-state-directory "$TOOLS_STATE" \
  --component runner --readback-only --evidence "$NEW_READBACK_EVIDENCE"
```

## Переход конфигурации

Bundle готовится read-only по фактически выбранной controller policy и CP catalog:

```bash
node tools/release/runner-policy-transition.mjs prepare --context "$CONTEXT" \
  --runner-digest "$RUNNER_DIGEST" --output "$NEW_PRIVATE_BUNDLE"
```

Bundle/plan/evidence хранятся вне source с правами 0600 в private directory.
Имена файлов новые; старые FAIL/UNKNOWN не перезаписываются. Последовательность:
`maintenance`, `reader`, `resources`, `binding`, `control-plane`,
`role-image-builder`, `controller`, `resume`, `open`.

Для каждой фазы сначала отдельный read-only plan, затем его exact apply:

```bash
node tools/release/runner-policy-transition.mjs plan --context "$CONTEXT" \
  --bundle "$NEW_PRIVATE_BUNDLE" --reader-image "$READER_IMAGE" \
  --phase "$PHASE" --output "$NEW_PRIVATE_PLAN"
node tools/release/runner-policy-transition.mjs apply --context "$CONTEXT" \
  --bundle "$NEW_PRIVATE_BUNDLE" --reader-image "$READER_IMAGE" \
  --plan "$NEW_PRIVATE_PLAN" --evidence "$NEW_PRIVATE_EVIDENCE" \
  --confirm APPLY-STAGING-RUNNER-POLICY
```

После `reader` обычный controller завершает прежние циклы; готовность следующей
фазы проверяется новым планом. Timeout наблюдателя не означает завершение
команды: продолжить тот же handle. CAS drift не исправляется force.

```bash
node tools/release/runner-policy-transition.mjs inspect --context "$CONTEXT" \
  --bundle "$NEW_PRIVATE_BUNDLE" --output "$NEW_PRIVATE_READBACK"
```

Partial/UNKNOWN требует авторитетного readback конкретного шага. Совпадающие
уже созданные immutable ресурсы и частично расширенное exact read permission
распознаются при новом плане. Старый apply не повторяется. При ином состоянии
нужен совместимый исправляющий PR. Legacy resource names, schema, revocation,
generation floor и admission history не откатываются. После выбора versioned
policy возврат прежнего reader binary несовместим. API открывается только
после возобновления совместимых consumers; при незавершённой фазе простой
явно остаётся незавершённым, не объявляется успешной выкладкой.

## Проверки

Локальные entrypoints:

```bash
node --test tools/release/runner-policy-transition.test.mjs
GOMAXPROCS=2 GOWORK=off go -C services/jobs/role-image-builder test -p 2 -count=1 ./...
GOMAXPROCS=2 GOWORK=off go -C services/jobs/role-image-builder build -p 2 -o /dev/null ./...
GOMAXPROCS=2 scripts/tests/control-plane-postgres-test.sh '^(TestBootstrapComponent|TestWorkerGrantInstancesComponent)$'
bash -n tools/dev/build-local-image-supply-chain.sh
bash -n tools/dev/seed-local-image-supply-chain.sh
```

SQL readback включён в disposable PostgreSQL harness. Модель проверяет
corruption, точные names/owner, forward revision, сохранение старого helper,
паузы, Deny/RBAC, CAS drift и восстановление исходного числа API replicas.
Go tests проверяют продолжение прежних фаз при запрете новых циклов.
Фактические результаты команд и live операции фиксируются отдельно с SHA.
Полная CFG/runtime приёмка, активная mixed-policy нагрузка и обычные
независимые обновления/откаты остаются в #1031/#1223.

Актуальные документы проверены через Context7: Kubernetes ValidatingAdmissionPolicy
и RBAC (Deny при отсутствии Parameters, exact resourceNames), Docker buildx
OCI output и выбор build target. Секретные значения не входят в bundle или отчёт.
