#!/usr/bin/env python3
"""Встраивает полный замороженный013 proof в самостоятельные014 SQL-файлы.

В runtime не читается внешний predecessor-файл: каждый план пинит один готовый
SQL snapshot. --check проверяет воспроизводимость без изменения файлов.
"""

import argparse
import hashlib
import re
from pathlib import Path


DIRECTORY = Path(__file__).resolve().parent
PREDECESSOR_SHA256 = "6755e2bc1a40ebf7d6de8240e714dfa11cb092472bb5e3ad287d2799dd2aee30"
REPLACEMENTS = {
    "74f7d5fd475cb0919b76579335b547a5": "7010547e11bc8d1e8f67ac56feaba030",
    "70f0fd55213d39ae7f29567fb6b2200b": "7f8dc9469eb755a7b7760e8d4d7fc2cd",
    "3045149fe411e8acd562c73e8f2600cf": "80b6457cc872824d87164494cd44dc1a",
    "1f71f6d19e09709f61f2c2273a9af0ca": "29166a148eef617208fc527afaf907d1",
}


def compose(predecessor: str, metadata: str, final: bool) -> str:
    """Объединяет CTE без повторного snapshot и без коллизий имён proof-полей."""
    if final:
        for old, new in REPLACEMENTS.items():
            assert old in predecessor, "missing predecessor function pin"
            predecessor = predecessor.replace(old, new)
    head, projection = predecessor.split("\nSELECT (base_proof.proof ||", 1)
    projection, suffix = projection.rsplit(")::text FROM base_proof;", 1)
    assert suffix.strip() == "ROLLBACK;", "unexpected predecessor transaction tail"
    own_ctes, own_projection = metadata.split("\nSELECT jsonb_build_object(", 1)
    own_projection, suffix = own_projection.rsplit(")::text;", 1)
    assert suffix.strip() == "ROLLBACK;", "unexpected metadata transaction tail"
    own_ctes = own_ctes.split("WITH RECURSIVE ", 1)[1]
    # Все CTE metadata именуются отдельно; ссылки на реальные relations сохраняются.
    ctes = re.findall(r"(?:^|\n\), )([a-z_]+)(?:\([^\n]*?\))? AS \(", own_ctes)
    assert ctes and ctes[0] == "expected", "metadata CTE shape changed"
    own = own_ctes + "\nSELECT jsonb_build_object(" + own_projection + ") AS proof"
    for name in sorted(ctes, key=len, reverse=True):
        own = re.sub(r"(?<![.\w])" + name + r"\b", "evidence_" + name, own)
    own_ctes, own_query = own.split("\nSELECT jsonb_build_object(", 1)
    own_query = "SELECT jsonb_build_object(" + own_query
    return (
        "-- Полный predecessor013 и metadata014 проверяются в одном READ ONLY snapshot.\n"
        f"-- predecessor-source-sha256: {PREDECESSOR_SHA256}\n"
        + head + ", predecessor_proof AS (\nSELECT (base_proof.proof ||"
        + projection + ") AS proof FROM base_proof\n), "
        + own_ctes + ", evidence_proof AS (\n" + own_query
        + "\n)\nSELECT (predecessor_proof.proof || evidence_proof.proof)::text\n"
        + "FROM predecessor_proof CROSS JOIN evidence_proof;\nROLLBACK;\n"
    )


def main() -> None:
    """Отказывает при изменённом predecessor, неизвестной форме SQL или drift результата."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    predecessor = (DIRECTORY / "workflow-upstream-rework-migration-postcondition.sql").read_text()
    assert hashlib.sha256(predecessor.encode()).hexdigest() == PREDECESSOR_SHA256, "predecessor source changed"
    for phase in ("pre", "post"):
        metadata = (DIRECTORY / f"workflow-review-qa-evidence-{phase}-metadata.sql").read_text()
        output = DIRECTORY / f"workflow-review-qa-evidence-migration-{phase}condition.sql"
        result = compose(predecessor, metadata, phase == "post")
        if args.check:
            assert output.read_text() == result, "generated proof drift"
        else:
            output.write_text(result)


if __name__ == "__main__":
    main()
