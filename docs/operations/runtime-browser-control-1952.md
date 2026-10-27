---
id: OPS-RUNTIME-BROWSER-CONTROL-1952
title: Изолированный контроль падения Chromium в runtime
type: operations
status: approved
owner: developer
version: 1.1.0
updated: 2026-10-27
---

# Назначение

Диагностический manifest `tools/dev/runtime-browser-control-1952.yaml` относится
к [#1952](https://github.com/codex-k8s/kodex/issues/1952). Он проверяет тот же
закреплённый role image и UID 10002 в отдельном Pod без модели,
credentials, PVC, Secret и host mounts. Запуск не является приёмкой сотрудника
или исправлением дефекта. Успех только локализует различие между Pod и
песочницей Codex; отказ требует анализа фактического stderr и termination.

Начальный контроль вне Codex sandbox на `09e58115a` открыл страницу за 3715 мс
с `chromiumSandbox: true`; Pod и NetworkPolicy удалены по точным UID.
Текущая ревизия запускает две короткие команды `codex sandbox` без модели и
авторизации: с `/proc = deny` и `/proc = read`. Это дифференциальная диагностика
только пустого Pod. Профиль сотрудников и права действующей платформы не меняются;
результат `read` не разрешает предоставлять такое право настоящему сотруднику.
Временные CODEX_HOME содержат только создаваемую диагностическую конфигурацию.
Только одно изменение правила `/proc` различает два профиля; каждый получает
собственные временные HOME и каталоги Codex, но одинаковую команду браузера.

QA самой платформы уже воспроизвёл SIGTRAP двумя запусками Playwright с
`chromiumSandbox: true`. Штатный Dockerfile smoke использует default этого
параметра, который равен false согласно Context7 `/microsoft/playwright`,
`docs/src/api/params.md`; эти проверки не эквивалентны.

# Порядок оператора

1. Проверить текущие cluster, namespace и node UID штатным
   `staging-capacity-preflight.py`, бюджет 256 MiB и резерв 5 GiB. Сохранить
   приватный receipt. Проверить отсутствие обоих ресурсов manifest; неизвестное
   существующее состояние запрещает create или повтор.
2. Проверить SHA, чистоту worktree, digest образа в manifest и его наличие
   на указанном staging node. `imagePullPolicy: Never` запрещает загрузку образа.
   Сверить отсутствие активного изменения этих ресурсов другим оператором.
3. Выполнить server dry-run с явным `--context k3d-kodex`. Создать сначала
   NetworkPolicy и проверить exact spec/UID, затем Pod. В Pod нет секретов и
   service-account token. Отдельная NetworkPolicy запрещает ingress/egress;
   её добавление не меняет правила других Pod. Проверить, что иные namespace
   policies не выбирают этот диагностический label и не расширяют egress.
4. Сохранить assigned UID и фактический spec обоих ресурсов. Ожидать terminal
   не более 60 секунд, читать только журнал `browser-control` и termination.
   Browser открывает локальный DOM; внешние URL и `--no-sandbox` запрещены.
   Две команды имеют отдельные 18-секундные ограничения; ошибка запуска
   оболочки отличается от наблюдённого отказа Chromium. Общий exit 0 говорит
   лишь о завершённом сборе двух результатов, а не об их успешности.
   Timeout/OOM/image absence/API error остаются отдельными отказами контроля.
5. Удалить только exact созданные UID: сначала Pod и дождаться NotFound, затем
   NetworkPolicy. При неизвестном исходе не повторять create; перечитать ресурсы.
   Не менять seccomp, AppArmor, user namespace policy, kernel или firewall хоста.

# Границы и возврат

Manifest не является workload платформы и не получает runtime grant. Для
сопоставимости container seccomp повторяет существующий provider-runtime;
остальные ограничения Pod сохраняются: non-root, все capabilities удалены,
no-new-privileges и read-only root. Ограничены CPU, память, временный диск и
время. Контроль не читает действующий рабочий каталог сотрудника.

Возврат — подтверждённое удаление двух созданных объектов с UID preconditions.
Образы, registry, приложения и принятые артефакты не меняются. Даже при
`titleOK: true` требуется отдельная проверка реального сотрудника после
исправления; два предыдущих FAIL не заменяются результатом этого контроля.
