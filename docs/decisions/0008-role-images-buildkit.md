---
id: ADR-MC-008
title: Образы ролей и BuildKit
type: decision
status: approved
owner: architect
version: 0.4.0
updated: 2026-09-01
---

# ADR-MC-008. Образы ролей и BuildKit

## Решение

`RoleImageRecipe` имеет канонический хеш. Существующий подписанный дайджест образа переиспользуется; отсутствующий образ собирается BuildKit в изолированном контуре сборки. Типизированные списки пакетов предпочтительнее shell; сценарий установки — проверенный администратором аварийный механизм.

Сценарий установки необязателен; его каноническое отсутствие — пустая строка.
Полную version-pinned specification читает только проверенный owner через
специализированную операцию. Trusted materializer получает context/package/tool
из одного exact OCI repository по manifest и payload digests через отдельную
pull-only mTLS identity. Недоверенный `RUN` не получает secret или registry
credential; protected runtime ABI после него восстанавливается из exact trusted
base и проверяется admission до promotion.

Kaniko исключается из промышленной конфигурации как архивированный и неподдерживаемый исходный проект.

Образ допускается в среду выполнения после SBOM, проверки уязвимостей,
фиксации происхождения и проверки подписи. Сборщик публикует только в staging.
Отдельный admission owner связывает все доказательства с exact source/build/image
digest и выдаёт короткоживущий подписанный claim; только его promotion writer
переносит exact digest в read-only для node pull контур. Среда выполнения
использует дайджест.

Проверка уязвимостей всегда сохраняет полный отчёт закреплённой offline-базы.
Политика `fix-available-high-or-critical/v1` закрыто отклоняет образ при любой
уязвимости уровня `High` или `Critical`, для которой база указывает состояние
`fixed` и хотя бы одну исправленную версию. Уязвимости без доступной версии не
скрываются и не удаляются из отчёта: их количество входит в подписанное evidence,
но само отсутствие выпущенного исправления не блокирует прототипный runtime.
Ошибка scanner, базы, разбора отчёта или вычисления policy всегда блокирует образ.
Переход к более строгому порогу выполняется новой policy revision и требует новой
сборки/admission, а не изменения прежнего terminal verdict.

BuildKit работает с process sandbox от namespace-root внутри обязательного
Kubernetes Pod user namespace (`hostUsers: false`). Контейнеру нужен
`privileged: true`, но эта привилегия ограничена remapped user namespace и не
является host-root доступом. Профиль с отсутствующим либо истинным
`hostUsers`, rootless `newuidmap`, `noProcessSandbox` или иным insecure fallback
запрещён. Readiness выполняет тот же Dockerfile `RUN`, что и рабочая сборка.

## Последствия

- Нужны OCI-реестр и кеш сборки промышленного профиля.
- Сборщик отделяется от среды выполнения агента и его учетных данных.
- Изменение рецепта автоматически меняет `RuntimeRevision`.
- Отказ, устаревшее или отсутствующее доказательство закрыто запрещает promotion;
  admission receipt сохраняется как OCI artifact exact promoted digest.
