---
title: Runtime-контракты Kodex
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-05
---

# Runtime-контракты Kodex

Модуль содержит общие типизированные границы server-materialized runtime,
инвентаризации образов и безопасных доказательств допуска. Доменные permissions,
tenant eligibility, PostgreSQL lifecycle и issuer credentials ему не принадлежат.
Он не выполняет RPC, сетевые запросы, сканирование, подпись или изменение данных.

## Отчёт об уязвимостях и решение о риске

- `ProjectImageVulnerabilityReport` потребляет полный исходный Grype JSON и
  immutable build tuple, сохраняя все `matches` и `ignoredMatches`.
- `CanonicalImageVulnerabilityReport` / `DecodeImageVulnerabilityReport`
  задают единственный canonical JSON `kodex.dev/image-vulnerability-report/v1`.
- `CanonicalImageRiskAcceptance` / `DecodeImageRiskAcceptance` задают единственный
  canonical JSON `kodex.dev/image-risk-acceptance/v1`.
- `ImageRiskAcceptanceMatchesReport` сверяет exact owner/build/image/report/policy
  tuple. Проверку previous receipt, evidence manifest, свежего attempt/fence,
  подписи и verified transport выполняет потребитель.
- `ProjectImageVulnerabilityEnvelope` и `VerifyImageRiskAcceptanceReportEnvelope`
  обслуживают чистый stdin-валидатор admission worker без claim credentials.
  Вход проекции передаёт исходные scanner bytes как canonical base64, сохраняя
  whitespace и завершающий перевод строки при вычислении SHA-256.

Находки группируются по полному tuple, сохраняя число повторений. Все счётчики
проверяются, включая suppressed и неизвестный severity. Ссылки выводятся только
из распознанного CVE/GHSA/GO identifier; scanner URLs, paths и metadata не попадают
в проекцию. Текущее правило блокирует только active HIGH/CRITICAL с доступной
исправленной версией. Решение о риске не отменяет технических проверок.

Ограничения: raw scanner JSON 64 MiB, canonical projection 4 MiB и 10 000 групп,
risk binding 16 KiB, причина 1..2048 UTF-8 bytes без controls. Duplicate keys,
неизвестные поля закрытых схем, неканоническая форма и неполные счётчики
отклоняются. Внешняя metadata допускается только в scanner input, с глубиной
JSON не более 32; прежние prompt-контракты сохраняют глубину 8.

Функции чистые: process lifecycle, shutdown и журналирование остаются у
потребителя. Ошибки фиксированы и не содержат исходных данных. Ошибка или
превышение ограничения означает закрытый технический отказ, а не усечение
готового отчёта. Исторические форматы не декодируются запасным путём.

Потребители: control-plane owner repository, admission reporter/bridge/signer и
promotion verifier. Публикация нового формата требует одновременной материализации
контрактов, consumers и deploy policy. Обратной совместимости и ручного backfill
старых отчётов нет; для них требуется штатная новая сборка.

Проверка: `GOTOOLCHAIN=local GOENV=off GOWORK=off go test ./...` из этого модуля
с закреплённым в `go.mod` toolchain; только безопасные синтетические fixtures.
