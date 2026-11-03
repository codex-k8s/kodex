#!/usr/bin/env python3
"""Read-only план optional services: strict values → Helm schema → private render."""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools/install"))
from deployment_profile import exact_hosts, Rejected


def require(value):
    if not value:
        raise ValueError("portable services plan rejected")


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result)
        result[key] = value
    return result


def validate_values(value):
    exposure = value["exposure"]
    if value["registry"].get("nodeCIDRs"):
        exact_hosts(value["registry"]["nodeCIDRs"])
    if value["storage"]["trustedProxyCIDRs"]:
        exact_hosts(value["storage"]["trustedProxyCIDRs"])
    if exposure["trustedProxyCIDRs"]:
        exact_hosts(exposure["trustedProxyCIDRs"])
    for field in ("sharedHost", "s3Host", "storageHost", "registryHost"):
        host = exposure[field]
        if not host:
            continue
        require(isinstance(host, str) and host == host.lower() and "." in host and len(host) <= 253)
        require(all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label)
                    for label in host.split(".")))
        try:
            ipaddress.ip_address(host)
        except ValueError:
            pass
        else:
            raise ValueError("DNS host required")
    return value


def merge_defaults(defaults, supplied):
    if isinstance(defaults, dict) and isinstance(supplied, dict):
        return {key: merge_defaults(defaults.get(key), supplied[key]) if key in supplied else item
                for key, item in {**defaults, **supplied}.items()}
    return supplied


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--values", type=Path, required=True)
    parser.add_argument("--release", required=True)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    require(all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,51}[a-z0-9])?", value)
                for value in (args.release, args.namespace)))
    require(args.values.is_file() and not args.values.is_symlink())
    raw = args.values.read_bytes()
    require(len(raw) <= 65536)
    supplied = json.loads(raw, object_pairs_hook=unique_object)
    require(isinstance(supplied, dict))
    defaults = json.loads((Path(__file__).parent / "values.yaml").read_text())
    validate_values(merge_defaults(defaults, supplied))
    require(args.output.is_absolute() and not args.output.exists() and not args.output.is_symlink())
    parent = args.output.parent
    require(parent.is_dir() and not parent.is_symlink() and parent.stat().st_uid == os.getuid()
            and stat.S_IMODE(parent.stat().st_mode) == 0o700)
    with tempfile.TemporaryDirectory(prefix=".portable-plan-", dir=parent) as directory:
        snapshot = Path(directory) / "values.json"
        snapshot.write_bytes(raw)
        snapshot.chmod(0o600)
        rendered = subprocess.run(["helm", "template", args.release, str(Path(__file__).parent),
                                   "--namespace", args.namespace, "--values", str(snapshot)],
                                  capture_output=True, timeout=30, check=False)
    require(rendered.returncode == 0 and len(rendered.stdout) <= 2 * 1024 * 1024)
    fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(rendered.stdout)
    print(json.dumps({"phase": "PLAN_ONLY", "values_sha256": hashlib.sha256(raw).hexdigest(),
                      "render_sha256": hashlib.sha256(rendered.stdout).hexdigest()}))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError, Rejected, subprocess.SubprocessError):
        raise SystemExit("Portable services plan rejected") from None
