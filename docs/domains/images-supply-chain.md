---
id: DOM-MC-010
title: Образы и цепочка поставки
type: domain
status: approved
owner: architect
version: 0.3.0
updated: 2026-08-05
---

# Образы и цепочка поставки

## Назначение

Владеет `RoleImageRecipe`, запросом сборки, неизменяемым дайджестом образа, кешем, SBOM, происхождением, проверкой уязвимостей и состоянием подписи.

## Рецепт

Рецепт содержит:

- закрепленную ссылку или дайджест базового образа;
- целевые платформы;
- типизированные пакеты ОС, языков и инструментов;
- возможности браузера и тестирования;
- необязательный проверенный администратором сценарий установки;
- сетевую политику и политику реестра на время сборки;
- метаданные для каталога инструментов в промпте.

Хеш вычисляется по канонической сериализации полной спецификации: base/source/
context/builder/frontend/platform/package/tool/toolchain/policy digests,
версиям и multiline installation block. Изменение любого байта installation
block меняет `spec_sha256`; этот hash входит в image labels и provenance binding,
поэтому новый рецепт не может переиспользовать старый manifest digest. Reuse
разрешён только для exact promoted artifact с актуальными admission receipt,
policy, signature и registry readback.

## Сборщик

Kaniko не используется в промышленной конфигурации, поскольку исходный проект архивирован. BuildKit выполняет сборку в отдельном namespace и под отдельной служебной учетной записью. Режим без root предпочтителен; привилегированный резервный режим допускается только изолированно и документируется.

Сборщик не получает промышленные учетные данные среды выполнения. Токен реестра пакетов, если нужен, выдается как краткоживущий секрет с ограниченной областью и не попадает в слои образа или логи.

Канонический локальный контур разделяет staging push, staging admin,
promotion writer и node pull по Pod, ServiceAccount, mTLS/Vault identity,
NetworkPolicy и хранилищу. Pull монтирует promoted storage только read-only и
не имеет пути к внутренним endpoints. Отдельный deployable
`services/jobs/role-image-builder` получает server-owned claim, принимает
read-only `context.tar` с exact digest/revision, обращается к BuildKit через client-only
mTLS и публикует только в staging. Отдельный admission owner связывает exact
source/build/image digest с BuildKit provenance, SBOM digest, версией и
результатом vulnerability policy, проверенной signature identity и
OCI admission receipt, чей content и manifest digests фиксируются owner-side.
Update, archive или delete рецепта в той же owner-транзакции закрывает
незавершённые build/artifact и отзывает их build, admission и promotion claims.
Только отдельный HMAC-signed fenced короткоживущий claim, который включает
оба receipt digest,
выданный promotion workload после verdict, разрешает перенести exact digest;
истечение заменяет claim с повышением generation/fence. Admission receipt
до verdict публикуется отдельным staging OCI artifact с exact subject, а
promotion воспроизводит тот же payload с exact promoted subject. Consumed claim
и совместный image/receipt manifest readback фиксируются owner-транзакцией, а
pull видит только promoted admitted content. Admin DELETE не выдаётся сборщику
или pull. Rootless BuildKit
сохраняет process sandbox, работает без Kubernetes token, прикладных owner
secrets и persistent worker state; ослаблять mTLS или registry scopes запрещено.
Builder сверяет заявленный builder digest с exact BuildKit image, а toolchain
digest — с отрендеренным builder image. Package/tool blobs внутри context имеют
digest-named пути, повторно хешируются до BuildKit, устанавливаются offline, а
source context подключается к installation step read-only и не входит в layers.

## Допуск к публикации

Образ доступен агентам после:

- успешной сборки;
- формирования SBOM;
- прохождения политики уязвимостей;
- фиксации происхождения;
- проверки подписи;
- публикации в разрешенный OCI-реестр.

## Критерии приемки

- Одинаковый рецепт переиспользует дайджест.
- Изменение сценария, инструмента или основы меняет хеш.
- Неуспешная проверка блокирует использование и дает понятное состояние.
- Среда выполнения запускает дайджест, а не изменяемый тег.
- Перечень инструментов в промпте соответствует фактическому манифесту образа.
