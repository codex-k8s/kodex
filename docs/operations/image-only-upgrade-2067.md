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
   --previous-receipt-sha256 ... --context ... --output ...`. Capture проверяет
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

`python3 -B tools/install/test_bootstrap_image_upgrade.py` проверяет сохранение
всех пяти identities, TTL, два последовательных обновления, native proof,
подмену render/spec/receipt/UID/schema/source и late replacement. Контур включён в
`make test-web-only-release`; прежние forward/fresh suites сохраняются.

Локальный synthetic контракт не доказывает успешного установленного обновления.
Старый image безопасен для возврата только после отдельной проверки API/schema/
data compatibility. Этот профиль не выполняет downgrade БД, не выдаёт restore
acceptance и не заменяет functional smoke нового immutable release.
