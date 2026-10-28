#!/usr/bin/env python3
"""Создаёт отдельный016 proof; замороженный015 proof остаётся прежним.

Каталог архивов и задач проверяется в том же read-only snapshot, что и полный
граф106/296 и обратная014 проекция. Исторические verifier не принимают016.
"""

import argparse
import hashlib
from pathlib import Path

DIRECTORY = Path(__file__).resolve().parent


def replace_exact(source: str, old: str, new: str) -> str:
    """Отклоняет изменение структуры замороженного015 до формирования SQL."""
    assert source.count(old) == 1, "checkpoint predecessor shape changed"
    return source.replace(old, new)


def main() -> None:
    """Генерирует отдельный read-only016 SQL либо проверяет его воспроизводимость."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    base = (DIRECTORY / "session-workspace-migration-postcondition.sql").read_bytes()
    expected = "1fd005d4bd318a82eb4d5130588692f5afb7f9263fc329b346ba3ef75b87f44f"
    assert hashlib.sha256(base).hexdigest() == expected, "frozen015 proof changed"
    source = base.decode()
    guard = (DIRECTORY / "workspace-checkpoint-catalog-guard.sql").read_text()
    source = replace_exact(source, "-- Read-only точный каталог workspace;", guard + "\n-- Read-only точный каталог workspace;")
    source = replace_exact(source, "'profile','SESSION_WORKSPACE_V2',", "'profile','WORKSPACE_CHECKPOINT_V2',")
    source = replace_exact(source, "'workspaceCatalog',true,", "'workspaceCatalog',true,\n 'checkpointCatalog',true,")
    source = replace_exact(source, "-- Generated: generate-session-workspace-proof.py; actual015 и inverse014 не смешиваются.",
                           "-- Generated: generate-workspace-checkpoint-proof.py; actual016 и inverse014 не смешиваются.")
    output = DIRECTORY / "workspace-checkpoint-migration-postcondition.sql"
    if args.check:
        assert output.read_text() == source, "checkpoint proof drift"
    else:
        output.write_text(source)


if __name__ == "__main__":
    main()
