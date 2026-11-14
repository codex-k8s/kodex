#!/usr/bin/env python3
"""Обновляет закреплённую поисковую проекцию models.dev без изменения маршрутов и выбора моделей."""
import argparse
import gzip
import hashlib
import json
import io
from pathlib import Path
import re
import tempfile
import jsonschema

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / "services/internal/control-plane/internal/domain/service/modeldiscovery/data"
MAXIMUM_BYTES = 16 * 1024 * 1024
SCHEMA = "kodex.model-provider-discovery/v1"


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate catalog field")
        result[key] = value
    return result


def read_bounded(path):
    with Path(path).open("rb") as stream:
        raw = stream.read(MAXIMUM_BYTES + 1)
    if not raw or len(raw) > MAXIMUM_BYTES:
        raise ValueError("catalog size is invalid")
    return raw


def text(value, maximum=512):
    if not isinstance(value, str) or not value or len(value.encode()) > maximum or any(ord(c) < 32 or ord(c) == 127 for c in value):
        raise ValueError("catalog text is invalid")
    return value


def project(raw):
    providers = json.loads(raw, object_pairs_hook=unique)
    if not isinstance(providers, dict) or not 1 <= len(providers) <= 1024:
        raise ValueError("catalog providers are invalid")
    records = []
    for provider_id, provider in sorted(providers.items()):
        if provider.get("id") != provider_id or not isinstance(provider.get("models"), dict):
            raise ValueError("catalog provider identity is invalid")
        for model_id, model in sorted(provider["models"].items()):
            if model.get("id") != model_id:
                raise ValueError("catalog model identity is invalid")
            apis = []
            for protocol, api in sorted(model.get("api", {}).items()):
                apis.append({"protocol": text(protocol, 128), "base_url": text(api.get("base_url", "UNSPECIFIED"), 2048),
                             "sdk": text(api.get("sdk", {}).get("aisdk", "UNSPECIFIED"), 256)})
                if 'path' in api:
                    apis[-1]['path'] = text(api['path'], 512)
            capabilities = model.get("capabilities", {})
            limits = model.get("limit", {})
            reasoning = capabilities.get("reasoning", {})
            modalities = model.get("modalities", {})
            context, output = limits.get("context", 0), limits.get("output", 0)
            if any(isinstance(v, bool) or not isinstance(v, (int, float)) or v < 0 or v > 10**12 for v in [context, output]):
                raise ValueError("catalog limits are invalid")
            efforts = [text(v, 32) for v in reasoning.get("effort", [])]
            records.append({"provider_id": text(provider_id, 128), "provider_name": text(provider["name"]),
                            "model_id": text(model_id), "name": text(model["name"]), "type": text(model.get("type", "chat"), 64),
                            "context_tokens": int(context), "output_tokens": int(output),
                            "tools": capabilities.get("tools", {}).get("supported") is True,
                            "reasoning": reasoning.get("supported") is True, "reasoning_efforts": efforts,
                            "input_modalities": [text(v, 128) for v in modalities.get("input", [])],
                            "output_modalities": [text(v, 128) for v in modalities.get("output", [])],
                            "upstream_status": model.get("status", "active"), "apis": apis})
    if not records or len(records) > 50000:
        raise ValueError("catalog model count is invalid")
    schema = json.loads((ROOT / 'contracts/model-provider-discovery/v1/snapshot.schema.json').read_text())
    jsonschema.Draft202012Validator(schema).validate(records)
    return records


def refresh(raw, revision, schema_raw, output=DATA):
    if not re.fullmatch(r"[a-f0-9]{40}", revision):
        raise ValueError("catalog revision is invalid")
    records = project(raw)
    payload = json.dumps(records, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
    retired = {}
    retired_path = output / 'retired-models.json'
    if retired_path.exists():
        retired = {(v['provider_id'], v['model_id']): v for v in json.loads(read_bounded(retired_path), object_pairs_hook=unique)}
    previous_path, previous_manifest_path = output / 'snapshot.json.gz', output / 'manifest.json'
    if previous_path.exists() or previous_manifest_path.exists():
        with gzip.GzipFile(fileobj=io.BytesIO(read_bounded(previous_path))) as compressed:
            previous = compressed.read(MAXIMUM_BYTES + 1)
        previous_manifest = json.loads(read_bounded(previous_manifest_path), object_pairs_hook=unique)
        if len(previous) > MAXIMUM_BYTES or hashlib.sha256(previous).hexdigest() != previous_manifest['snapshot_sha256']:
            raise ValueError('previous catalog digest mismatch')
        for record in json.loads(previous, object_pairs_hook=unique):
            retired[(record['provider_id'], record['model_id'])] = record
    for record in records:
        retired.pop((record['provider_id'], record['model_id']), None)
    retired_payload = json.dumps([retired[key] for key in sorted(retired)], ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()
    manifest = {"schema": SCHEMA, "source": "models.dev", "license": "MIT", "source_revision": revision,
                "upstream_schema": "models.dev/experimental-v2.0", "schema_sha256": hashlib.sha256(schema_raw).hexdigest(),
                "upstream_sha256": hashlib.sha256(raw).hexdigest(), "snapshot_sha256": hashlib.sha256(payload).hexdigest(),
                "source_url": "https://models.dev/_experimental_v2.0_api.json", "model_count": len(json.loads(payload))}
    # Обновляется только импорт. Custom overrides и выбранные пользователями модели не читаются и не переписываются.
    output.mkdir(parents=True, exist_ok=True)
    for filename, content in [("snapshot.json.gz", gzip.compress(payload, mtime=0)),
                              ("retired-models.json", retired_payload),
                              ("manifest.json", json.dumps(manifest, ensure_ascii=False, sort_keys=True, indent=2).encode() + b"\n")]:
        with tempfile.NamedTemporaryFile(dir=output, delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(content)
        temporary.replace(output / filename)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--schema-input", required=True)
    parser.add_argument("--expected-input-sha256", required=True)
    parser.add_argument("--expected-schema-sha256", required=True)
    args = parser.parse_args()
    raw, schema_raw = read_bounded(args.input), read_bounded(args.schema_input)
    if hashlib.sha256(raw).hexdigest() != args.expected_input_sha256 or hashlib.sha256(schema_raw).hexdigest() != args.expected_schema_sha256:
        raise ValueError("catalog input digest mismatch")
    refresh(raw, args.revision, schema_raw)
    print("Model discovery snapshot refreshed")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        raise SystemExit("Model discovery refresh rejected")
