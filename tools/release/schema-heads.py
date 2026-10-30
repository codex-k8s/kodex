"""Публичные schema pins из закреплённого исходного дерева, без чтения БД."""
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
COMPONENTS = ('control-plane', 'internal-rpc-authority', 'email-bridge')


def heads():
    result = {}
    for name in COMPONENTS:
        path = ROOT / 'services/internal' / name / 'cmd/cli/migrations'
        values = [int(m[1]) for f in path.glob('*.sql') if (m := re.fullmatch(r'(\d+)_.+\.sql', f.name))]
        if not values or len(values) != len(set(values)):
            raise ValueError('Migration registry rejected')
        result[name] = max(values)
    return result


if __name__ == '__main__':
    print(json.dumps(heads(), sort_keys=True))
