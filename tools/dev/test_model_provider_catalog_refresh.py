"""Проверки ограниченного обновления каталога без перезаписи пользовательских данных."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import gzip

SPEC = importlib.util.spec_from_file_location("catalog_refresh", Path(__file__).with_name("model-provider-catalog-refresh.py"))
CATALOG = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CATALOG)


class CatalogRefreshTest(unittest.TestCase):
    def refresh(self, raw, revision, output):
        return CATALOG.refresh(raw, revision, (CATALOG.DATA / 'upstream-schema-v2.ts').read_bytes(), output, retrieved_at='2026-11-14T04:03:01Z')

    def fixture(self):
        return {"fixture": {"id": "fixture", "name": "Synthetic provider", "models": {"same": {"id": "same", "name": "Synthetic model", "type": "chat", "limit": {"context": 8192, "output": 1024}, "api": {"chat-completions": {"base_url": "https://model.example.invalid/v1", "sdk": {"aisdk": "@ai-sdk/openai-compatible"}}}}}}}

    def test_refresh_preserves_custom_overrides_and_selections(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            custom = output / "custom-overrides.json"
            selections = output / "selections.json"
            custom_bytes = json.dumps(CATALOG.project(json.dumps(self.fixture()).encode())).encode()
            custom.write_bytes(custom_bytes)
            selections.write_bytes(b'{"selected":"retired-model"}')
            first = self.refresh(json.dumps(self.fixture()).encode(), "a" * 40, output)
            second = self.refresh(json.dumps(self.fixture()).encode(), "a" * 40, output)
            self.assertEqual(first, second)
            self.assertEqual(custom.read_bytes(), custom_bytes)
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
            self.refresh(json.dumps(fixture).encode(), "a" * 40, output)
            second = self.fixture()
            second['fixture']['models'] = {'new': {**second['fixture']['models']['same'], 'id': 'new'}}
            self.refresh(json.dumps(second).encode(), "b" * 40, output)
            retired = json.loads((output / 'retired-models.json').read_bytes())
            self.assertEqual([(v['provider_id'], v['model_id']) for v in retired], [('fixture', 'same')])
            self.refresh(json.dumps(fixture).encode(), "c" * 40, output)
            retired = json.loads((output / 'retired-models.json').read_bytes())
            self.assertEqual([(v['provider_id'], v['model_id']) for v in retired], [('fixture', 'new')])

    def test_absent_capabilities_and_limits_remain_unknown(self):
        fixture = self.fixture()
        fixture['fixture']['models']['same'].pop('limit')
        fixture['fixture']['models']['same'].pop('type')
        record = CATALOG.project(json.dumps(fixture).encode())[0]
        self.assertEqual((record['tools'], record['reasoning'], record['cache']), ('UNKNOWN', 'UNKNOWN', 'UNKNOWN'))
        self.assertFalse(record['context_known'])
        self.assertEqual(record['family'], 'UNKNOWN')
        self.assertEqual((record['type'], record['upstream_status']), ('UNKNOWN', 'UNKNOWN'))
        fixture['fixture']['models']['same']['capabilities'] = {'tools': {'supported': 0}}
        with self.assertRaises(ValueError):
            CATALOG.project(json.dumps(fixture).encode())

    def test_duplicate_overlay_fails_before_any_write(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            raw = json.dumps(self.fixture()).encode()
            self.refresh(raw, 'a' * 40, output)
            previous = (output / 'snapshot.json.gz').read_bytes()
            record = CATALOG.project(raw)[0]
            (output / 'custom-overrides.json').write_text(json.dumps([record, record]))
            with self.assertRaises(ValueError):
                self.refresh(raw, 'b' * 40, output)
            self.assertEqual(previous, (output / 'snapshot.json.gz').read_bytes())

    def test_projection_expansion_fails_before_replacing_any_asset(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            raw = json.dumps(self.fixture()).encode()
            self.refresh(raw, 'a' * 40, output)
            (output / 'custom-overrides.json').write_bytes(json.dumps(CATALOG.project(json.dumps(self.fixture()).encode()), indent=2).encode() + b'\n')
            (output / 'selections.json').write_bytes(b'{"selected":"same"}')
            before = {path.name: path.read_bytes() for path in output.iterdir()}
            fixture = self.fixture()
            template = fixture['fixture']['models']['same']
            fixture['fixture']['models'] = {str(index): {**template, 'id': str(index)} for index in range(16)}
            expanded_raw = json.dumps(fixture).encode()
            payload = json.dumps(CATALOG.project(expanded_raw), ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()
            self.assertGreater(len(payload), len(expanded_raw))
            with patch.object(CATALOG, 'MAXIMUM_BYTES', len(expanded_raw)):
                with self.assertRaisesRegex(ValueError, 'catalog size is invalid'):
                    self.refresh(expanded_raw, 'b' * 40, output)
            self.assertEqual(before, {path.name: path.read_bytes() for path in output.iterdir()})

    def test_combined_retired_bytes_fail_before_replacing_any_asset(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            fixture = self.fixture()
            self.refresh(json.dumps(fixture).encode(), 'a' * 40, output)
            fixture['fixture']['models'] = {'second': {**fixture['fixture']['models']['same'], 'id': 'second'}}
            self.refresh(json.dumps(fixture).encode(), 'b' * 40, output)
            (output / 'custom-overrides.json').write_bytes(json.dumps(CATALOG.project(json.dumps(self.fixture()).encode()), indent=2).encode() + b'\n')
            (output / 'selections.json').write_bytes(b'{"selected":"same"}')
            before = {path.name: path.read_bytes() for path in output.iterdir()}
            previous = gzip.decompress(before['snapshot.json.gz'])
            retired = before['retired-models.json']
            fixture['fixture']['models'] = {'third': {**fixture['fixture']['models']['second'], 'id': 'third'}}
            raw = json.dumps(fixture).encode()
            projected = json.dumps(CATALOG.project(raw), ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()
            limit = max(len(previous), len(retired), len(before['manifest.json']), len(before['custom-overrides.json']), len(raw), len(projected))
            merged = json.dumps(json.loads(retired) + json.loads(previous), ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()
            self.assertGreater(len(merged), limit)
            with patch.object(CATALOG, 'MAXIMUM_BYTES', limit):
                with self.assertRaisesRegex(ValueError, 'catalog size is invalid'):
                    self.refresh(raw, 'c' * 40, output)
            self.assertEqual(before, {path.name: path.read_bytes() for path in output.iterdir()})

    def test_unqualified_upstream_schema_and_invalid_timestamp_fail(self):
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaises(ValueError):
                CATALOG.refresh(json.dumps(self.fixture()).encode(), 'a' * 40, b'unqualified schema', Path(temporary), retrieved_at='2026-11-14T04:03:01Z')
            with self.assertRaises(ValueError):
                CATALOG.refresh(json.dumps(self.fixture()).encode(), 'a' * 40, (CATALOG.DATA / 'upstream-schema-v2.ts').read_bytes(), Path(temporary), retrieved_at='invalid')


if __name__ == "__main__":
    unittest.main()
