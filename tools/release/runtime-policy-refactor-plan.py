"""Проверка доказательств read-only плана; применение здесь отсутствует.

Вызывающая сторона должна проверить Git revisions и получить candidate именно
из server-side dry-run; переданные словари сами по себе не доказывают источник.
"""

import copy
import importlib.util
from pathlib import Path
import re


_spec = importlib.util.spec_from_file_location("runtime_policy_model", Path(__file__).with_name("runtime-policy-refactor-model.py"))
model = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(model)

BASE_REVISION = "f2e64eb556276211e74e4ccd718fd4614ee1c04e"
TARGET_REVISION = "f245d6f16877e1e288d4e440cfafb87eae962d0d"
BEFORE_SHA256 = "9ef77c8dfe7d67f29fe6bb9b0bfab348fba562121f0ded01a43f84c437385c72"
AFTER_SHA256 = "40051a30210e7e71a26dd545c683848ee338014050e9698528d73582ff33e31e"
METADATA_GUARDS = ("labels", "annotations", "ownerReferences", "finalizers")


def guarded_metadata(resource):
    metadata = resource["metadata"]
    return {name: metadata.get(name) for name in METADATA_GUARDS}


def identity(resource, kind):
    model.require(isinstance(resource, dict), "IDENTITY_INVALID")
    meta = resource.get("metadata")
    model.require(resource.get("apiVersion") == "admissionregistration.k8s.io/v1" and resource.get("kind") == kind and
                  isinstance(meta, dict) and meta.get("name") == model.POLICY and
                  isinstance(meta.get("uid"), str) and re.fullmatch(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", meta["uid"]) and
                  isinstance(meta.get("resourceVersion"), str) and re.fullmatch(r"[0-9]+", meta["resourceVersion"]),
                  "IDENTITY_INVALID")
    return {"uid": meta["uid"], "resourceVersion": meta["resourceVersion"]}


def compiled(policy):
    generation = policy.get("metadata", {}).get("generation")
    status = policy.get("status")
    model.require(type(generation) is int and generation > 0 and isinstance(status, dict) and
                  type(status.get("observedGeneration")) is int and status["observedGeneration"] == generation,
                  "POLICY_COMPILATION_STALE")
    checking = status.get("typeChecking")
    model.require(checking is None or isinstance(checking, dict), "POLICY_STATUS_INVALID")
    warnings = (checking or {}).get("expressionWarnings")
    model.require(warnings is None or (isinstance(warnings, list) and not warnings), "POLICY_WARNINGS")


def binding_state(binding):
    result = identity(binding, "ValidatingAdmissionPolicyBinding")
    spec = binding.get("spec")
    model.require(isinstance(spec, dict) and spec.get("policyName") == model.POLICY and
                  spec.get("validationActions") == ["Deny"] and isinstance(spec.get("paramRef"), dict) and
                  spec["paramRef"].get("namespace") == "kodex-system" and
                  spec["paramRef"].get("parameterNotFoundAction") == "Deny" and
                  isinstance(spec["paramRef"].get("name"), str) and bool(spec["paramRef"]["name"]),
                  "BINDING_INVALID")
    result["specSHA256"] = model.fingerprint(spec)
    result["metadataSHA256"] = model.fingerprint(guarded_metadata(binding))
    return result


def parameters_state(binding, config, parameters):
    """Связывает immutable ConfigMap с точным параметром binding по UID и версии."""
    binding_state(binding)
    name = binding["spec"]["paramRef"]["name"]
    result = []
    for resource, kind, version in ((config, "ConfigMap", "v1"),
                                    (parameters, "ImageAdmissionPolicyParameters", "supplychain.kodex.dev/v1alpha1")):
        model.require(isinstance(resource, dict) and resource.get("kind") == kind and resource.get("apiVersion") == version and
                      isinstance(resource.get("metadata"), dict), "PARAMETER_IDENTITY_INVALID")
        meta = resource["metadata"]
        model.require(meta.get("name") == name and meta.get("namespace") == "kodex-system" and
                      isinstance(meta.get("uid"), str) and re.fullmatch(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", meta["uid"]) and
                      isinstance(meta.get("resourceVersion"), str) and re.fullmatch(r"[0-9]+", meta["resourceVersion"]),
                      "PARAMETER_IDENTITY_INVALID")
        result.append({"kind": kind, "uid": meta["uid"], "resourceVersion": meta["resourceVersion"],
                       "metadataSHA256": model.fingerprint(guarded_metadata(resource))})
    data = config.get("data")
    model.require(config.get("immutable") is True and isinstance(data, dict) and bool(data) and
                  data == parameters.get("spec"), "PARAMETER_CONTENT_MISMATCH")
    return {"resources": result, "contentSHA256": model.fingerprint(data)}


def make_patch(before_render, before_raw, after_render, after_raw, policy, binding, config, parameters):
    """Подтверждает source, compiled predecessor и binding до построения patch."""
    identity(policy, "ValidatingAdmissionPolicy")
    compiled(policy)
    parameters_state(binding, config, parameters)
    return model.build_patch(before_render, before_raw, after_render, after_raw, policy, BEFORE_SHA256, AFTER_SHA256)


def confirm_dry_run(before, binding_before, config_before, parameters_before,
                    candidate, after, binding_after, config_after, parameters_after, patch):
    """Проверяет полный candidate и отсутствие фактической записи после dry-run."""
    original_id = identity(before, "ValidatingAdmissionPolicy")
    model.require(identity(candidate, "ValidatingAdmissionPolicy")["uid"] == original_id["uid"] and
                  identity(after, "ValidatingAdmissionPolicy") == original_id, "POLICY_IDENTITY_DRIFT")
    compiled(before)
    compiled(after)
    model.require(binding_state(binding_before) == binding_state(binding_after), "BINDING_DRIFT")
    parameter_snapshot = parameters_state(binding_before, config_before, parameters_before)
    model.require(parameter_snapshot == parameters_state(binding_after, config_after, parameters_after), "PARAMETER_DRIFT")
    model.require(isinstance(patch, list) and len(patch) == 4 and all(isinstance(item, dict) for item in patch) and
                  set(patch[2]) == {"op", "path", "value"} and set(patch[3]) == {"op", "path", "value"} and patch[2]["op"] == "test" and
                  patch[3]["op"] == "replace" and patch[2]["path"] == patch[3]["path"], "PATCH_INVALID")
    model.require(patch[0] == {"op": "test", "path": "/metadata/uid", "value": original_id["uid"]} and
                  patch[1] == {"op": "test", "path": "/metadata/resourceVersion", "value": original_id["resourceVersion"]},
                  "PATCH_IDENTITY_INVALID")
    path = patch[3]["path"]
    model.require(re.fullmatch(r"/spec/validations/(0|[1-9][0-9]*)", path) is not None, "PATCH_INVALID")
    index = int(path.rsplit("/", 1)[1])
    expected = copy.deepcopy(before["spec"])
    model.require(model.fingerprint(model.api_defaults(expected)) == BEFORE_SHA256, "SOURCE_DIGEST_MISMATCH")
    model.require(index < len(expected["validations"]) and expected["validations"][index] == patch[2]["value"], "PATCH_PREIMAGE_CHANGED")
    expected["validations"][index] = copy.deepcopy(patch[3]["value"])
    model.require(model.fingerprint(model.api_defaults(expected)) == AFTER_SHA256, "SOURCE_DIGEST_MISMATCH")
    model.require(candidate.get("spec") == expected and after.get("spec") == before.get("spec") and
                  guarded_metadata(candidate) == guarded_metadata(before) and
                  guarded_metadata(after) == guarded_metadata(before),
                  "POLICY_SPEC_DRIFT")
    return {"status": "PASS_DRY_RUN_ONLY", "baseRevision": BASE_REVISION, "targetRevision": TARGET_REVISION,
            "beforeSHA256": BEFORE_SHA256, "afterSHA256": AFTER_SHA256,
            "policy": original_id, "binding": binding_state(binding_before), "parameters": parameter_snapshot,
            "candidateSpecSHA256": model.fingerprint(candidate["spec"]), "ownerReadiness": "NOT_CHECKED", "apply": "NOT_RUN"}
