---
id: OPS-DOC-1390
title: Устойчивая ротация ключей internal RPC authority
status: approved
type: operation-evidence
owner: developer
version: 1.0.0
updated: 2026-09-09
---

# Граница и причина

Документ закрывает реализацию [#1390](https://github.com/codex-k8s/kodex/issues/1390).
Прежний publisher при новой revision одновременно переводил `NEXT` в `CURRENT`
и записывал новый `NEXT`; SQL переходил из `PREPARED` сразу в `PROMOTED`.
После частичной Kubernetes CAS-доставки не существовало устойчивого состояния,
которое позволяло отличить безопасный abort от обязательного resume.

Additive migration сохраняет старый writer как `protocol_version=1`, а новый
writer использует закрытый forward-only граф:

```text
PREPARED -> DELIVERING -> DELIVERED -> PROMOTED -> RETIRED
    |
    +-> ABORTED
```

`ABORTED` допустим только до первой доставки. После `DELIVERING` сбой, рестарт
или потерянный ответ ведут к readback и продолжению того же deterministic
intent. Нельзя создать параллельное намерение или пропустить source revision.
Payload JWS, private keys и credentials в таблицу намерений и операторский
readback не входят.

# Матрица lifecycle

| Переход | Инициатор и durable precondition | Действие и readback | Закрытый отказ |
| --- | --- | --- | --- |
| `PREPARED` | Единственный publisher, exact следующая source revision и digest registry; predecessor уже `PROMOTED/RETIRED`, его 40-секундное окно окончено | PostgreSQL фиксирует UUID, predecessor и точное число обязательных readers до Kubernetes CAS | Параллельный intent, пропуск revision, неизвестный predecessor, незавершённый overlap |
| `DELIVERING` | Тот же UUID/source/digest | Переход фиксируется до первой записи Secret; повтор после рестарта идемпотентен | После этого abort запрещён независимо от частичной доставки |
| `DELIVERED` | Snapshot history уже содержит exact publication; все записи и snapshot прочитаны обратно | Publisher фиксирует завершение CAS-доставки | Отсутствующий history, другой digest или частичная доставка |
| `PROMOTED` | `DELIVERED` и точный полный набор `(workload, role, generation)` readbacks | SQL атомарно фиксирует promotion и `overlap_until = DB time + 40 seconds` | Лишние динамические receipts не заменяют отсутствующий обязательный reader |
| `RETIRED` | Начинается следующая revision после окончания overlap | Predecessor переводится forward-only перед новым intent | Старый ключ нельзя вернуть в `CURRENT` обычным application rollback |
| `ABORTED` | Только `PREPARED`, history отсутствует, delivery ещё не начиналась | Publisher либо отдельная migrator job выполняет exact CAS и readback | `DELIVERING/DELIVERED/PROMOTED/RETIRED` нельзя отменить |

# Порядок поставки и активации

1. Запустить additive migration из точного source. Она не изменяет применённые
   migration bytes и не активирует новый writer сама.
2. Обновить publisher на совместимый бинарь через штатный authority
   infrastructure workflow. Обычный `scoped-release` приложения эту фазу не
   заменяет. В hot-reload staging пересборка immutable image не нужна, если
   контейнер, toolchain и встроенный binary не менялись; exact source и
   исполняемый процесс всё равно фиксируются.
3. До изменения target registry выполнить `rotation-status`. Новый publisher
   сам создаёт version2 intent. Нормальная ротация не требует ручного изменения
   статуса в PostgreSQL.
4. Наблюдать один и тот же intent до `PROMOTED`, затем выдержать оба
   40-секундных окна. Следующую revision нельзя публиковать раньше.
5. Только `PREPARED` можно закрыть `rotation-abort`. После неизвестного исхода
   сначала выполнить `resume`/`rotation-status`, а не создавать новый intent.

Инструмент `tools/release/authority-rotation-transition.mjs` принимает только
точный staging context и новый private plan. Он закрепляет namespace UID,
completed migration Job template UID/resourceVersion/spec digest, source path
и revision. `apply` создаёт отдельную ограниченную Job, fsync-запись `INTENT`
предшествует Kubernetes create. `observe` и `resume` проверяют exact Job и
возвращают только безопасную lifecycle metadata.

```bash
node tools/release/authority-rotation-transition.mjs plan \
  --context "$KODEX_RELEASE_CONTEXT" \
  --source /srv/kodex-dev/<EXACT_SOURCE> --revision <EXACT_SHA> \
  --action status --output /private/rotation-status-plan.json

node tools/release/authority-rotation-transition.mjs apply \
  --context "$KODEX_RELEASE_CONTEXT" \
  --plan /private/rotation-status-plan.json \
  --evidence /private/rotation-status.jsonl \
  --confirm APPLY-STAGING-AUTHORITY-ROTATION

node tools/release/authority-rotation-transition.mjs resume \
  --context "$KODEX_RELEASE_CONTEXT" \
  --plan /private/rotation-status-plan.json
```

Для abort plan дополнительно закрепляет `--intent-id`, `--source-revision` и
`--source-digest-sha256`, полученные из readback. Production context и другое
confirmation значение закрыто отклоняются.

# Crash, corruption и совместимость

- Crash до `DELIVERING` допускает exact abort либо повтор того же intent.
- Crash после `DELIVERING` требует resume. Уже записанный Secret сверяется по
  version/digest; несовпадение закрывает publisher readiness.
- Corruption JWK, snapshot, source binding или history не заменяется новой
  генерацией и не переводится в `PROMOTED`.
- Пропущенная registry revision закрыто отвергается до CAS.
- `CURRENT` новой revision всегда был `NEXT` предыдущей полностью
  подтверждённой revision. Поэтому mixed readers принимают его до switch;
  новый `NEXT` не становится signing key до следующей полной ротации.
- Issuer/verifier после рестарта загружают только полностью проверенный exact
  snapshot и собственную immutable projection. Last-known-good действует лишь
  до законного 30-секундного срока authorization context.
- Emergency revoke из #1322 не реализуется и не ослабляется этой state machine.
  Нормальные 40-секундные окна не разрешают fail-open, бессрочное доверие или
  автоматический rollback ключевого состояния.

# Доказательства реализации

PostgreSQL component harness проверяет migration, идемпотентный restart,
запрет promotion до полной доставки/readback, безопасный abort только до
delivery, запрет параллельной/пропущенной revision, bounded overlap и retire.
Go unit/build проверяют publisher и CLI; Node unit проверяет exact Job plan,
readback и закрытые статусы. Live activation, restart под нагрузкой и emergency
revoke остаются отдельными staging-сценариями #1223/#1322 и не считаются PASS
по локальным тестам.
