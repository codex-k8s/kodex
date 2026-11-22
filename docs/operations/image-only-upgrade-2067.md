---
id: OPS-IMAGE-UPGRADE-2067
title: Обновление immutable images без повторного bootstrap
type: runbook
status: approved
owner: developer
version: 1.1.1
updated: 2026-11-22
---

# Обновление immutable images без повторного bootstrap

Контракт #2067 применяется, когда source/images изменились, а все три schema
heads остаются прежними. Это отдельный опциональный профиль; прежний
`kodex.bootstrap-upgrade/v1` по-прежнему означает forward migration. Профиль
не ослабляет image admission, bootstrap completion, native schema proof или
кластерную identity и не делает обновление автоматически совместимым.

В `kodex.bootstrap-upgrade/v2` обязательны:

- `upgrade_kind: SAME_SCHEMA_IMAGES`, `managed_by: git`;
- `previous_source_sha`, `target_source_sha`: разные exact40hex SHA;
- `previous_render_sha256`, `target_render_sha256`: exact64hex исходных bytes;
- `from_schema_heads`, `to_schema_heads`: одинаковый полный набор
  `control-plane`, `internal-rpc-authority`, `email-bridge`;
- `schema_profile: plain|backup`: явный профиль native proof.

Допускаются только schema heads, для которых установщик содержит точный
native catalog verifier. Unknown schema и дополнительные поля отклоняются.
Для schema051 (`20261121000200`) доступен профиль `backup`. Он выполняет
штатную проверку каталога PostgreSQL в транзакции READ ONLY и требует, чтобы
в истории Goose были подтверждены ART050 и schema051, а права backup-reader
соответствовали каноническому профилю.
Предыдущий render и completion receipt хранятся как защищённые operator files,
вне Git, с явными SHA256. Их нельзя восстанавливать из предположения об успехе.

Подготовленный render с Job `release-materializer-<40hex>` в `kodex-system`
не является входом новой установки. Оба установочных entrypoint отклоняют его
в режиме `apply` до inventory и изменений Kubernetes; проверка и последующее
исполнение используют один приватный снимок render. Обновление выполняется
через `upgrade` с проверенной completion receipt. Режим `plan` остаётся чтением.

## Порядок

1. Собрать полный совместимый release lock, сохранить текущие render/receipt,
   preimage и применимые backups. Подготовить новый render обычным renderer и
   явный profile с точными pins. Проверить plan до mutations.
2. Выполнить локальный `bootstrap_upgrade.py prepare --profile ...
   --previous-render ... --target-render ... --output ...` в private0700
   каталоге. Он сохраняет все пять прежних Job specs, включая старые image refs.
   Различия bootstrap command/args/env/volumes/identity отклоняются. Единственная
   нормализация — уже поддержанные значения migration backoff0/2 при сравнении;
   в выходе остаётся исходный spec, а не изменённый Job.
   Prepare также создаёт декларации Job `release-materializer-<source-sha>`
   и immutable ConfigMap его скрипта. Recipe SHA256 закрепляет release lock,
   Job и скрипт. SHA источника совпадает с областью promotion tags.
3. До rollout выполнить `bootstrap_upgrade.py capture --render ...
   --previous-render ... --target-render ... --previous-receipt ...
   --previous-receipt-sha256 ... --kubeconfig ... --context ... --output ...`. Capture проверяет
   точные байты выхода prepare, заново материализует его из тех же двух
   исходных render и записывает SHA256 подготовленного render. Затем сверяет
   прежний receipt, cluster/namespace UID, Job identities/spec, schema heads,
   native catalog proof и повторное чтение. Он не создаёт Job/Pod и не исполняет
   `up`. Job, удалённый по TTL, допускается только по подлинному pinned receipt.
4. Использовать stock `deploy-platform.sh --mode upgrade` с подготовленным
   render и новым `--bootstrap-completion-receipt`, точным
   `--bootstrap-completion-sha256`, явным
   `--allow-reconciled-bootstrap-completion` и остальными параметрами установки.
   `verify-apply` заново проверяет receipt, SHA256 всего подготовленного render
   и native schema до первой mutation. Если передана квитанция, подготовка
   admission, apply, resume и readback используют тот же приватный снимок
   render, который проверила их точка входа.
   Registry и Deployment publisher сверяются с этим снимком до записи.
   Смена образа publisher выполняется штатной процедурой
   `tools/release/authority-publisher-image.mjs`. Подготовленный render
   должен закреплять уже действующие registry data, rotation markers и spec
   publisher.
   До записи rollout и непосредственно перед запуском проверяются выбранный
   Job, его script ConfigMap и завершение прежних materializer writers.
   Повтор одного release использует прежний Job. Другая recipe для того же SHA,
   конфликт script ConfigMap или неподтверждённое завершение прежнего writer
   закрывают допуск. Прежний Job и его скрипт остаются в истории.
   Сохраняйте Job и его script ConfigMap, пока используется соответствующий
   release source. Если Job удалён вне установщика, перед повтором вручную
   сверьте публикацию артефактов в registry.
   Все пять bootstrap Jobs пропускаются; штатный materializer и остальные
   rollout/readback gates остаются обязательными.
5. Проверить actual CRI digests, readiness, данные и функции. Сохранить новый
   render/receipt для следующего обновления; повторный readback не выдаёт
   нового migration Job UID и не пересоздаёт завершённые effects.

## Жизненный цикл и границы

| Переход | Полномочия и authoritative source | Результат / отказ |
| --- | --- | --- |
| prepare | Operator + Git-managed exact profile и два render | Новый render сохраняет пять прежних effects; mismatch закрыто отклоняется, API вызовов нет |
| capture | Оба исходных render + прежний pinned completion + текущие Kubernetes UID/spec и native SQL catalog | Новый `SAME_SCHEMA_IMAGE_UPGRADE` receipt сохраняет исходные Job UID/timestamps и закрепляет подготовленный render; только чтение |
| verify/apply | Stock operator lifecycle + новый receipt/profile, свежий native proof и точная пара registry/publisher | Bootstrap не повторяется; пара authority остаётся в области записи штатной ротации |
| materializer | Release source, immutable recipe и текущие Job/Pod identities | Точный Job запускается либо используется повторно. Прежний незавершённый writer или конфликт того же source останавливают rollout |
| TTL / повтор / второе обновление | Прежняя цепочка receipt, original cluster/namespace и fingerprints | Сохраняются те же effects; отсутствие доказательств не заменяется успехом |
| Drift / crash / неизвестный исход apply | Actual readback и сохранённый operation journal | Нет автоматического повторного apply либо новых bootstrap effects |

Квитанция закрепляет предыдущие receipt/render, текущие profile и подготовленный
render, а также native schema proof. Исходные Job UID сохраняются. Для квитанции
смены образов без `prepared_render_sha256` повторите capture из сохранённых
исходных render и предшествующей квитанции. Эта операция выполняется только
чтением. Forward migration сохраняет собственный формат квитанции.
Для старого подготовленного image upgrade с фиксированным именем materializer
повторите prepare и capture по исходным pins. Прежние квитанции bootstrap и
завершённые Jobs сохраняются. Fresh и forward профили используют фиксированное
имя своего materializer.

## Проверка и возврат

Для сверки уже завершённой schema051 используется операторский read-only
вход. Передайте приватные планы и журналы migration и API restore. После
каждого файла укажите SHA256 из сохранённой записи операции.

```sh
node tools/release/component-migration-archive.mjs CONTEXT /path/to/kubeconfig \
  /path/to/migration.json MIGRATION_PLAN_SHA256 \
  /path/to/migration.jsonl MIGRATION_JOURNAL_SHA256 \
  /path/to/api-restore.json RESTORE_PLAN_SHA256 \
  /path/to/api-restore.jsonl RESTORE_JOURNAL_SHA256
```

Сверка связывает исторический Job/Pod через ранее принятый terminal API
restore, текущие UID и spec Job/Service/Deployment, а также живой каталог
PostgreSQL. Замена PostgreSQL Pod допускается при прежних StatefulSet и
backend identity. Новый Pod фиксируется на всё чтение. Текущий Job должен
ещё присутствовать в Kubernetes. Выход сохраняется в приватный файл и служит
исходным доказательством для отдельного допуска мигрированной установки.
Штатные apply и resume по-прежнему проверяют собственные квитанции и гейты.

Сводное наблюдение для установки, обновлявшейся отдельными миграциями,
выполняет `tools/release/component-migration-adoption.mjs CONTEXT KUBECONFIG
DOSSIER_PATH DOSSIER_SHA256`. В dossier с типом
`kodex.component-migration-adoption-inputs/v1` входят пары `path/sha256`
для `migrationPlan`, `migrationJournal`, `restorePlan`, `restoreJournal`,
`rotationPlan`, `rotationJournal` и `storageContext`. Все входы читаются
как приватные operator files. Сверка заново наблюдает три schema heads,
authority110, привязку к PVC/PV до инцидента и право artifact-retention.
Выход хранит отдельно декларацию текущего installation ConfigMap и живые
schema heads, а также точные исторические Job/Pod, источник миграции и
terminal hashes. Storage proof относится к data-томам PostgreSQL control-plane.
Приём bootstrap-квитанций сохраняет исходные пять строк отдельно от этой
сверки. Разрешение rollout выдаётся штатными гейтами установки.

```sh
python3 -B tools/install/bootstrap_migration_custody.py \
  --context CONTEXT --kubeconfig /path/to/kubeconfig \
  --previous-render /path/to/original.yaml --previous-render-sha256 ORIGINAL_RENDER_SHA256 \
  --previous-receipt /path/to/original.json --previous-receipt-sha256 ORIGINAL_RECEIPT_SHA256 \
  --dossier /path/to/dossier.json --dossier-sha256 DOSSIER_SHA256 \
  --output /private/custody.json
```

Команда создаёт новый файл в приватном каталоге. Сохраняются прежние Job UID,
архивная декларация, текущая декларация и наблюдённые schema heads. Формат
`kodex.bootstrap-migration-custody/v2` предназначен для проверяемого
предобраза. В нём записаны отдельные дайджесты каталога БД и устойчивой
идентичности authority. Последний сохраняет связи с owner, registry, publisher
и backend БД и позволяет сравнивать повторно наблюдённое состояние при изменении
времени наблюдения и числа уже ответивших optional readers. Для повторной
сверки укажите `--expected-custody` и `--expected-custody-sha256` прежнего
снимка, а в `--output` новый файл.
Перед подачей rollout установщик запрашивает собственный допуск.

Для read-only проверки конкретного нового релиза запустите
`tools/install/bootstrap_migration_adoption.py` с точными `--context` и
`--kubeconfig`. Передайте файлы `--archive-render`, `--archive-receipt`,
`--expected-custody`, `--target-render` и `--dossier` вместе с их SHA256.
Укажите в `--release-lock` полный lock нового OpenCode-релиза. Команда
проверяет его хеш из манифеста, schema heads и digest образа materializer
штатным валидатором поставки.
Команда сверит три фактические схемы, пять исторических Job, действующую
authority и materializer нового релиза. В приватном `--output` будут хеш
подготовленного манифеста и хеши декларации установки до и после перехода.
План имеет `apply_granted=false` и предназначен для проверки перед rollout.

Для подготовки к обновлению повторите команду с теми же закреплёнными входами
и укажите `--verified-plan`, `--verified-plan-sha256` и новый приватный
`--capsule-directory`. Записываются `render.yaml` и `receipt.json`. Квитанция
содержит пять исходных Job, хеш полного render и привязку к архиву миграции.
Команда повторно читает фактическую схему и текущий стенд до выдачи файлов.

Передайте файлы и объявленные SHA256 в штатный запуск
`portable-release.sh --mode upgrade`. Укажите `--bootstrap-completion-receipt`,
`--bootstrap-completion-sha256`, `--allow-reconciled-bootstrap-completion` и
`--migration-adoption-dossier`. Для этого перехода обязательны явные
`--context` и `--kubeconfig`.

| Переход | Live preconditions | Результат |
| --- | --- | --- |
| Begin | Исходные UID установки, API Service и Deployment, прежний либо точный целевой spec каждого объекта, прежние schema/authority/storage и все пять bootstrap-квитанций | Допуск штатных этапов rollout. Неизвестное состояние закрывает допуск |
| Commit | После чтения БД и архива подтверждены готовность всех controllers, завершение materializer, UID и resourceVersion прежней установки | Один JSON CAS обновляет декларацию установки |
| После потерянного ответа | Повторно проверены новая декларация, весь render, API readiness и источники сохранности | Фиксируется подтверждённый результат либо закрытый отказ для сверки |
| Повтор завершённого обновления | Фактическая декларация и обслуживающий API соответствуют целевому релизу | Выполняется readback без повторной подачи Job |

Сверка PostgreSQL закрепляет базу, StatefulSet и том. Замена Pod того же
StatefulSet допустима при том же backend и неизменном каталоге. Отдельный
readback сравнивает текущие Service и Deployment с закреплёнными состояниями и
проверяет происхождение миграционного Job. Перед продолжением после дрейфа
объекта за пределы двух состояний нужен авторитетный readback.

`python3 -B tools/install/test_bootstrap_image_upgrade.py` проверяет сохранение
всех пяти identities, TTL, два последовательных обновления, native proof,
подмену render/spec/receipt/UID/schema/source и late replacement. Проверка schema051
вызывает штатный JS-валидатор и отклоняет неподтверждённый ART050 и дрейф
каталога или прав backup-reader. Контур включён в
`make test-web-only-release`; прежние forward/fresh suites сохраняются.

Локальный synthetic контракт не доказывает успешного установленного обновления.
Старый image безопасен для возврата только после отдельной проверки API/schema/
data compatibility. Этот профиль не выполняет downgrade БД, не выдаёт restore
acceptance и не заменяет functional smoke нового immutable release.
