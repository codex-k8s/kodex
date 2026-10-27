"""Классифицирует readback после intent; не пишет policy и не разрешает retry.

Вызывающая сторона проверяет приватное хранение и долговечность intent,
получает все объекты из авторитетного API и повторно проверяет их свежесть.
Два последовательных GET не являются атомарным snapshot.
"""

import importlib.util
from pathlib import Path
import re


_spec = importlib.util.spec_from_file_location("runtime_policy_plan", Path(__file__).with_name("runtime-policy-refactor-plan.py"))
plan = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(plan)
UUID = r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"
REPORT_KEYS = {"status", "baseRevision", "targetRevision", "beforeSHA256", "afterSHA256", "policy", "binding",
               "parameters", "candidateSpecSHA256", "ownerReadiness", "apply", "executorSHA", "context",
               "clusterUID", "namespaceUID", "patchSHA256", "schema"}


def validate_report(report):
    """Проверяет форму и exact pins read-only отчёта без вывода authority."""
    require = plan.model.require
    require(isinstance(report, dict) and set(report) == REPORT_KEYS and
            report.get("schema") == "kodex.runtime-policy-refactor-dry-run/v1" and
            report.get("status") == "PASS_DRY_RUN_ONLY" and report.get("ownerReadiness") == "NOT_CHECKED" and
            report.get("apply") == "NOT_RUN" and report.get("context") == "k3d-kodex" and
            report.get("baseRevision") == plan.BASE_REVISION and report.get("targetRevision") == plan.TARGET_REVISION and
            report.get("beforeSHA256") == plan.BEFORE_SHA256 and report.get("afterSHA256") == plan.AFTER_SHA256 and
            isinstance(report.get("executorSHA"), str) and re.fullmatch(r"[0-9a-f]{40}", report["executorSHA"]) and
            report["executorSHA"] != "0" * 40, "REPORT_INVALID")
    require(all(isinstance(report.get(key), str) and re.fullmatch(r"[0-9a-f]{64}", report[key])
                for key in ("candidateSpecSHA256", "patchSHA256")), "REPORT_INVALID")
    require(isinstance(report.get("policy"), dict) and set(report["policy"]) == {"uid", "resourceVersion", "metadataSHA256"} and
            isinstance(report["policy"].get("uid"), str) and re.fullmatch(UUID, report["policy"]["uid"]), "REPORT_INVALID")


def validate_intent(report, intent):
    """Проверяет только структурную связь intent с отчётом, не owner approval."""
    validate_report(report)
    plan.model.require(isinstance(intent, dict) and set(intent) == {"schema", "planSHA256", "policyUID", "attemptID"} and
            intent.get("schema") == "kodex.runtime-policy-refactor-intent/v1" and
            intent.get("planSHA256") == plan.model.fingerprint(report) and
            intent.get("policyUID") == report["policy"]["uid"] and
            isinstance(intent.get("attemptID"), str) and re.fullmatch(UUID, intent["attemptID"]), "INTENT_INVALID")


def classify(report, intent, cluster_uid, namespace_uid, policy, binding, config, parameters):
    """Возвращает только наблюдаемое состояние; причинность и retry не выводит."""
    require = plan.model.require
    validate_intent(report, intent)
    require(isinstance(cluster_uid, str) and re.fullmatch(UUID, cluster_uid) and
            isinstance(namespace_uid, str) and re.fullmatch(UUID, namespace_uid) and
            cluster_uid == report.get("clusterUID") and namespace_uid == report.get("namespaceUID"), "NAMESPACE_IDENTITY_DRIFT")
    live_id = plan.identity(policy, "ValidatingAdmissionPolicy")
    planned_policy = report.get("policy")
    require(isinstance(planned_policy, dict) and set(planned_policy) == {"uid", "resourceVersion", "metadataSHA256"} and
            isinstance(planned_policy.get("resourceVersion"), str) and re.fullmatch(r"[0-9]+", planned_policy["resourceVersion"]) and
            isinstance(planned_policy.get("metadataSHA256"), str) and re.fullmatch(r"[0-9a-f]{64}", planned_policy["metadataSHA256"]) and
            live_id["uid"] == planned_policy["uid"] == intent.get("policyUID") and
            plan.model.fingerprint(plan.guarded_metadata(policy)) == planned_policy["metadataSHA256"], "POLICY_IDENTITY_DRIFT")
    require(plan.binding_state(binding) == report.get("binding") and
            plan.parameters_state(binding, config, parameters) == report.get("parameters"), "AUTHORITY_STATE_DRIFT")
    digest = plan.model.fingerprint(plan.model.api_defaults(policy.get("spec")))
    if digest == plan.BEFORE_SHA256:
        plan.compiled(policy)
        state = "SOURCE_OBSERVED_NO_RETRY"
    elif digest == plan.AFTER_SHA256:
        try:
            plan.compiled(policy)
            state = "TARGET_COMPILED_OBSERVED"
        except plan.model.PolicyTransitionError as error:
            if str(error) == "POLICY_COMPILATION_STALE":
                state = "TARGET_PENDING_OBSERVED"
            elif str(error) == "POLICY_WARNINGS":
                state = "TARGET_WARNINGS_OBSERVED"
            else:
                raise
    else:
        raise plan.model.PolicyTransitionError("POLICY_SPEC_DRIFT")
    return {"state": state, "policyUID": live_id["uid"], "resourceVersion": live_id["resourceVersion"],
            "causality": "NOT_PROVEN", "retry": "NOT_AUTHORIZED", "snapshot": "NOT_ATOMIC"}
