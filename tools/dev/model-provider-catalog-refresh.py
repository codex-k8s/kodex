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
from datetime import datetime

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / "services/internal/control-plane/internal/domain/service/modeldiscovery/data"
MAXIMUM_BYTES = 16 * 1024 * 1024
SCHEMA = "kodex.model-provider-discovery/v1"
UPSTREAM_SCHEMA_SHA256 = '311d8d6fd567011417344ca66ea0e823564044673acdc16702070025e442deb5'


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate catalog field")
        result[key] = value
    return result


def bounded_bytes(raw):
    if not raw or len(raw) > MAXIMUM_BYTES:
        raise ValueError("catalog size is invalid")
    return raw


def read_bounded(path):
    with Path(path).open("rb") as stream:
        return bounded_bytes(stream.read(MAXIMUM_BYTES + 1))


def decode(raw):
    def invalid_constant(_value):
        raise ValueError('non-JSON numeric constant')
    value = json.loads(raw, object_pairs_hook=unique, parse_constant=invalid_constant)
    def depth(item, level=0):
        if level > 16:
            raise ValueError('catalog nesting exceeds limit')
        if isinstance(item, dict):
            for child in item.values():
                depth(child, level + 1)
        if isinstance(item, list):
            for child in item:
                depth(child, level + 1)
    depth(value)
    return value


def schema_file(name):
    return (ROOT / 'contracts/model-provider-discovery/v1' / name).read_bytes()


def validate_records(records):
    jsonschema.Draft202012Validator(decode(schema_file('snapshot.schema.json'))).validate(records)
    identities = set()
    for record in records:
        identity = (record['provider_id'], record['model_id'])
        if identity in identities:
            raise ValueError('duplicate catalog identity')
        identities.add(identity)
        if len({api['protocol'] for api in record['apis']}) != len(record['apis']):
            raise ValueError('duplicate catalog protocol')
        for name, maximum in {'provider_id': 128, 'provider_name': 512, 'model_id': 512, 'name': 512, 'family': 128, 'type': 64, 'upstream_status': 32}.items():
            text(record[name], maximum)
        if record['reasoning'] != 'SUPPORTED' and record['reasoning_efforts']:
            raise ValueError('unsupported reasoning has effort values')
        if not record['context_known'] and record['context_tokens'] or not record['output_known'] and record['output_tokens']:
            raise ValueError('unknown limit has a numeric value')
    return records


def support(capabilities, key):
    if key not in capabilities:
        return 'UNKNOWN'
    value = capabilities[key]
    if not isinstance(value, dict) or type(value.get('supported')) is not bool:
        raise ValueError('catalog capability is invalid')
    return 'SUPPORTED' if value['supported'] else 'UNSUPPORTED'


def cache_support(capabilities):
    if 'cache' not in capabilities:
        return 'UNKNOWN', 'UNKNOWN', 'UNKNOWN'
    cache = capabilities['cache']
    if cache is False:
        return 'NONE', 'UNSUPPORTED', 'UNSUPPORTED'
    if not isinstance(cache, dict) or not cache or set(cache) - {'implicit', 'explicit'}:
        raise ValueError('catalog cache capability is invalid')
    for value in cache.values():
        if value is not True and not isinstance(value, dict):
            raise ValueError('catalog cache capability is invalid')
    return ('BOTH' if len(cache) == 2 else next(iter(cache)).upper(),
            'SUPPORTED' if 'implicit' in cache else 'UNKNOWN', 'SUPPORTED' if 'explicit' in cache else 'UNKNOWN')


def text(value, maximum=512):
    if not isinstance(value, str) or not value or len(value.encode()) > maximum or any(ord(c) < 32 or ord(c) == 127 for c in value):
        raise ValueError("catalog text is invalid")
    return value


def project(raw):
    providers = decode(raw)
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
            if any(isinstance(v, bool) or not isinstance(v, (int, float)) or v < 0 or v > 10**12 or int(v) != v for v in [context, output]):
                raise ValueError("catalog limits are invalid")
            efforts = [text(v, 32) for v in reasoning.get("effort", [])]
            cache, implicit, explicit = cache_support(capabilities)
            records.append({"provider_id": text(provider_id, 128), "provider_name": text(provider["name"]),
                            "model_id": text(model_id), "name": text(model["name"]), "type": text(model.get("type", "UNKNOWN"), 64),
                            "context_tokens": int(context), "output_tokens": int(output),
                            "family": text(model.get('family', 'UNKNOWN'), 128),
                            "tools": support(capabilities, 'tools'),
                            "reasoning": support(capabilities, 'reasoning'), "reasoning_efforts": efforts,
                            "cache": cache, "cache_implicit": implicit, "cache_explicit": explicit,
                            "context_known": 'context' in limits, "output_known": 'output' in limits,
                            "input_modalities": [text(v, 128) for v in modalities.get("input", [])],
                            "output_modalities": [text(v, 128) for v in modalities.get("output", [])],
                            "upstream_status": model.get("status", "UNKNOWN"), "apis": apis})
    if not records or len(records) > 50000:
        raise ValueError("catalog model count is invalid")
    return validate_records(records)


def refresh(raw, revision, schema_raw, output=DATA, retrieved_at=''):
    if not re.fullmatch(r"[a-f0-9]{40}", revision):
        raise ValueError("catalog revision is invalid")
    if hashlib.sha256(schema_raw).hexdigest() != UPSTREAM_SCHEMA_SHA256:
        raise ValueError('upstream schema source is not qualified')
    if not re.fullmatch(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z', retrieved_at):
        raise ValueError('retrieval timestamp is invalid')
    datetime.fromisoformat(retrieved_at)
    records = project(raw)
    payload = bounded_bytes(json.dumps(records, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode())
    retired = {}
    retired_path = output / 'retired-models.json'
    if retired_path.exists():
        retired = {(v['provider_id'], v['model_id']): v for v in validate_records(decode(read_bounded(retired_path)))}
    custom_path = output / 'custom-overrides.json'
    if custom_path.exists():
        validate_records(decode(read_bounded(custom_path)))
    previous_path, previous_manifest_path = output / 'snapshot.json.gz', output / 'manifest.json'
    if previous_path.exists() or previous_manifest_path.exists():
        with gzip.GzipFile(fileobj=io.BytesIO(read_bounded(previous_path))) as compressed:
            previous = compressed.read(MAXIMUM_BYTES + 1)
        previous_manifest = decode(read_bounded(previous_manifest_path))
        jsonschema.Draft202012Validator(decode(schema_file('manifest.schema.json')), format_checker=jsonschema.FormatChecker()).validate(previous_manifest)
        if len(previous) > MAXIMUM_BYTES or hashlib.sha256(previous).hexdigest() != previous_manifest['snapshot_sha256']:
            raise ValueError('previous catalog digest mismatch')
        for record in validate_records(decode(previous)):
            retired[(record['provider_id'], record['model_id'])] = record
    for record in records:
        retired.pop((record['provider_id'], record['model_id']), None)
    retired_payload = bounded_bytes(json.dumps([retired[key] for key in sorted(retired)], ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode())
    if len(retired) + len(records) > 50000:
        raise ValueError('combined catalog exceeds limit')
    manifest = {"schema": SCHEMA, "source": "models.dev", "license": "MIT", "source_revision": revision,
                "upstream_schema": "models.dev/experimental-v2.0", "schema_sha256": hashlib.sha256(schema_raw).hexdigest(),
                "upstream_sha256": hashlib.sha256(raw).hexdigest(), "snapshot_sha256": hashlib.sha256(payload).hexdigest(),
                "source_url": "https://models.dev/_experimental_v2.0_api.json", "model_count": len(records),
                "retrieved_at": retrieved_at, "projection_schema": 'urn:kodex:contract:model-provider-discovery:v1',
                "projection_schema_sha256": hashlib.sha256(schema_file('snapshot.schema.json')).hexdigest(),
                "upstream_schema_validation": 'NOT_PERFORMED'}
    jsonschema.Draft202012Validator(decode(schema_file('manifest.schema.json')), format_checker=jsonschema.FormatChecker()).validate(manifest)
    # Каждый распакованный документ и каждый публикуемый файл укладываются в предел runtime loader до первой замены.
    artifacts = [("snapshot.json.gz", gzip.compress(payload, mtime=0)),
                 ("retired-models.json", retired_payload),
                 ("snapshot.schema.json", schema_file('snapshot.schema.json')),
                 ("manifest.schema.json", schema_file('manifest.schema.json')),
                 ("manifest.json", json.dumps(manifest, ensure_ascii=False, sort_keys=True, indent=2).encode() + b"\n")]
    for _, content in artifacts:
        bounded_bytes(content)
    # Обновляется только импорт. Custom overrides и выбранные пользователями модели не читаются и не переписываются.
    output.mkdir(parents=True, exist_ok=True)
    for filename, content in artifacts:
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
    parser.add_argument('--retrieved-at', required=True)
    args = parser.parse_args()
    raw, schema_raw = read_bounded(args.input), read_bounded(args.schema_input)
    if hashlib.sha256(raw).hexdigest() != args.expected_input_sha256 or hashlib.sha256(schema_raw).hexdigest() != args.expected_schema_sha256:
        raise ValueError("catalog input digest mismatch")
    refresh(raw, args.revision, schema_raw, retrieved_at=args.retrieved_at)
    print("Model discovery snapshot refreshed")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        raise SystemExit("Model discovery refresh rejected")
