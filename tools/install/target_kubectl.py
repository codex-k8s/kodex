#!/usr/bin/env python3
"""Явный target для дочерних установочных команд без изменения kubeconfig хоста."""

import os
from pathlib import Path
import sys


def command(arguments, environment):
    binary = environment.get("KODEX_TARGET_KUBECTL", "")
    config = environment.get("KODEX_TARGET_KUBECONFIG", "")
    context = environment.get("KODEX_TARGET_CONTEXT", "")
    if not all((binary.startswith("/"), config.startswith("/"), context)):
        raise ValueError("Explicit Kubernetes client, configuration and context are required")
    if Path(binary).resolve() == Path(__file__).resolve():
        raise ValueError("Recursive Kubernetes client is forbidden")
    remaining = []
    index = 0
    while index < len(arguments):
        value = arguments[index]
        matched = False
        for flag, expected in (("--kubeconfig", config), ("--context", context)):
            if value == flag or value.startswith(flag + "="):
                if value == flag:
                    index += 1
                    actual = arguments[index] if index < len(arguments) else ""
                else:
                    actual = value[len(flag) + 1:]
                if actual != expected:
                    raise ValueError("Kubernetes target override is forbidden")
                matched = True
                break
        if not matched:
            if value.startswith("-s") and not value.startswith("--") or value in ("--server", "--cluster", "--user") or any(
                value.startswith(flag + "=") for flag in ("--server", "--cluster", "--user")
            ):
                raise ValueError("Kubernetes transport override is forbidden")
            remaining.append(value)
        index += 1
    if remaining[:1] == ["config"]:
        if remaining == ["config", "current-context"]:
            remaining = ["config", "view", "--minify", "-o", "jsonpath={.contexts[0].name}"]
        elif remaining[1:2] not in (["view"], ["get-contexts"], ["get-clusters"], ["get-users"]):
            raise ValueError("Host Kubernetes configuration mutation is forbidden")
    return [binary, "--kubeconfig", config, "--context", context, *remaining]


def main():
    try:
        arguments = command(sys.argv[1:], os.environ)
    except ValueError as error:
        print(str(error), file=sys.stderr)
        return 2
    os.execv(arguments[0], arguments)


if __name__ == "__main__":
    sys.exit(main())
