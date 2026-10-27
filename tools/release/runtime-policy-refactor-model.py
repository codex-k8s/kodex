"""Точная модель CEL без записи policy; сама по себе не является допуском.

Вызывающая сторона обязана проверить происхождение source и утверждённые
digest, binding, compilation status, lifecycle, журнал и полномочия до применения.
"""

import copy
import hashlib
import json
import re


SHARED = "runtime environment volumes must have exact shared initializer driver and executor mounts"
AUTHORITY = "managed role Pod authority mount boundary is invalid"
POLICY = "runtime-role-pod-exact-secret-projection"


class PolicyTransitionError(Exception):
    """Закрытый отказ при несовпадении проверенных границ перехода."""


def require(condition, code):
    if not condition:
        raise PolicyTransitionError(code)


def api_defaults(spec):
    """Добавляет только утверждённые Kubernetes defaults, не изменяя CEL."""
    result = copy.deepcopy(spec)
    require(isinstance(result, dict), "POLICY_SPEC_REQUIRED")
    for name in ("matchConstraints", "matchResources"):
        if name not in result:
            continue
        match = result[name]
        require(isinstance(match, dict), "MATCH_CONSTRAINTS_INVALID")
        match.setdefault("matchPolicy", "Equivalent")
        match.setdefault("namespaceSelector", {})
        match.setdefault("objectSelector", {})
        for key in ("resourceRules", "excludeResourceRules"):
            rules = match.get(key, [])
            require(isinstance(rules, list), "MATCH_RULES_INVALID")
            for rule in rules:
                require(isinstance(rule, dict), "MATCH_RULES_INVALID")
                rule.setdefault("scope", "*")
    return result


def unique_rule(spec, message):
    rules = spec.get("validations")
    require(isinstance(rules, list), "VALIDATIONS_REQUIRED")
    indices = [i for i, rule in enumerate(rules) if isinstance(rule, dict) and rule.get("message") == message]
    require(len(indices) == 1, "EXACT_VALIDATION_REQUIRED")
    return indices[0]


def replay_initializer(rendered, raw):
    """Воспроизводит две exact записи прежнего initializer CAS в доверенных входах."""
    rendered = api_defaults(rendered)
    raw = api_defaults(raw)
    require(rendered.get("failurePolicy") == raw.get("failurePolicy") == "Fail", "FAIL_CLOSED_REQUIRED")
    for message in (SHARED, AUTHORITY):
        index = unique_rule(rendered, message)
        require(unique_rule(raw, message) == index, "RULE_INDEX_CHANGED")
        rendered["validations"][index] = copy.deepcopy(raw["validations"][index])
    return rendered


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()).hexdigest()


def build_patch(before_render, before_raw, after_render, after_raw, live, approved_before, approved_after):
    """Связывает полный предшественник и единственную замену с UID и версией."""
    old = replay_initializer(before_render, before_raw)
    new = replay_initializer(after_render, after_raw)
    require(all(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value)
                for value in (approved_before, approved_after)) and
            fingerprint(old) == approved_before and fingerprint(new) == approved_after,
            "SOURCE_DIGEST_MISMATCH")
    index = unique_rule(old, AUTHORITY)
    require(unique_rule(new, AUTHORITY) == index, "RULE_INDEX_CHANGED")
    require(old["validations"][index] != new["validations"][index], "TARGET_UNCHANGED")
    require({k: v for k, v in old["validations"][index].items() if k != "expression"} ==
            {k: v for k, v in new["validations"][index].items() if k != "expression"}, "TARGET_METADATA_CHANGED")
    expected = copy.deepcopy(old)
    expected["validations"][index] = copy.deepcopy(new["validations"][index])
    require(expected == new, "UNEXPECTED_SOURCE_CHANGE")
    require(isinstance(live, dict) and live.get("apiVersion") == "admissionregistration.k8s.io/v1" and
            live.get("kind") == "ValidatingAdmissionPolicy", "POLICY_IDENTITY_INVALID")
    metadata = live.get("metadata")
    require(isinstance(metadata, dict) and metadata.get("name") == POLICY and
            isinstance(metadata.get("uid"), str) and re.fullmatch(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", metadata["uid"]) and
            isinstance(metadata.get("resourceVersion"), str) and re.fullmatch(r"[0-9]+", metadata["resourceVersion"]),
            "POLICY_IDENTITY_INVALID")
    require(api_defaults(live.get("spec")) == old, "LIVE_SPEC_DRIFT")
    original = copy.deepcopy(live["spec"]["validations"][index])
    replacement = copy.deepcopy(new["validations"][index])
    path = f"/spec/validations/{index}"
    return [
        {"op": "test", "path": "/metadata/uid", "value": metadata["uid"]},
        {"op": "test", "path": "/metadata/resourceVersion", "value": metadata["resourceVersion"]},
        {"op": "test", "path": path, "value": original},
        {"op": "replace", "path": path, "value": replacement},
    ]
