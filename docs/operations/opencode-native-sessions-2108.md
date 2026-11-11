---
id: OPS-OPENCODE-NATIVE-2108
title: OpenCode native adapter и границы квалификации
type: runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-11
---

# OpenCode: нативные сессии и восстановление

Работа: [#2108](https://github.com/codex-k8s/kodex/issues/2108), дочерняя
[#1982](https://github.com/codex-k8s/kodex/issues/1982).
Точка передачи: native adapter квалифицирован локально. Полный переход платформы
остаётся в работе в #2108; локальные тесты не подтверждают установленный результат.

## Решение об upstream

Используется официальный приватный headless server OpenCode **1.19.5**,
V1 HTTP API. Один процесс обслуживает одну текущую execution generation.
Он слушает только `127.0.0.1` со случайной локальной авторизацией; общий
публичный multi-tenant OpenCode server не создаётся.

Проверенные исходники релиза:
`810c0eba1a7348036f8bdf61e6632d0c36c6876c`.
Архив `opencode-linux-x64-baseline.tar.gz`:
`sha256:17c200fd4b2289954cc01552559f9b6d03ec157b2ddfb5eb54144f491656333d`.
Извлечённый Linux amd64 executable:
`sha256:7df4e4136301f16589b0f30bf91a61380fca5f8ef22dd36570f5ebe1d4ec802c`.

Проверены Context7, [V1 server](https://opencode.ai/v1/docs/server/) и
[точные исходники](https://github.com/anomalyco/opencode/tree/810c0eba1a7348036f8bdf61e6632d0c36c6876c).
Схемы V2 (`/api/session`, `compaction.keep`) не применяются к этому бинарнику.
Наличие более новой страницы документации не является доказательством
поддержки параметра. Health требует exact version перед первым запросом.

OpenCode владеет transcript, model loop и автоматической compaction.
Kodex не реализует второй цикл шагов и не вызывает собственный summarizer.
Тонкий HTTP-клиент отправляет только новый ввод и текущую утверждённую
system-материализацию; summary не является итогом бизнес-хода или authority.

CLI `run --format json` заменяется API: его поток не предоставляет удобной
границы свежей system-материализации, точного native binding и terminal reply.
Штатные CLI `export <exact-id>` / `import <local-file>` остаются offline
средствами переноса native state; Kodex не читает и не воспроизводит схему DB.

## Граница сохранности

У каждой сессии отдельный приватный HOME/XDG. Config и credentials выдаются
заново процессу и не входят в checkpoint. Пользовательский repository config,
default plugins, native shell/file/network tools и native subagents не должны
расширять управляемый каталог инструментов Kodex.

После остановки provider process требуется положительный join всей process
group. Затем официальный export читает согласованную native DB. Экспортёр
также должен завершиться и освободить writer до публикации.
Копирование живых SQLite/WAL файлов не используется.

Большой stdout pipe **не подходит** для CLI export 1.19.5: локальная
квалификация обнаружила обрезанный JSON размером 196608 байт при exit 0.
Экспорт направляется прямо в новый приватный regular file, затем проверяется
полный JSON, exact native ID/version, message/part identity, размер и SHA256.
Один успешный exit не подтверждает checkpoint.

Официальный import получает уже открытый descriptor проверенного локального
файла в **новый** HOME. Инициализированная или неопределённая native DB
не объединяется и не очищается автоматически. Повреждённый, отсутствующий,
чужой либо несовместимый export не заменяется пустой сессией.
Незавершённый native tool требует reconciliation, а не автоматического replay.

Это механизм native state. Сквозной checkpoint также обязан связать его с
workspace generation и принятыми effect receipts. Без owner commit этой
связи локальный export не объявляется успешным восстановлением платформы.

## Карта владельцев и переходов

| Сценарий | Владелец / проверка | Результат и потребитель |
| --- | --- | --- |
| Новый ход | CP: current initiator, grants, account, immutable RuntimeRevision, lease/fence | Controller проверяет exact signed input; broker запускает приватный OpenCode |
| Создание native session | Только broker после owner claim, один POST `/session` | Полученный upstream ID связывается с exact Kodex session; prompt не выбирает ID |
| Следующий ход | CP выбирает прежний подтверждённый binding и совместимость; права/credentials разрешаются заново | Official import + exact session; только новый ввод и текущие инструкции |
| Native tool | Управляемый MCP → прежний owner/gateway | Текущие grants, Human Gate и effect reconciliation остаются обязательными |
| Compaction | Штатный OpenCode threshold и разрешённая модель | Наблюдение/usage; не новый business turn и не SUCCESS |
| Обычный final | Completed ordinary assistant message + согласованный SSE terminal | Broker публикует итог; summary/error/length stop не проходят |
| Stop/timeout | Owner прекращает admission effects; native abort/process join | Частичный результат сохраняется только с доказанной границей; неизвестный tool не повторяется |
| Потеря SSE/POST | Нет автоматического повторного POST | Неопределённый результат, остановка provider; дальнейшее действие после reconciliation |
| Lease expiry / stale writer | Прежний single-writer CAS и fence у CP/controller | Нельзя подтвердить checkpoint старой attempt или продолжить её authority |
| Export | Join native process → официальный export → join exporter | Private file/hash; ещё не owner checkpoint receipt |
| Restore | Exact owner scope/digest/version + согласованный workspace checkpoint | Новый приватный HOME, текущий runtime input; отсутствие state закрывает запуск |
| Cutover | Drain старых executions и явно выбранная оператором очистка старых сессий | Нет legacy decoder или неявного преобразования auth; подключения и внешние объекты не удаляются |

Bindings, checkpoint и terminal receipts принадлежат существующему control
plane. Их read/claim/complete paths должны использовать прежние owner
транзакции, idempotency, OCC, terminal closure и authoritative readback;
native API не становится новым источником прикладных полномочий.

Первый native writer получает `INITIALIZING` intent в той же транзакции,
что lease и immutable RuntimeRevision. Последующий writer захватывает только
подтверждённый `LIVE` checkpoint без прежнего незавершённого writer.
Потерянный initial create не превращается в разрешение создать другую пустую
сессию. Completion связывает exact lease/revision, следующее native generation,
сохранённый native ID и текущую workspace generation. Успешный native outcome
без checkpoint отклоняется и исполнителем, и owner.

Архиватор использует формат 3 для официального native export как непрозрачного
файла. Его SHA256, размер, ID, версия и owner metadata закреплены до передачи
worker. Он не декодирует историю и не восстанавливает JSONL старого runtime.
Формат 2 для workspace сохраняется. Миграция отказывается работать при наличии
старых сессий и сама пользовательские данные не удаляет.

Image-owned `tool.execute.before` hook подписывает native session/call identity,
точные arguments и текущую RuntimeRevision отдельной процессной capability.
Loopback bridge проверяет её до передачи managed tool, удаляет envelope и
связывает owner receipt с native call вместо нестабильного SDK request ID.
SDK получает обратно свой correlation ID. Это не grant: текущие полномочия и
input/version checks остаются у владельца. Незавершённые внешние эффекты всего
root graph блокируют дальнейший native resume до сверки authoritative receipts.

После cancellation/timeout точный stopped writer может прислать поздний
checkpoint через тот же owner command. Проверяются исходные fence/generation,
immutable revision и текущий native writer CAS; metadata и подтверждённый usage
сохраняются без изменения terminal verdict и без нового graph event.
Authoritative read path — `session_storage` и обычный readback run/graph.
Точный receipt replay идемпотентен, а поздний writer не перезаписывает более новый.
Само продолжение требует отдельного разрешённого пользовательского перехода.

Hook не имеет npm dependencies. Read-only config directory предотвращает
автоматический npm bootstrap во время turn. Его подпись может оставаться в
закрытом native transcript как неавторитетная transport metadata; секретный
ключ туда не попадает. Штатный messages-transform hook удаляет только эту
metadata перед обычным inference и compaction, не меняя сообщения или результаты
инструментов.

## Доказательства текущего среза

Тесты запускают **настоящий** закреплённый binary. Синтетические HTTP endpoints
заменяют только модель и внешний MCP effect, а не OpenCode или его compactor.

```sh
cd services/jobs/agent-runner
KODEX_TEST_OPENCODE_BINARY=/absolute/path/to/qualified/opencode \
  GOTOOLCHAIN=go1.26.10 go test -count=1 -race -v ./internal/opencode
```

Без явного binary native-тесты дают `SKIP`; это не PASS квалификации.
Обычные unit-тесты не требуют скачивания runtime или model credentials.

| Проверка | Локальный результат |
| --- | --- |
| Exact native ID после замены процесса, прежний private XDG | PASS |
| 12 managed tool calls, 3 штатные compaction, обычный final | PASS |
| 144 tool calls, 36 compaction, 256 сохранённых messages | PASS |
| Official export/import в новый HOME, последующий business turn | PASS |
| Обновлённый system input, без повторения прежних tools | PASS |
| Полная история ~804 KB, максимальный фактический model HTTP body ~18 KB | PASS, synthetic forced threshold; это не tokenizer count |
| Duplicate JSON, чужие IDs, незавершённый tool, подмена digest/links | Unit-контур |
| Установленный Kodex / Pod replacement / S3 / WORKFLOW | NOT RUN для нового candidate |
| Полный cutover / UI / owner lifecycle / установка | Ветка продолжения, не готова к merge: общий PostgreSQL-контур FAIL |

Синтетический token pressure ускоряет достижение штатного порога, а не
имитирует многочасовую нагрузку или реальные пределы облачной модели.
Нормализованные counters OpenCode учитывают uncached input отдельно от
cache read/write, visible output отдельно от reasoning. Accounting не
доказывает физический reuse KV и не подтверждает отсутствие retention.

## Ограничения точки передачи

Изолированный native adapter можно квалифицировать независимо от включения в
платформу. WIP перехода owner/controller/installer на native contract нельзя
считать установленной функцией по тестам этого пакета. Полный PostgreSQL-прогон
обнаружил незавершённые bootstrap fixtures, старые archive paths, зависимости
purge graph и сценарии terminal/retry. Их необходимо устранить до cutover.
Миграция041, новые образы и RoleImage в тестовый кластер не установлены.

Точка воспроизведения native harness: `make test-opencode-native`, с явным
`KODEX_TEST_OPENCODE_BINARY`. Опциональный `KODEX_TEST_LITELLM_REDIS=1` запускает
изолированные LiteLLM1.104.2/Redis7.2 по digest, без доступа к внешней модели.
Полный native пакет с race прошёл за86.479s, включая managed effects,
compaction, pruning и official export/import. Это локальный synthetic model
контур, не квалификация реального облачного account либо аппаратного inference.
