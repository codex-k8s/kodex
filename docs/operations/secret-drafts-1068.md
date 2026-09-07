---
id: OPS-SECRET-DRAFTS-1068
title: Защищённые черновики Runtime Secret
type: operations
status: approved
owner: developer
version: 1.4.0
updated: 2026-09-07
---

# Граница поставки

Issue #1068 завершает Secret Broker часть D6 #1046 для MVP-UI-46/47/49/50.
Control-plane владеет metadata, tenant/owner, draft generation, OCC,
идемпотентностью, lease/claim и конечным состоянием. Secret Broker принимает
plaintext только в ограниченном RPC save и публикует его только в разрешённую
immutable runtime materialization. Сохранённый черновик содержит ciphertext;
ключевой материал доставляется отдельно только broker.

## Карта сценариев и жизненного цикла

| Действие | Полномочия и переход владельца | Эффект broker | Итог и восстановление |
| --- | --- | --- | --- |
| Save | HTTP session/CSRF → CP PrepareSave с project ownership, expected version и idempotency → отдельный одноразовый SAVE grant | Bounded plaintext/type/digest validation, AES-256-GCM с random nonce и exact immutable AAD, encrypted Kubernetes Secret | Owner Complete фиксирует descriptor; active Secret не меняется. Lost reply возвращает прежний exact intent. |
| Validate | CP разрешает текущий draft и выдаёт отдельный VALIDATE grant | Exact UID/resourceVersion/ciphertext/key identity readback, decrypt и type/content validation | Owner фиксирует VALID только для той же generation. Plaintext не возвращается. |
| Publish | Fresh owner/tenant authority, validated generation, expected active version, отдельный PUBLISH claim | Exact encrypted read, decrypt, fenced immutable runtime materialization | CP атомарно фиксирует active revision/audit/receipt; effect-before-complete восстанавливается по exact descriptor. |
| Discard | CP закрывает draft, grants и claims в owner-транзакции | Bounded удаление только exact UID/resourceVersion | Повтор после lost response не публикует и не удаляет чужой объект. |
| Expiry | Серверное время и точный незавершённый draft, закрытие owner-графа | Точная очистка закрытого encrypted descriptor | Новая replica продолжает по authoritative owner read; локальный таймер не назначает terminal. |
| Claim expiry/retry | Предыдущий lease закрыт до новой claim generation | Readback прежнего эффекта перед новым действием | Неизвестный исход не подменяется новым idempotency key. |
| Key rotation | Отдельная repo-owned операция доставки, монотонная generation | Read-only keyring, durable rollback guard, overlap для сохранённых черновиков | Missing/corrupt/retired key закрыто отклоняет decrypt/readiness; прежние drafts не удаляются. |

Исправление #1084: recovery читает не больше десяти страниц по сто операций
за один цикл. Проверенный owner cursor сохраняется как локальная подсказка
следующего batch и обнуляется после конца списка. Это не watermark, grant
или подтверждение cleanup: каждая запись повторно проходит owner readback и
exact descriptor/fence перед эффектом. Restart начинает чтение с начала;
отменённые или временно ошибочные операции остаются в CP и возвращаются при
следующем обходе. Retained KEEP в начале списка не мешает достигнуть хвоста.
Тот же алгоритм закрывает системный аналог в RuntimeSecret recovery прежнего
CREATE/ROTATE/REVEAL lifecycle; scope/fence остаются у его отдельного owner RPC.
Ошибка/повтор cursor и повреждённая страница закрыто отклоняют весь batch;
ожидание единственного pagination reader отменяется его context.

FAILED без внешнего эффекта завершается внутри CP Recover одной транзакцией
только после сверки отсутствия actual/recorded descriptors. Broker не выдаёт
ACK произвольному KEEP и не удаляет retained material. Этот companion
принадлежит #1046/#1071. Ни один локальный cursor не объявляет очередь пустой
и не подменяет durable owner completion.

Публичные endpoints и подготовка операций принадлежат #1045/#1046; browser не
получает operation grant, encrypted descriptor или keyring. Authority RPC,
клиент, policy registration, readiness и итоговый render входят в полную
поставку; наличие типов само по себе не означает завершения.

Для каждой terminal операции отдельного доменного события не требуется:
авторитетный путь — CP draft/tombstone/operation receipt, а broker recovery
читает exact owner work. Published Runtime Secret использует существующий
owner revision/impact/rebind путь. Потребители не читают staging напрямую.
Предварительный immutable impact plan и selective rebind до публикации
реализуются владельцем #1046 и потребляются #1045/#1022; один broker RPC не
является доказательством завершения MVP-UI-47.

## Развёртывание и восстановление ключей

Оба профиля включают `deploy/k8s/base/runtime-secret-drafts` один раз.
Зашифрованные immutable объекты находятся в `kodex-secret-drafts`; там нет
workload и действует deny-all NetworkPolicy. Broker получает только
`get/create/delete` Secrets этого namespace и `get/update` одного ConfigMap
`secret-broker-draft-key-guard`. Keyring Secret `secret-broker-draft-keyring`
находится в `kodex-system` и проецируется read-only только в контейнер broker,
без `subPath`. Доступ к прочим platform Secrets через этот Role не появляется.

`generate-material.sh` создаёт новый installation keyring только для genesis;
`--secret-draft-keyring-file` переносит проверенную retained копию. Его material и
projection files остаются в owner-private installation directory.
`materialize-secrets.sh` передаёт этот единственный Secret отдельному
`bootstrap-secret-drafts.sh ensure`; общий declarative apply его не обновляет.
Genesis guard создаётся только при отсутствии и keyring, и guard. Существующий
guard никогда не сбрасывается. Удалённый guard при сохранённом keyring требует
восстановления точного защищённого backup, а не повторной инициализации.

## Сохранение и exact recovery после refresh (#1156)

`draft-key-recovery.py` сохраняет immutable Secret отдельного purpose
`secret-draft-key-backup` в retained `kodex-secret-drafts`. Имя связывает revision
и digest, annotations — полный canonical digest и UID retained namespace.
Перед serving create/rotation выполняется backup и независимый readback; старые
backup не перезаписываются и не удаляются. Это не клиентский draft payload:
label encrypted draft отсутствует, exact Store Read/Lookup/Delete его отвергают.
RBAC не расширяется: broker уже получает тот же материал через serving volume;
в retained namespace только его RoleBinding, без list, с get/create/delete.
Обычным PWA/CP клиентам Kubernetes backup не выдаётся.

| Переход | Предусловие и эффект | Crash/replay |
| --- | --- | --- |
| Первый ensure | Genesis guard, новый keyring revision1; backup → serving create | UNKNOWN backup останавливает запуск; повтор начинает GET, не повторяет POST вслепую |
| Rotation | Сохранение всех read keys, next revision, backup → serving resourceVersion CAS | Неуспешный CAS сохраняет прежний current и безопасный неиспользованный backup; broker ACK обязателен |
| Local refresh | `preserve` до namespace DELETE/rm material; serving UID/RV и retained manifest/namespace повторно проверены | Missing/mismatched backup блокирует destructive шаг; rotation использует тот же backup-before-publish protocol |
| Generate после refresh | `export` exact backup принятого manifest, затем canonical private copy | Файлы под material не являются единственной копией; повтор readback без замены ключей |
| Missing serving | `restore` или ensure с exact candidate совпадающего retained manifest | Восстанавливается только projection; guard/uses не меняются, повтор подтверждает bytes |
| Lost keys | Нет копии с exact canonical manifest | Закрытый отказ; пустой список drafts/нулевые encryptions не разрешают reset |

Повторный guard read допускает рост encryptions, но не уменьшение; смена manifest,
guard UID или namespace UID останавливает операцию. Операторы применяют текущий
repo-owned protocol последовательно; конкурирующий старый installer не считается
поддерживаемым способом rotation. Serving keyring, не подтверждённый retained
manifest, блокирует refresh до завершения rotation. Retained backup не является
заменой внешней защищённой резервной копии всего контура.

Для уже отсутствующей projection и найденной внешней private копии:

```bash
./tools/install/bootstrap-secret-drafts.sh restore --context "$EXACT_CONTEXT" \
  --keyring-file "$EXACT_RETAINED_KEYRING_FILE"
```

Для подготовки нового material вручную через repo-owned installer:

```bash
python3 tools/install/draft-key-recovery.py export --context "$EXACT_CONTEXT" \
  --output-directory "$PRIVATE_RECOVERY_DIRECTORY"
```

Каталог должен уже существовать, иметь mode0700 и принадлежать оператору. Вывод —
только путь нового private0400 файла; `generate-material.sh` принимает этот путь
через `--secret-draft-keyring-file`. Обычный `dev.sh up` соединяет шаги сам.
`reset-local` не расширялся; он не заявляется полным безопасным reset D6.
Для инцидента 5e864d32 отсутствие старых ключей остаётся невосстановленным
состоянием, пока не найдена exact копия. Prevention не объявляет live recovery PASS.

Проверки: existing `test-secret-broker-drafts`,
`test-local-material-contract-revision`, install/render и
`make test-secret-draft-key-recovery-api` с `KODEX_DRAFT_RECOVERY_TEST_IMAGE`
(локальный cached immutable k3s image ID, без pull), бюджет360s. API fixture
не запускает workloads/provider; ACK manifest задаётся test-owned записью,
реальный parser/crypto проверяется broker unit. Context7 `/kubernetes/website`:
namespace deletion, immutable Secret и UID/resourceVersion preconditions.

Для согласованной ротации используются только repo-owned команды. Переменные
с путями указывают на private файлы, `EXACT_CONTEXT` — на разрешённый disposable
контур. Файлы keyring не печатаются, не передаются в GitHub и не становятся
артефактом проверки.

```bash
cd services/internal/secret-broker
go run ./cmd/secret-draft-keys rotate \
  --input-file "$CURRENT_KEYRING_FILE" --output-file "$NEXT_KEYRING_FILE" \
  --expected-revision "$EXPECTED_KEYRING_REVISION"
cd ../../..
./tools/install/bootstrap-secret-drafts.sh rotate --context "$EXACT_CONTEXT" \
  --keyring-file "$NEXT_KEYRING_FILE" \
  --expected-revision "$EXPECTED_KEYRING_REVISION"
```

Key CLI требует абсолютный путь, parent directory `0700`, owner UID,
единственную hard link и regular file `0400/0600`; новый файл создаётся через
`O_EXCL`, сохраняется `0400` и fsync. Ротация сохраняет все прежние read keys,
добавляет один новый current key и выполняет Kubernetes resourceVersion CAS.
Повтор с теми же bytes подтверждает прежний результат. Lost reply требует
exact readback; скрипт не делает hidden retry замены. Успех rotate включает
подтверждение, что broker уже прочитал новый revision/digest и записал guard.

Лимит — 128 retained keys и `1 << 24` зарезервированных шифрований текущим
ключом. Резервация перед AES-GCM устойчива к restart, конфликтам реплик и
неизвестному результату API. Readiness сверяет тот же лимит без расходования
счётчика; на границе требуется следующий ключ. Автоматическое удаление старых
ключей не выполняется: сохранённые черновики продолжают расшифровываться.
Rollback на старый manifest закрыто отклоняется, поэтому откат приложения
не должен откатывать keyring или guard.

## Composition, readiness и диагностика

Все пять draft RPC проходят authority verifier в том же сервере, что и
рабочие операции. Семь CP work RPC используют общий client operation
profile с точными методами `platform.runtime-secret-drafts.*`. UID/RV, lease и generation
проверяются до эффекта и при exact readback. Public ответ содержит только
безопасные metadata, включая owner Secret version; версия draft после publish
совпадает с версией возвращённого Secret.

Startup проверяет CP work readiness, keyring/guard и Kubernetes API, затем
выполняет ограниченный recovery cycle. Recovery worker запускается после
барьера и входит в общий cancel/join. Неудачный последний recovery делает
readiness отрицательной; успешный цикл восстанавливает её. Recovery не
расшифровывает и не повторяет publish, а выполняет отдельные owner решения
KEEP/DELETE для encrypted и runtime materialization с точными preconditions.

Метрики `kodex_secret_broker_draft_recovery_cycles_total`,
`kodex_secret_broker_draft_cleanup_readbacks_total` и
`kodex_secret_broker_draft_recovery_ready` используют закрытые labels.
Успешная очистка учитывается и при последующей ошибке другого эффекта/ACK.
Логи и RPC errors не содержат исходную Kubernetes/CP диагностику, plaintext,
key material, grant или descriptor.

## Проверенная библиотечная документация

Context7 resolve-library-id для Go завершился `fetch failed`; использованы
официальные [Go cipher](https://pkg.go.dev/crypto/cipher#NewGCMWithRandomNonce)
и [Kubernetes Secret](https://kubernetes.io/docs/concepts/configuration/secret/).
Random nonce добавляет 28 bytes overhead; число шифрований одним ключом
ограничивается устойчивым счётчиком ниже границы 2^32. Immutable Secret и
UID/resourceVersion preconditions защищают сохранённый encrypted descriptor.

## Проверки и допуск

Публичная точка входа `make test-secret-broker-drafts` выполняет Go race/vet/build,
bootstrap/CAS/recovery fixtures и render обоих полных профилей. Composition test
соединяет настоящий RPC caster, owner adapter, AES-GCM, FileKeys, durable guard,
encrypted storage и runtime storage через fake Kubernetes/generated CP stub:
save без активного Secret, validate, publish exact runtime bytes, отдельный
save/discard и отказ/восстановление readiness. Он не подменяет защищённый live
CP/Kubernetes path. Отдельно проверяются Proto/codegen и authority policy/render.

Exact SHA и результаты каждого запуска фиксируются в PR #1069 после commit.
Live CP→broker, Kubernetes, browser, staging и production — NOT RUN.
Общий baseline, gate и приёмка принадлежат #1031.

## Удаление provider authorization (#1089)

CP deletion intent → immutable cleanup task с account/attempt/generation и
точным descriptor → protected Observe METADATA_ONLY → новый exact cleanup
grant → broker durable fence → cancel/join локального worker → безопасный
descriptor произведённого credential → CP новая cleanup task → terminal receipt
и DELETED только после завершения всей очереди. Metadata read не запускает
provider polling и не возвращает user code, masked account или material.

Pending Secret превращается CAS в immutable tombstone с удалением его material.
Точный deterministic credential Secret name также остаётся занят credential
либо immutable tombstone. Поэтому поздний Create на другой реплике не может
восстановить очищенное material. Проверка отдельного fence до Create без
занятого имени не была бы достаточной: между двумя Kubernetes запросами есть
гонка. Локальный worker отменяется и присоединяется; удалённый producer
закрыто отклоняется устойчивым fence независимо от состояния процесса.

AUTHORIZATION_ABSENCE создаёт tombstone только CAS Create. Если pending object
успел появиться, возвращается conflict для нового owner snapshot/generation;
существующий объект без UID/resourceVersion не удаляется. При exact credential
delete UID/RV preconditions сохраняются. Поздний replacement получает отдельный
bounded descriptor и новую CP cleanup task, без слепого удаления replacement.
Receipt вместе с produced descriptor сохраняется устойчиво до ответа и
повторяется неизменным после потери ACK/restart. Tombstones не содержат секретов
и не удаляются фоновым GC, который снова разрешил бы materialization имени.

Код producer/source checkpoint 8dd6f0e потреблён prerequisite commit db11c1b73.
Broker обслуживает все три target kind через строгий discriminator/XOR;
неизвестный mode, смешанный target и неполные exact pins закрыто отклоняются.
API-key attempt без pending Secret также имеет metadata path по точному
детерминированному credential name. Старый Discard использует те же fences;
ошибка компенсации не завершает принадлежащую CP cleanup task.

| Состояние / сбой | Переход broker | Авторитетный результат |
| --- | --- | --- |
| Pending существует | Exact UID/RV CAS → immutable tombstone, cancel/join локального worker | Receipt после занятия credential name либо produced descriptor |
| Pending отсутствует | CAS Create tombstone | Concurrent pending creation → conflict и новый owner snapshot |
| Crash между fences | Metadata `ABSENT_UNFENCED` | Новая absence attempt завершает второй fence |
| Credential появился до/во время cleanup | Проверка account/UID/RV/content | Отдельный produced target для CP, без слепого удаления |
| Credential изменён после read | Delete UID/RV preconditions | Conflict, прежняя attempt не получает terminal receipt |
| Lost ACK / restart | Exact immutable receipt read | Прежние receipt и produced descriptor неизменны |
| Cancel / deadline во время local join | Pending fence остаётся, receipt не записан | Повтор продолжает readback; local worker bounded finish |
| Corrupt / чужой / mutable fence | Закрытый отказ | Никакого эффекта и terminal receipt |
| Успешная очистка | Material отсутствует, оба имени заняты | CP завершает task и проверяет весь deletion graph перед `DELETED` |

Проверена актуальная документация через Context7: `/kubernetes/website`,
разделы Immutable Secrets и API Preconditions (`uid`, `resourceVersion`).
Регрессии используют настоящий store/service/RPC mapping и fake Kubernetes с
UUID, CAS и управляемыми гонками. Они не заменяют live Kubernetes admission и
межрепличный provider acceptance. Совместные CP lifecycle, baseline и deploy
остаются NOT RUN до объединённого exact SHA; результаты unit фиксируются
после чистого checkpoint.
