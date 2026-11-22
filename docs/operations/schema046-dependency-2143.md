---
id: OPS-DOC-SCHEMA046-2143
title: Закрытая schema046 dependency для следующих миграций
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-14
---

# Schema046 как зависимость

Этот частный snapshot содержит только exact045→046 DDL, normal,
canonical-backup и отдельно table-only catalog proofs, forward backup cleanup и
Go CLI/native fixtures. Он предназначен для подготовки следующего schema047
поверх квалифицированного predecessor. Полный Telegram unit и его установка
этим snapshot не объявляются готовыми.

Источник046: полный Telegram source `b48bc45ac31990e8ed708e522b8e3fdaa9e7f62a`.
DDL, manifest, SQL и Go CLI идентичны этому source. Node owner profile046 здесь
не регистрируется: добавлены только те же закрытые schema validators для native
проверок. Runtime owner API, Proto, policy107, UI и deploy/activation входят в
полный Telegram unit #2143 и поставляются вместе с ним.

Manifest: `tools/release/telegram-notifications-schema-manifest.json`.
Его `snapshots.normal-after`, `snapshots.backup-after` и
`snapshots.table-only-backup-after` — отдельные exact predecessors. Table-only
не является canonical backup admission. Генератор проверяет pinned initializer,
полный045 predecessor manifest и все schema/restore fingerprints.

Публичные native проверки с Go1.26.10:

- `make test-telegram-notifications-upgrade-postgres`;
- `make test-telegram-notifications-backup-upgrade-postgres`;
- `python3 -B tools/release/generate-telegram-notifications-proof.py --check`.

Использовать private `GOCACHE` и `GOTMPDIR`, `GOMAXPROCS=2`,
`KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1`. Disposable PostgreSQL допускает
`KODEX_TEST_POSTGRES_STORAGE=ram`; production targets и credentials не нужны.
Capture включается отдельно через
`KODEX_TELEGRAM_NOTIFICATIONS_SCHEMA_CAPTURE_DIRECTORY` (owned directory0700)
и `KODEX_TELEGRAM_NOTIFICATIONS_SCHEMA_CAPTURE_MODE=normal|backup|table-only-backup`;
entrypoint: `scripts/tests/control-plane-postgres-test.sh --telegram-notifications-capture`.
Private full metadata из capture не публикуются.
