#!/usr/bin/env python3
"""Закрытый oracle реальных selectors локального deploy без запуска shell."""

import argparse
import json
from pathlib import Path
import re
import subprocess
import sys


PHASES = ("supply-chain-builder-configuration", "core-runtime-configuration")
FRONTEND = "staff-control-center-runtime-764chfbgdm"
ENDPOINTS = "kodex-platform-endpoints"
NAMES = (
    "role-image-builder-runtime",
    "control-plane-runtime",
    "secret-broker-runtime",
    ENDPOINTS,
    FRONTEND,
)
EXPECTED = {
    "supply-chain": {"role-image-builder-runtime"},
    "": {"control-plane-runtime", "secret-broker-runtime", ENDPOINTS, FRONTEND},
    "control-plane": {"control-plane-runtime", ENDPOINTS},
    "secret-broker": {"secret-broker-runtime"},
    "control-api-gateway": {ENDPOINTS},
    "staff-control-center": {FRONTEND},
}


def require(condition, diagnostic):
    if not condition:
        raise ValueError(diagnostic)


def extract_selectors(source):
    # При изменении формы shell вызова тест закрыто требует обновить parser.
    # Никакие substitutions, shell expansion или source/eval не исполняются.
    phase_pattern = "(?:" + "|".join(map(re.escape, PHASES)) + ")"
    invocation = re.compile(
        r"^[ \t]*apply_render[ \t]+(?P<phase>"
        + phase_pattern
        + r")[ \t]+'(?P<expression>[^']*)'",
        re.MULTILINE,
    )
    calls = list(invocation.finditer(source))
    starts = re.findall(
        r"^[ \t]*apply_render[ \t]+" + phase_pattern + r"\b", source, re.MULTILINE
    )
    require(len(calls) == len(starts) == 6, "quoted runtime selector registry changed")
    builder = [call for call in calls if call["phase"] == PHASES[0]]
    require(len(builder) == 1, "builder selector cardinality changed")
    case_starts = list(re.finditer(r'case "\$selected_workload" in', source))
    require(len(case_starts) == 1, "selected workload case registry changed")
    case_start = case_starts[0].end()
    case_end = re.search(r"^[ \t]*esac\b", source[case_start:], re.MULTILINE)
    require(case_end is not None, "selected workload case is incomplete")
    case = source[case_start : case_start + case_end.start()]
    branches = re.compile(
        r'^[ \t]*(?P<label>""|control-plane|secret-broker|control-api-gateway|staff-control-center)\)'
        r"[ \t]*\n[ \t]*apply_render[ \t]+core-runtime-configuration[ \t]+'(?P<expression>[^']*)'"
        r"[ \t]*\n[ \t]*;;",
        re.MULTILINE,
    )
    selectors = {"supply-chain": builder[0]["expression"]}
    for branch in branches.finditer(case):
        label = "" if branch["label"] == '""' else branch["label"]
        require(label not in selectors, "runtime selector branch duplicated")
        selectors[label] = branch["expression"]
    require(set(selectors) == set(EXPECTED), "runtime selector branches changed")
    require(
        sorted(call["expression"] for call in calls)
        == sorted(selectors.values()),
        "runtime selector escaped the selected workload case",
    )
    return selectors


def resource(name, namespace="kodex-system", kind="ConfigMap"):
    return {
        "apiVersion": "v1",
        "kind": kind,
        "metadata": {
            "name": name,
            "namespace": namespace,
            "labels": {
                "app.kubernetes.io/part-of": "kodex",
                "kodex.dev/local-profile": "hot-reload",
                "kodex.dev/security-profile": "trusted-cluster",
            },
        },
        "data": {"syntheticFixture": "public-configuration-only"},
    }


def fixture_resources():
    accepted = [resource(name) for name in NAMES]
    rejected = []
    for name in NAMES:
        # Owner labels не разрешают запись в чужой namespace.
        for namespace in ("other-project", "kodex-system-other", "", None):
            rejected.append(resource(name, namespace))
        absent_namespace = resource(name)
        del absent_namespace["metadata"]["namespace"]
        rejected.append(absent_namespace)
        for kind in ("Secret", "Service", "Deployment", "Certificate"):
            rejected.append(resource(name, kind=kind))
    for name in (
        "kodex-internal-ca",
        "kodex-oidc-ca",
        "kodex-nats-ca",
        "kodex-otel-ca",
        "control-plane-skill-scanner",
        "integration-gateway-runtime",
        "automation-scheduler-runtime",
        "staff-control-center-runtime",
        "staff-control-center-runtime-",
        "staff-control-center-runtime-ABC123",
        "other-staff-control-center-runtime-abc123",
        "control-plane-runtime-shadow",
        "secret-broker-runtime-shadow",
        "role-image-builder-runtime-shadow",
        "kodex-platform-endpoints-shadow",
    ):
        rejected.append(resource(name))
    return accepted, rejected


def check_selectors(selectors):
    accepted, rejected = fixture_resources()
    fixtures = accepted + rejected
    wire = "\n".join(json.dumps(item, sort_keys=True) for item in fixtures) + "\n"
    for label, expression in selectors.items():
        result = subprocess.run(
            ["jq", "-c", expression],
            input=wire,
            text=True,
            capture_output=True,
            check=False,
            timeout=5,
        )
        require(result.returncode == 0, "runtime selector evaluation failed")
        actual = [json.loads(line) for line in result.stdout.splitlines()]
        expected = [
            item for item in accepted if item["metadata"]["name"] in EXPECTED[label]
        ]
        require(actual == expected, "runtime selector resource boundary mismatch")
        if label in ("", "control-plane", "control-api-gateway"):
            require(
                any(item["metadata"]["name"] == ENDPOINTS for item in actual),
                "shared endpoint prerequisite is missing",
            )
    return len(fixtures)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--source-root", type=Path, default=Path(__file__).resolve().parents[2]
    )
    arguments = parser.parse_args()
    source = (arguments.source_root / "tools/dev/deploy-local.sh").read_text(
        encoding="utf-8"
    )
    selectors = extract_selectors(source)
    count = check_selectors(selectors)
    print(
        f"Local runtime configuration selectors passed: {len(selectors)} selectors, {count} synthetic resources"
    )


if __name__ == "__main__":
    try:
        main()
    except (
        ValueError,
        OSError,
        subprocess.TimeoutExpired,
        json.JSONDecodeError,
    ):
        print("Local runtime configuration selector contract failed", file=sys.stderr)
        sys.exit(1)
