"""Связывает intent с read-only отчётом; не выполняет и не разрешает apply.

Caller отвечает за owner-допуск, единый реестр попыток и fencing. Проверка
digest не доказывает согласие владельца, а уникальность имени действует только
внутри выбранного приватного каталога.
"""

import importlib.util
from pathlib import Path
import re


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


inspect = load("runtime_policy_inspect", "runtime-policy-refactor-inspect.py")
private = load("runtime_policy_private", "runtime-policy-refactor-private.py")


def build_intent(report, expected_digest, attempt_id):
    """Строит закрытую связь с планом; не создаёт новую идентичность попытки."""
    inspect.validate_report(report)
    require = inspect.plan.model.require
    require(isinstance(expected_digest, str) and re.fullmatch(r"[0-9a-f]{64}", expected_digest) and
            expected_digest == inspect.plan.model.fingerprint(report) and
            isinstance(attempt_id, str) and re.fullmatch(inspect.UUID, attempt_id), "INTENT_BINDING_INVALID")
    intent = {"schema": "kodex.runtime-policy-refactor-intent/v1", "planSHA256": expected_digest,
              "policyUID": report["policy"]["uid"], "attemptID": attempt_id}
    inspect.validate_intent(report, intent)
    return intent


def persist_intent(path, report, intent):
    """Однократно записывает intent; при ошибке файл сохраняется для readback."""
    inspect.validate_intent(report, intent)
    target = Path(path)
    inspect.plan.model.require(target.name == f"runtime-policy-{intent['attemptID']}.intent.json", "INTENT_PATH_INVALID")
    private.create_private(target, intent)
    result = private.read_private(target)
    inspect.plan.model.require(result == intent, "INTENT_READBACK_MISMATCH")
    return result


def read_intent(path, report):
    """Проверяет приватный артефакт и его точную привязку к отчёту."""
    result = private.read_private(path)
    inspect.validate_intent(report, result)
    inspect.plan.model.require(Path(path).name == f"runtime-policy-{result['attemptID']}.intent.json", "INTENT_PATH_INVALID")
    return result
