---
id: OPS-OPENCODE-NATIVE-ADAPTER-2108
title: Квалификация нативного адаптера OpenCode
type: runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-11
---

# Нативный адаптер OpenCode

Работа: [#2108](https://github.com/codex-k8s/kodex/issues/2108),
связанная с [#1982](https://github.com/codex-k8s/kodex/issues/1982).

Пакет `services/jobs/agent-runner/internal/opencode` реализует и проверяет
приватный V1 HTTP/SSE adapter закреплённого OpenCode1.19.5. Этот PR поставляет
адаптер и воспроизводимый integration harness. Он **ещё не переключает** broker,
control-plane, installer или существующие сессии платформы на новый driver.
Миграция, образы и кластерный cutover остаются в #2108. Тест этого пакета
не является приёмкой установленной платформы.

## Версии и официальный интерфейс

- Upstream source: `810c0eba1a7348036f8bdf61e6632d0c36c6876c`.
- `opencode-linux-x64-baseline.tar.gz` sha256:
  `17c200fd4b2289954cc01552559f9b6d03ec157b2ddfb5eb54144f491656333d`.
- Executable sha256:
  `7df4e4136301f16589b0f30bf91a61380fca5f8ef22dd36570f5ebe1d4ec802c`.
- Bundled SDK: openai3.0.88, anthropic3.0.111, openai-compatible2.0.41.

Проверены Context7, [V1 server](https://opencode.ai/v1/docs/server/) и
[исходники закреплённого релиза](https://github.com/anomalyco/opencode/tree/810c0eba1a7348036f8bdf61e6632d0c36c6876c).
Схемы V2 и параметры latest не подставляются в старый binary. Health требует
exact version до первого запроса. Model loop, transcript, compaction и pruning
принадлежат upstream; своего summarizer или KV manager пакет не создаёт.

## Владение процессом и состоянием

Одна execution generation получает приватный HOME/XDG и loopback server со
случайной авторизацией. Native shell/file/network tools и native subagents
закрыты: доступны только явно переданные managed MCP tools. Config directory
read-only, image-owned hook не имеет npm dependencies; repository config не
выбирает engine, credentials, plugins или модель.

Обычный completed assistant final завершает ход, summary — нет. Поток обработан
с bounded frames и backpressure, без lifetime ограничения4096 событий. Потеря
ответа на POST не запускает повторный model/tool effect. Pending native tool
закрывает автоматическое восстановление до owner reconciliation.

После abort требуется положительный join всей process group. Только затем
официальный export направляется в новый private regular file, проверяется по
ID/version/shape/size/hash, а import выполняется в новый HOME. Копирования
живой SQLite/WAL нет. CLI1.19.5 при stdout pipe способен обрезать большой export
до196608 байт с exit0: успешный exit не заменяет проверку полного JSON.

Managed MCP hook подписывает exact native session/call/arguments/revision;
bridge проверяет envelope и удаляет его до owner tool dispatch. Это лишь
correlation proof, не grant. Права, эффект receipts и связывание native checkpoint
с workspace generation должен обеспечить platform consumer при включении driver.
Без этих owner consumers пакет нельзя считать безопасным platform resume.

## Политики кеша на transport boundary

Закрытый transport shim работает с actual native SDK requests, включая
compaction и внутренние повторы. NONE не добавляет hints и не доказывает
отключение внутреннего provider cache. VLLM_CACHE_SALT использует переданный
защищённый namespace; NO_REUSE создаёт nonce на каждый фактический запрос.
OpenAI prompt_cache_key — routing key, Anthropic ephemeral — provider accounting;
они не доказывают физическую tenant isolation или NO_STORE. Consumer обязан
до inference проверить точные capabilities/версию/маршрут и строгую политику.

LITELLM transport добавляет квалифицированный read/write response cache bypass
и запрещает fallback; DIRECT не получает proxy-only поля. Локальный harness
квалифицирует LiteLLM1.104.2. Это не гарантия для произвольной версии чужого proxy.
Правила title/summary/small-model не должны обходить тот же выбранный маршрут.
Ключи и namespace не должны попадать в checkpoint, model text или diagnostics.

## Воспроизведение

Получить официальный binary выбранного релиза, проверить указанные SHA256:

```sh
KODEX_TEST_OPENCODE_BINARY=/absolute/path/to/qualified/opencode \
  GOTOOLCHAIN=go1.26.10 GOMAXPROCS=2 make test-opencode-native
```

Требуется Linux amd64 для закреплённого baseline asset. Без явно выбранного
binary обычный `go test` помечает native cases как SKIP. Make target отвергает
отсутствие binary: SKIP не является PASS native квалификации.

Дополнительно `KODEX_TEST_LITELLM_REDIS=1` включает настоящий disposable
LiteLLM+Redis с synthetic upstream. Docker должен быть доступен. Образы:

- `docker.io/litellm/litellm@sha256:3d88bd757134a00e8c00237b85a021ab1f6d54abb9c1df750f411dbe7256a500`
- `docker.io/library/redis@sha256:82403a0812a51faf6779f8d0dbe25ea85279ec7d49d24a3c78a65e9d8a87fa5f`

Harness использует отдельную internal network, private Unix sockets, случайный
тестовый key и точную ownership cleanup; не обращается к корпоративным credentials,
не публикует host TCP port, не очищает общий Redis и не использует внешнюю модель.
Отсутствующий optional Docker case фиксируется как SKIP отдельно.

Матрица включает настоящий native compactor, turn длиннее128 сообщений,
144 managed effects/36 compactions, original outputs после pruning, official
export/import, fresh instructions, Responses/Messages SDK, streaming, ошибки,
abort после принятого эффекта, отказ при unknown join и privacy проверки.
Token pressure синтетический и ускоряет штатный threshold. Размер wire body
в байтах не является tokenizer count или измерением реального окна модели.

## Оставшаяся приёмка

Подключение к платформе требует owner/controller callbacks, свежих grants и
credentials, paired native/workspace checkpoint, single-writer fencing,
installer/RoleImage delivery и UI/API/readiness. Отдельно проверяются AGENT,
WORKFLOW/delegation, Human Gate, Stop/restart/Pod replacement, PVC/S3 и два проекта.
Эти сценарии и переносимая чистая установка остаются в #2108; не закрывать
#1830/#1977/#1953 по локальному synthetic endpoint.
