#!/usr/bin/env python3
"""Локальная Kubernetes fixture для проверок #1068; секреты не печатаются."""

import base64
import hashlib
import json
import os
from pathlib import Path
import sys

state_file = Path(os.environ["KODEX_DRAFT_FIXTURE_STATE"])
state = json.loads(state_file.read_text()) if state_file.exists() else {}
args = sys.argv[1:]
for flag in ("--context", "--kubeconfig", "-n"):
    while flag in args:
        index = args.index(flag)
        del args[index:index + 2]
args = [arg for arg in args if not arg.startswith("--request-timeout=")]


def save():
    state_file.write_text(json.dumps(state))


def acknowledge():
    if os.environ.get("KODEX_DRAFT_FIXTURE_NO_ACK") == "1":
        return
    document = json.loads(base64.b64decode(state["keyring"]["data"]["keyring.json"]))
    keys = [{"ID": key["id"], "Generation": key["generation"]} for key in sorted(document["keys"], key=lambda item: item["generation"])]
    manifest = {"revision": document["revision"], "current": next(key for key in keys if key["ID"] == document["current"]), "keys": keys, "digest": ""}
    manifest["digest"] = hashlib.sha256(json.dumps(manifest, separators=(",", ":")).encode()).hexdigest()
    prior = json.loads(state["guard"]["data"]["state.json"])
    uses = {key["id"]: key for key in prior["uses"]}
    prior["manifest"] = manifest
    prior["uses"] = [uses.get(key["ID"], {"id": key["ID"], "generation": key["Generation"], "encryptions": 7}) for key in keys]
    state["guard"]["data"]["state.json"] = json.dumps(prior, separators=(",", ":"))
    state["guard"]["metadata"]["resourceVersion"] = str(int(state["guard"]["metadata"]["resourceVersion"]) + 1)


if args[:2] == ["get", "namespace"]:
    print(json.dumps({"metadata": {"name": "kodex-secret-drafts"}}))
elif args[:2] in (["get", "configmap"], ["get", "secret"]):
    key = "guard" if args[1] == "configmap" else "keyring"
    if key not in state:
        sys.exit(0 if "--ignore-not-found" in args else 1)
    print(json.dumps(state[key]))
elif args and args[0] in ("create", "replace"):
    incoming = json.loads(Path(args[args.index("-f") + 1]).read_text())
    key = "guard" if incoming["kind"] == "ConfigMap" else "keyring"
    if args[0] == "create":
        if key in state:
            sys.exit(1)
        incoming["metadata"]["uid"] = "fixture-" + key
        incoming["metadata"]["resourceVersion"] = "1"
    else:
        if key != "keyring" or key not in state or incoming["metadata"]["resourceVersion"] != state[key]["metadata"]["resourceVersion"] or os.environ.get("KODEX_DRAFT_FIXTURE_CONFLICT") == "1":
            sys.exit(1)
        incoming["metadata"]["resourceVersion"] = str(int(state[key]["metadata"]["resourceVersion"]) + 1)
    state[key] = incoming
    if key == "keyring":
        acknowledge()
    save()
    if os.environ.get("KODEX_DRAFT_FIXTURE_LOST_WRITE") == "1":
        sys.exit(1)
else:
    sys.exit(1)
