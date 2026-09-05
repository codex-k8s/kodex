---
id: OPS-DOC-1046-RUNTIME-V7
title: Переход runtime input на v7
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-05
---

# Переход runtime input на v7

Issue #1046 / MVP-UI-18: default effort из exact account/model catalog
сохраняется owner в immutable provider policy. При materialization control-plane
повторно проверяет eligibility, freshness и exact catalog pin. Пользовательский
TOML override проверяется на совместимость; без override используется
сохранённый default выбранного account. Результат записывается в
`RuntimeRevisionSnapshot.effective_reasoning_effort`, затем в
`RunnerInput.effective_reasoning_effort` и revision digest.

Обязательный `reasoning_mode=SUPPORTED|UNSUPPORTED` также назначает CP из
capabilities и включает в digest. SUPPORTED требует непустой effort;
UNSUPPORTED требует пустой effort и отсутствие TOML override. В последнем
случае runner не пишет параметр ни в TOML, ни в turn/start. Отсутствующий или
неизвестный mode закрыто отклоняется; пустая строка сама по себе не доказывает
отсутствие reasoning у модели.

Существующая `contracts/runtime-controller/v6/agent-runner-input.schema.json`
не изменяется. Fresh workload использует только `kodex.agent-runner-input.v7`.
Runner отклоняет v6 и отсутствие/подмену effective effort до provider call;
локальный список моделей не является вторым authority. Согласованное значение
идёт в config.toml, app-server turn params и continuation fingerprint.

Обновление требует rebuild и promotion образа с exact digest v7 schema.
Runtime-controller, role-image admission, environment render и warm reuse
проверяют тот же revision/digest. Уже сохранённые RuntimeRevision/attempt не
переписываются и не преобразуются в v7. Старый image не допускается к fresh
v7 workload; fallback к v6 запрещён.

Локальные source/consumer проверки не означают deployment. Переход развёрнутого
профиля, создание и promotion production image выполняются только отдельным
owner gate. Rollback не включает переписывание старых snapshots или выдачу
нового input старому образу.
