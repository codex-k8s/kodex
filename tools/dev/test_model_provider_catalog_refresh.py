"""Проверки ограниченного обновления каталога без перезаписи пользовательских данных."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("catalog_refresh", Path(__file__).with_name("model-provider-catalog-refresh.py"))
CATALOG = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CATALOG)


class CatalogRefreshTest(unittest.TestCase):
    def fixture(self):
        return {"fixture": {"id": "fixture", "name": "Synthetic provider", "models": {"same": {"id": "same", "name": "Synthetic model", "type": "chat", "limit": {"context": 8192, "output": 1024}, "api": {"chat-completions": {"base_url": "https://model.example.invalid/v1", "sdk": {"aisdk": "@ai-sdk/openai-compatible"}}}}}}}

    def test_refresh_preserves_custom_overrides_and_selections(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            custom = output / "custom-overrides.json"
            selections = output / "selections.json"
            custom.write_bytes(b'["synthetic custom bytes"]')
            selections.write_bytes(b'{"selected":"retired-model"}')
            first = CATALOG.refresh(json.dumps(self.fixture()).encode(), "a" * 40, b"synthetic schema", output)
            second = CATALOG.refresh(json.dumps(self.fixture()).encode(), "a" * 40, b"synthetic schema", output)
            self.assertEqual(first, second)
            self.assertEqual(custom.read_bytes(), b'["synthetic custom bytes"]')
            self.assertEqual(selections.read_bytes(), b'{"selected":"retired-model"}')
            self.assertEqual(first["model_count"], 1)

    def test_identity_and_duplicate_fields_fail_closed(self):
        fixture = self.fixture()
        fixture["fixture"]["models"]["same"]["id"] = "foreign"
        with self.assertRaises(ValueError):
            CATALOG.project(json.dumps(fixture).encode())
        with self.assertRaises(ValueError):
            CATALOG.project(b'{"fixture":{},"fixture":{}}')

    def test_removed_models_remain_as_tombstones_without_changing_custom_bytes(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            fixture = self.fixture()
            CATALOG.refresh(json.dumps(fixture).encode(), "a" * 40, b"synthetic schema", output)
            second = self.fixture()
            second['fixture']['models'] = {'new': {**second['fixture']['models']['same'], 'id': 'new'}}
            CATALOG.refresh(json.dumps(second).encode(), "b" * 40, b"synthetic schema", output)
            retired = json.loads((output / 'retired-models.json').read_bytes())
            self.assertEqual([(v['provider_id'], v['model_id']) for v in retired], [('fixture', 'same')])
            CATALOG.refresh(json.dumps(fixture).encode(), "c" * 40, b"synthetic schema", output)
            retired = json.loads((output / 'retired-models.json').read_bytes())
            self.assertEqual([(v['provider_id'], v['model_id']) for v in retired], [('fixture', 'new')])


if __name__ == "__main__":
    unittest.main()
