#!/usr/bin/env python3
"""Проверяет неизменённый официальный parser и равенство syntax с qualified runtime.

Публичная read-only проверка использует уже загруженный Go module cache. Она не
обращается к provider, не загружает зависимости и не изменяет workspace.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
MANIFEST = ROOT / "tools/release/observability-parser-profile.json"


def main():
    """Сверяет оба pinned source tree, закрытый набор файлов и текущий go.mod."""
    profile = json.loads(MANIFEST.read_text())
    cache = Path(subprocess.check_output(["go", "env", "GOMODCACHE"], text=True).strip())
    versions = [profile["libraryVersion"], profile["qualifiedRuntimeVersion"]]
    expected = {item["path"]: item["sha256"] for item in profile["sourceFiles"]}
    if len(expected) != 15 or profile["schema"] != "kodex-observability-parser-profile/v1":
        raise SystemExit("parser source profile is invalid")
    for version in versions:
        module = cache / (profile["libraryModule"] + "@" + version)
        directory = module / "pkg/logql/syntax"
        actual = {
            "pkg/logql/syntax/" + item.name
            for item in directory.iterdir()
            if item.is_file() and item.suffix in (".go", ".y") and not item.name.endswith("_test.go")
        }
        if actual != set(expected):
            raise SystemExit("official parser source file set changed")
        for path, digest in expected.items():
            if hashlib.sha256((module / path).read_bytes()).hexdigest() != digest:
                raise SystemExit("official parser source digest changed")
    imported = {item["path"]: item["sha256"] for item in profile["importedSourceFiles"]}
    if len(imported) != 126 or len(profile["importedPackages"]) != 18:
        raise SystemExit("official parser import profile is invalid")
    for version in versions:
        module = cache / (profile["libraryModule"] + "@" + version)
        for path, digest in imported.items():
            if hashlib.sha256((module / path).read_bytes()).hexdigest() != digest:
                raise SystemExit("official imported Loki source digest changed")
    environment = dict(os.environ, GOWORK="off", GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local", GOMAXPROCS="2", GOFLAGS="-p=1")
    target = subprocess.check_output(["go", "env", "GOOS", "GOARCH"], text=True, env=environment, timeout=10).splitlines()
    if target != [profile["goos"], profile["goarch"]]:
        raise SystemExit("parser source closure requires its qualified platform")
    output = subprocess.check_output(["go", "list", "-deps", "-json", "./internal/integration"],
                                     cwd=ROOT/"services/external/integration-gateway", text=True, env=environment, timeout=30)
    decoder = json.JSONDecoder()
    index = 0
    packages, actual_files = [], set()
    while index < len(output):
        while index < len(output) and output[index].isspace():
            index += 1
        if index == len(output):
            break
        item, index = decoder.raw_decode(output, index)
        name = item.get("ImportPath", "")
        prefix = profile["libraryModule"] + "/"
        if not name.startswith(prefix):
            continue
        if item.get("Module", {}).get("Version") != profile["libraryVersion"]:
            raise SystemExit("actual parser module version changed")
        packages.append(name)
        actual_files.update(name[len(prefix):]+"/"+filename for filename in item.get("GoFiles", []))
    if sorted(packages) != profile["importedPackages"] or actual_files != set(imported):
        raise SystemExit("actual imported Loki package/source closure changed")
    go_mod = (ROOT / "services/external/integration-gateway/go.mod").read_text()
    for module, version in [(profile["libraryModule"], profile["libraryVersion"]),
                            (profile["prometheusModule"], profile["prometheusVersion"])]:
        if (module + " " + version + "\n") not in go_mod:
            raise SystemExit("gateway parser module pin changed")
    print("PASS: official LogQL syntax and all126 imported Loki source files match; actual18-package closure and PromQL pin preserved")


if __name__ == "__main__":
    main()
